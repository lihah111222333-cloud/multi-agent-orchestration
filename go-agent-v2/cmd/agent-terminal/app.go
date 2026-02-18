// app.go — Wails 绑定: Agent 管理 + 通用 API 桥。
//
// 前端通过 window.go.main.App.XXX() 调用。
//
// 核心方法:
//   - CallAPI(method, params): 通用 JSON-RPC 桥, 覆盖全部后端功能
//   - GetLSPDiagnostics/GetLSPStatus: LSP 工具结果显示
//   - handleBridgeNotification: Go 标准化事件 → Wails 事件 → 前端展示
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// App Wails 绑定 — 前端通过 window.go.main.App.XXX() 调用。
type App struct {
	srv      *apiserver.Server    // 后端 API (所有操作通过此调用)
	mgr      *runner.AgentManager // Agent 进程管理 (事件监听 + 直接操作)
	group    string               // 分组名称
	autoN    int                  // 自动启动数量
	wailsApp *application.App
}

const callAPISampleEvery int64 = 30
const callAPISlowThreshold = 1200 * time.Millisecond
const bridgeNotifySampleEvery int64 = 120

var callAPIRequestSeq atomic.Int64
var bridgeNotifySeq atomic.Int64

func isCallAPIHotMethod(method string) bool {
	switch method {
	case "thread/list", "workspace/run/list", "thread/messages":
		return true
	default:
		return false
	}
}

func shouldLogCallAPIBegin(method string, reqID int64) bool {
	if reqID <= 6 {
		return true
	}
	if !isCallAPIHotMethod(method) {
		return true
	}
	return reqID%callAPISampleEvery == 0
}

func shouldLogCallAPIDone(method string, reqID int64, duration time.Duration) bool {
	if reqID <= 6 {
		return true
	}
	if duration >= callAPISlowThreshold {
		return true
	}
	if !isCallAPIHotMethod(method) {
		return true
	}
	return reqID%callAPISampleEvery == 0
}

func shouldLogBridgeNotify(method string, seq int64) bool {
	lower := strings.ToLower(method)
	if strings.Contains(lower, "delta") || strings.Contains(lower, "output") {
		return seq%bridgeNotifySampleEvery == 0
	}
	return true
}

// NewApp 创建 App 实例。
func NewApp(group string, autoN int, srv *apiserver.Server, mgr *runner.AgentManager) *App {
	return &App{
		srv:   srv,
		mgr:   mgr,
		group: group,
		autoN: autoN,
	}
}

// ServiceStartup Wails v3 Service 生命周期: 应用启动时调用。
func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	if a.autoN > 0 && a.wailsApp != nil {
		a.wailsApp.Event.Emit("auto-launch", map[string]interface{}{
			"count": a.autoN,
			"group": a.group,
		})
	}
	return nil
}

func (a *App) shutdown() {
	done := make(chan struct{})
	util.SafeGo(func() {
		a.mgr.StopAll()
		close(done)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// ========================================
// 通用 API 桥 — 覆盖全部 45+ JSON-RPC 方法
// ========================================

// CallAPI 通用 JSON-RPC 调用桥。
//
// 前端使用:
//
//	const result = await window.go.main.App.CallAPI("thread/start", '{"model":"o4-mini"}')
//	const dag = await window.go.main.App.CallAPI("resource_task_get_dag", '{"dag_id":"xxx"}')
//
// 覆盖: 线程生命周期, 对话控制, 技能管理, 模型/配置, MCP, 命令执行, 日志查询 等。
func (a *App) CallAPI(method, paramsJSON string) (resultJSON string, callErr error) {
	start := time.Now()
	reqID := callAPIRequestSeq.Add(1)
	if shouldLogCallAPIBegin(method, reqID) {
		logger.Info("CallAPI begin", logger.FieldReqID, reqID, logger.FieldMethod, method)
	}
	defer func() {
		duration := time.Since(start)
		if callErr != nil {
			logger.Warn("CallAPI failed",
				logger.FieldReqID, reqID,
				logger.FieldMethod, method,
				logger.FieldDurationMS, duration.Milliseconds(),
				logger.FieldError, callErr)
			return
		}
		if shouldLogCallAPIDone(method, reqID, duration) {
			if duration >= callAPISlowThreshold {
				logger.Warn("CallAPI slow",
					logger.FieldReqID, reqID,
					logger.FieldMethod, method,
					logger.FieldDurationMS, duration.Milliseconds())
			} else {
				logger.Info("CallAPI done",
					logger.FieldReqID, reqID,
					logger.FieldMethod, method,
					logger.FieldDurationMS, duration.Milliseconds())
			}
		}
	}()

	// UI 辅助方法 (不经过 apiserver)
	switch method {
	case "ui/selectProjectDir":
		path := a.SelectProjectDir()
		data, err := json.Marshal(map[string]string{"path": path})
		if err != nil {
			return "", fmt.Errorf("ui/selectProjectDir: marshal: %w", err)
		}
		return string(data), nil
	case "ui/selectFiles":
		paths := a.SelectFiles()
		data, err := json.Marshal(map[string]any{"paths": paths})
		if err != nil {
			return "", fmt.Errorf("ui/selectFiles: marshal: %w", err)
		}
		return string(data), nil
	case "ui/buildInfo":
		data, err := json.Marshal(currentBuildInfo())
		if err != nil {
			return "", fmt.Errorf("ui/buildInfo: marshal: %w", err)
		}
		return string(data), nil
	}

	var params json.RawMessage
	if paramsJSON != "" {
		params = json.RawMessage(paramsJSON)
	} else {
		params = json.RawMessage("{}")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	result, err := a.srv.InvokeMethod(ctx, method, params)
	if err != nil {
		return "", err
	}

	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(data), nil
}

// ========================================
// 项目管理 — 原生目录选择对话框
// ========================================

// SelectProjectDir 弹出原生目录选择对话框，返回所选路径。
// 用户取消返回空字符串。
// 约束: 前端仅做 UI，不应自行实现浏览器/系统文件选择能力。
// 目录选择必须统一走此 Wails/Go 原生桥接入口。
func (a *App) SelectProjectDir() string {
	logger.Info("SelectProjectDir: invoked")

	if a.wailsApp == nil {
		logger.Warn("SelectProjectDir: wails app not ready")
		return ""
	}

	cwd, _ := os.Getwd()
	dialog := a.wailsApp.Dialog.OpenFile().
		SetTitle("选择项目目录").
		SetMessage("请选择项目目录").
		SetButtonText("选择").
		SetDirectory(cwd).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true).
		ShowHiddenFiles(true)
	if current := a.wailsApp.Window.Current(); current != nil {
		dialog.AttachToWindow(current)
	}

	logger.Info("SelectProjectDir: opening Wails folder dialog")
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		if isDialogCancelError(err) {
			logger.Info("SelectProjectDir: dialog cancelled by user")
			return ""
		}
		logger.Warn("SelectProjectDir: Wails dialog failed", logger.FieldError, err)
		return ""
	}
	if path == "" {
		logger.Info("SelectProjectDir: dialog cancelled by user")
		return ""
	}
	logger.Info("SelectProjectDir: selected", logger.FieldPath, path)
	return path
}

// SelectFiles 弹出原生文件选择对话框(支持多选)，返回绝对路径数组。
// 用户取消返回空数组。
// 约束: 前端仅做 UI，不应实现任何浏览器侧文件系统选择兜底。
// 附件选择必须统一走此 Wails/Go 原生桥接入口。
func (a *App) SelectFiles() []string {
	logger.Info("SelectFiles: invoked")

	if a.wailsApp == nil {
		logger.Warn("SelectFiles: wails app not ready")
		return []string{}
	}

	cwd, _ := os.Getwd()
	dialog := a.wailsApp.Dialog.OpenFile().
		SetTitle("选择附件文件").
		SetMessage("可多选文件").
		SetButtonText("选择").
		SetDirectory(cwd).
		CanChooseDirectories(false).
		CanChooseFiles(true).
		ShowHiddenFiles(true)
	if current := a.wailsApp.Window.Current(); current != nil {
		dialog.AttachToWindow(current)
	}

	logger.Info("SelectFiles: opening Wails file dialog")
	paths, err := dialog.PromptForMultipleSelection()
	if err != nil {
		if isDialogCancelError(err) {
			logger.Info("SelectFiles: dialog cancelled by user")
			return []string{}
		}
		logger.Warn("SelectFiles: Wails dialog failed", logger.FieldError, err)
		return []string{}
	}
	if len(paths) == 0 {
		logger.Info("SelectFiles: dialog cancelled by user")
		return []string{}
	}
	logger.Info("SelectFiles: selected", logger.FieldCount, len(paths))
	return paths
}

func isDialogCancelError(err error) bool {
	if err == nil {
		return false
	}
	lowerErr := strings.ToLower(err.Error())
	return strings.Contains(lowerErr, "cancel")
}

// ========================================
// Agent 管理 (便捷方法, 内部走 apiserver)
// ========================================

// LaunchAgent 启动一个 Agent (通过 apiserver, 注入完整工具链)。
func (a *App) LaunchAgent(name, prompt, cwd string) (string, error) {
	logger.Info("ui: launch agent", logger.FieldSource, "ui",
		logger.FieldComponent, "agent", "name", name)

	params, err := json.Marshal(map[string]string{
		"model": "",
		"cwd":   cwd,
	})
	if err != nil {
		return "", fmt.Errorf("launch agent: marshal params: %w", err)
	}
	result, err := a.srv.InvokeMethod(context.Background(), "thread/start", params)
	if err != nil {
		return "", fmt.Errorf("launch agent: %w", err)
	}

	resultMap := util.ToMapAny(result)

	// 发送初始 prompt (如果有)
	if prompt != "" {
		// 用结构体提取 thread.id，避免深层类型断言链
		type threadResult struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		var tr threadResult
		if raw, marshalErr := json.Marshal(resultMap); marshalErr == nil {
			_ = json.Unmarshal(raw, &tr)
		}
		if tr.Thread.ID != "" {
			turnParams, marshalErr := json.Marshal(map[string]string{
				"threadId": tr.Thread.ID,
				"prompt":   prompt,
			})
			if marshalErr != nil {
				logger.Warn("turn/start marshal failed", logger.FieldError, marshalErr)
			} else if _, err := a.srv.InvokeMethod(context.Background(), "turn/start", turnParams); err != nil {
				logger.Warn("turn/start invoke failed", logger.FieldError, err)
			}
		}
	}

	data, err := json.Marshal(resultMap)
	if err != nil {
		return "", fmt.Errorf("launch agent: marshal result: %w", err)
	}
	return string(data), nil
}

// LaunchBatch 批量启动 N 个 Agent。
func (a *App) LaunchBatch(count int, cwd string) error {
	logger.Info("ui: launch batch", logger.FieldSource, "ui",
		logger.FieldComponent, "agent", "count", count, "group", a.group)
	for i := 1; i <= count; i++ {
		name := fmt.Sprintf("Agent %d", i)
		if a.group != "" {
			name = fmt.Sprintf("%s-%d", a.group, i)
		}
		if _, err := a.LaunchAgent(name, "", cwd); err != nil {
			return fmt.Errorf("launch %s: %w", name, err)
		}
	}
	return nil
}

// SubmitInput 向 Agent 发送消息。
func (a *App) SubmitInput(agentID, prompt string) error {
	logger.Info("ui: submit input", logger.FieldSource, "ui",
		logger.FieldComponent, "chat", logger.FieldAgentID, agentID,
		"prompt_len", len(prompt))
	return a.mgr.Submit(agentID, prompt, nil, nil)
}

// SubmitWithFiles 向 Agent 发送消息 + 附件。
func (a *App) SubmitWithFiles(agentID, prompt string, images, files []string) error {
	logger.Info("ui: submit with files", logger.FieldSource, "ui",
		logger.FieldComponent, "chat", logger.FieldAgentID, agentID,
		"images", len(images), "files", len(files))
	return a.mgr.Submit(agentID, prompt, images, files)
}

// SendCommand 向 Agent 发送斜杠命令。
func (a *App) SendCommand(agentID, cmd, args string) error {
	logger.Info("ui: send command", logger.FieldSource, "ui",
		logger.FieldComponent, "command", logger.FieldAgentID, agentID,
		"cmd", cmd)
	return a.mgr.SendCommand(agentID, cmd, args)
}

// StopAgent 停止一个 Agent (非阻塞)。
func (a *App) StopAgent(id string) error {
	logger.Info("ui: stop agent", logger.FieldSource, "ui",
		logger.FieldComponent, "agent", logger.FieldAgentID, id)
	done := make(chan error, 1)
	util.SafeGo(func() { done <- a.mgr.Stop(id) })

	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		return fmt.Errorf("stop %s: timeout", id)
	}
}

// ListAgents 返回所有 Agent 信息。
func (a *App) ListAgents() []runner.AgentInfo {
	return a.mgr.List()
}

// GetGroup 返回当前窗口的分组名。
func (a *App) GetGroup() string { return a.group }

// GetBuildInfo 返回当前桌面应用构建信息(JSON字符串)。
func (a *App) GetBuildInfo() string {
	data, err := json.Marshal(currentBuildInfo())
	if err != nil {
		logger.Warn("GetBuildInfo: marshal failed", logger.FieldError, err)
		return "{}"
	}
	return string(data)
}

// ========================================
// LSP 调度 — 前端展示 agent 使用 LSP 工具的结果
// ========================================

// GetLSPDiagnostics 获取当前缓存的 LSP 诊断信息 (按文件)。
//
// 前端使用:
//
//	const diags = await window.go.main.App.GetLSPDiagnostics("")
//	// 返回: {"file:///path/to/file.go": [{Range, Message, Severity}...]}
func (a *App) GetLSPDiagnostics(filePath string) (string, error) {
	params, _ := json.Marshal(map[string]string{"file_path": filePath})
	// 使用 apiserver 内置的 lsp_diagnostics handler
	result, err := a.srv.InvokeMethod(context.Background(), "lsp_diagnostics_query", params)
	if err != nil {
		// 直接查 diagCache — 如果没有专用 method, 走 JSON-RPC 不通时降级
		return "{}", nil
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

// GetLSPStatus 获取所有 LSP 服务器状态。
//
// 前端使用:
//
//	const status = await window.go.main.App.GetLSPStatus()
//	// 返回: [{"Language":"go","Status":"running","Port":0}...]
func (a *App) GetLSPStatus() (string, error) {
	result, err := a.CallAPI("mcpServerStatus/list", "{}")
	if err != nil {
		return "[]", nil
	}
	return result, nil
}

// SaveClipboardImage 保存剪贴板图片(base64)到临时文件, 返回路径。
//
// 前端使用:
//
//	const path = await window.go.main.App.SaveClipboardImage(base64Data)
func (a *App) SaveClipboardImage(base64Data string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		// 尝试 RawStdEncoding (无 padding)
		data, err = base64.RawStdEncoding.DecodeString(base64Data)
		if err != nil {
			return "", fmt.Errorf("decode base64: %w", err)
		}
	}

	tmpFile, err := os.CreateTemp("", "clipboard-*.png")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	logger.Info("ui: saved clipboard image", logger.FieldSource, "ui",
		logger.FieldComponent, "clipboard", "path", tmpFile.Name(), "size", len(data))
	return tmpFile.Name(), nil
}

// ========================================
// 多窗口 + 事件转发
// ========================================

// OpenNewWindow 启动一个新的 agent-terminal 进程 (独立窗口)。
func (a *App) OpenNewWindow(group string, n int) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable: %w", err)
	}
	args := []string{}
	if group != "" {
		args = append(args, "--group", group)
	}
	if n > 0 {
		args = append(args, "--n", fmt.Sprintf("%d", n))
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

// handleBridgeNotification 将 apiserver 标准化通知转发到 Wails 前端。
//
// 事件链路:
// codex raw event -> apiserver.Notify(method,payload) -> Wails runtime Events -> Vue 渲染。
func (a *App) handleBridgeNotification(method string, params any) {
	notifyID := bridgeNotifySeq.Add(1)
	start := time.Now()
	publishDebugBridgeEvent(method, params)

	if a.wailsApp == nil {
		if shouldLogBridgeNotify(method, notifyID) {
			logger.Info("bridge notify buffered without wails runtime",
				"notify_id", notifyID,
				"method", method,
				"duration_ms", time.Since(start).Milliseconds())
		}
		return
	}

	payloadMap := util.ToMapAny(params)

	rawPayload, marshalErr := json.Marshal(payloadMap)
	if marshalErr != nil {
		logger.Debug("wails: notify bridge rawPayload marshal", logger.FieldError, marshalErr)
		rawPayload = []byte("{}")
	}
	// 通用桥接事件: 前端可统一订阅 bridge-event 自行按 type 渲染。
	a.wailsApp.Event.Emit("bridge-event", map[string]any{
		"type":    method,
		"payload": payloadMap,
		"data":    string(rawPayload),
	})

	threadID, _ := payloadMap["threadId"].(string)
	if strings.TrimSpace(threadID) == "" {
		if shouldLogBridgeNotify(method, notifyID) {
			logger.Info("bridge notify emitted",
				"notify_id", notifyID,
				"method", method,
				"channels", 1,
				"duration_ms", time.Since(start).Milliseconds())
		}
		return
	}

	// 兼容历史 agent-event 通道 (按 threadId 路由)。
	a.wailsApp.Event.Emit("agent-event", map[string]any{
		"agent_id": threadID,
		"type":     method,
		"data":     string(rawPayload),
	})
	if shouldLogBridgeNotify(method, notifyID) {
		logger.Info("bridge notify emitted",
			"notify_id", notifyID,
			"method", method,
			"thread_id", threadID,
			"channels", 2,
			"duration_ms", time.Since(start).Milliseconds())
	}
}

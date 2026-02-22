// code_run_tools.go — 代码块执行动态工具: 构建、审批、审计。
//
// 两个工具:
//   - code_run:      执行代码片段 (Go/JS/TS) 或项目命令
//   - code_run_test: 执行 go test -run
//
// 审批策略: 仅 project_cmd 强制审批; run/test 默认免审批。
// 审计: 所有执行结果写入 audit_events 表。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ========================================
// 工具定义
// ========================================

// buildCodeRunTools 返回代码执行工具定义 (注入 codex agent)。
//
// 不可用的语言不影响工具注册 — 运行时按语言返回错误。
func (s *Server) buildCodeRunTools() []codex.DynamicTool {
	if s.codeRunner == nil {
		return nil
	}

	tools := []codex.DynamicTool{
		{
			Name:        "code_run",
			Description: "Execute a code snippet (Go, JavaScript, TypeScript) or a project shell command. Go snippets can be auto-wrapped with main function and imports. Use mode='project_cmd' for shell commands (requires approval).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"language":  map[string]any{"type": "string", "description": "Language: go, javascript, typescript. Required for run mode."},
					"code":      map[string]any{"type": "string", "description": "Code snippet to execute (for run mode)"},
					"command":   map[string]any{"type": "string", "description": "Shell command (for project_cmd mode)"},
					"mode":      map[string]any{"type": "string", "enum": []string{"run", "project_cmd"}, "description": "Execution mode. Default: run"},
					"auto_wrap": map[string]any{"type": "boolean", "description": "Auto-wrap Go code with package main and imports. Default: true for Go"},
					"work_dir":  map[string]any{"type": "string", "description": "Custom working directory (must be within project root)"},
					"timeout":   map[string]any{"type": "number", "description": "Timeout in seconds. Default: 30"},
				},
				"required": []string{"mode"},
			},
		},
		{
			Name:        "code_run_test",
			Description: "Run a specific Go test function. Equivalent to: go test -v -run ^TestFunc$ [package]",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"test_func": map[string]any{"type": "string", "description": "Test function name (e.g. TestMyFunction)"},
					"test_pkg":  map[string]any{"type": "string", "description": "Package path (e.g. ./internal/executor/). Default: ./..."},
					"timeout":   map[string]any{"type": "number", "description": "Timeout in seconds. Default: 30"},
				},
				"required": []string{"test_func"},
			},
		},
	}

	return tools
}

// ========================================
// Handler (需要 agentID + callID)
// ========================================

// codeRunWithAgent 处理 code_run 工具调用。
func (s *Server) codeRunWithAgent(ctx context.Context, agentID, callID string, args json.RawMessage) string {
	runner, cleanupRunner, runnerErr := s.resolveCodeRunner(agentID)
	if runnerErr != nil {
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, runnerErr.Error())
	}
	defer cleanupRunner()

	var p struct {
		Language string  `json:"language"`
		Code     string  `json:"code"`
		Command  string  `json:"command"`
		Mode     string  `json:"mode"`
		AutoWrap *bool   `json:"auto_wrap,omitempty"`
		WorkDir  string  `json:"work_dir"`
		Timeout  float64 `json:"timeout"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(err)
	}

	if p.Mode == "" {
		p.Mode = executor.ModeRun
	}

	// project_cmd 强制审批
	if p.Mode == executor.ModeProjectCmd {
		isDangerous := executor.DetectDangerous(p.Command) != ""
		if !s.awaitCodeRunApproval(agentID, callID, p.Mode, p.Command, isDangerous) {
			s.writeCodeRunAudit(agentID, p.Language, p.Mode, "denied", 0, 0, p.Code, p.Command, "")
			return `{"error":"execution denied by user","exit_code":-1}`
		}
	}

	// 构建执行请求
	autoWrap := p.Mode == executor.ModeRun && strings.EqualFold(p.Language, "go")
	if p.AutoWrap != nil {
		autoWrap = *p.AutoWrap
	}

	workDir := strings.TrimSpace(p.WorkDir)
	if workDir == "" {
		workDir = s.getAgentWorkDir(agentID)
	}
	timeout := parseCodeRunTimeout(p.Timeout)

	result, err := runner.Run(ctx, executor.RunRequest{
		Language: p.Language,
		Code:     p.Code,
		Command:  p.Command,
		Mode:     p.Mode,
		AutoWrap: autoWrap,
		WorkDir:  workDir,
		Timeout:  timeout,
	})
	if err != nil {
		s.writeCodeRunAudit(agentID, p.Language, p.Mode, "failed", -1, 0, p.Code, p.Command, err.Error())
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, err.Error())
	}

	s.writeCodeRunAudit(agentID, p.Language, p.Mode, resultStatus(result),
		result.ExitCode, result.Duration.Milliseconds(), p.Code, p.Command, result.Output)

	return codeRunResultJSON(result)
}

// codeRunTestWithAgent 处理 code_run_test 工具调用。
func (s *Server) codeRunTestWithAgent(ctx context.Context, agentID, _ string, args json.RawMessage) string {
	runner, cleanupRunner, runnerErr := s.resolveCodeRunner(agentID)
	if runnerErr != nil {
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, runnerErr.Error())
	}
	defer cleanupRunner()

	var p struct {
		TestFunc string  `json:"test_func"`
		TestPkg  string  `json:"test_pkg"`
		Timeout  float64 `json:"timeout"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(err)
	}

	timeout := parseCodeRunTimeout(p.Timeout)

	result, err := runner.Run(ctx, executor.RunRequest{
		Mode:     executor.ModeTest,
		TestFunc: p.TestFunc,
		TestPkg:  p.TestPkg,
		Timeout:  timeout,
	})
	if err != nil {
		s.writeCodeRunAudit(agentID, "go", executor.ModeTest, "failed", -1, 0, "", p.TestFunc, err.Error())
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, err.Error())
	}

	s.writeCodeRunAudit(agentID, "go", executor.ModeTest, resultStatus(result),
		result.ExitCode, result.Duration.Milliseconds(), "", p.TestFunc, result.Output)

	return codeRunResultJSON(result)
}

// ========================================
// 执行上下文 / 工作目录 / runner 解析
// ========================================

// parseCodeRunTimeout 将 timeout(秒)转换为 time.Duration。
// 返回 0 表示使用 CodeRunner 默认值。
func parseCodeRunTimeout(seconds float64) time.Duration {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0
	}
	ns := seconds * float64(time.Second)
	if ns >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	timeout := time.Duration(ns)
	if timeout <= 0 {
		return time.Nanosecond
	}
	return timeout
}

func samePath(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func normalizeAgentWorkDir(cwd string) string {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		return ""
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(abs)
}

func (s *Server) setAgentWorkDir(agentID, cwd string) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return
	}
	normalized := normalizeAgentWorkDir(cwd)
	if normalized == "" {
		return
	}
	s.agentWorkDirMu.Lock()
	if s.agentWorkDirs == nil {
		s.agentWorkDirs = make(map[string]string)
	}
	s.agentWorkDirs[id] = normalized
	s.agentWorkDirMu.Unlock()
}

func (s *Server) getAgentWorkDir(agentID string) string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return ""
	}
	s.agentWorkDirMu.RLock()
	cwd := s.agentWorkDirs[id]
	s.agentWorkDirMu.RUnlock()
	return cwd
}

func (s *Server) clearAgentWorkDir(agentID string) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return
	}
	s.agentWorkDirMu.Lock()
	delete(s.agentWorkDirs, id)
	s.agentWorkDirMu.Unlock()
}

// resolveCodeRunner 按 agent 默认 cwd 选择 runner。
// 当 agent cwd 与 server 全局 runner 根目录不一致时, 创建一次性 runner 并返回 cleanup。
func (s *Server) resolveCodeRunner(agentID string) (*executor.CodeRunner, func(), error) {
	defaultRunner := s.codeRunner
	agentCwd := s.getAgentWorkDir(agentID)
	if agentCwd == "" {
		if defaultRunner == nil {
			return nil, nil, fmt.Errorf("code runner not available")
		}
		return defaultRunner, func() {}, nil
	}

	if defaultRunner != nil && samePath(defaultRunner.WorkDir(), agentCwd) {
		return defaultRunner, func() {}, nil
	}

	runner, err := executor.NewCodeRunner(agentCwd)
	if err != nil {
		if defaultRunner != nil {
			logger.Warn("code-run: agent runner init failed, fallback to default runner",
				logger.FieldAgentID, agentID,
				logger.FieldCwd, agentCwd,
				logger.FieldError, err,
			)
			return defaultRunner, func() {}, nil
		}
		return nil, nil, err
	}
	return runner, runner.Cleanup, nil
}

// registerCodeRunCancel 注册运行中的 code_run 取消函数, 返回本次 runKey。
func (s *Server) registerCodeRunCancel(agentID, callID string, cancel context.CancelFunc) string {
	if cancel == nil {
		return ""
	}
	id := strings.TrimSpace(agentID)
	if id == "" {
		return ""
	}
	seq := s.codeRunSeq.Add(1)
	key := fmt.Sprintf("%s#%d", strings.TrimSpace(callID), seq)

	s.codeRunMu.Lock()
	if s.activeCodeRuns == nil {
		s.activeCodeRuns = make(map[string]map[string]context.CancelFunc)
	}
	runs := s.activeCodeRuns[id]
	if runs == nil {
		runs = make(map[string]context.CancelFunc)
		s.activeCodeRuns[id] = runs
	}
	runs[key] = cancel
	s.codeRunMu.Unlock()
	return key
}

func (s *Server) unregisterCodeRunCancel(agentID, runKey string) {
	id := strings.TrimSpace(agentID)
	key := strings.TrimSpace(runKey)
	if id == "" || key == "" {
		return
	}
	s.codeRunMu.Lock()
	if runs := s.activeCodeRuns[id]; runs != nil {
		delete(runs, key)
		if len(runs) == 0 {
			delete(s.activeCodeRuns, id)
		}
	}
	s.codeRunMu.Unlock()
}

// cancelCodeRuns 取消指定 agent 当前所有 code_run/code_run_test 执行。
func (s *Server) cancelCodeRuns(agentID string) int {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return 0
	}
	s.codeRunMu.Lock()
	runs := s.activeCodeRuns[id]
	delete(s.activeCodeRuns, id)
	s.codeRunMu.Unlock()
	if len(runs) == 0 {
		return 0
	}
	for _, cancel := range runs {
		cancel()
	}
	return len(runs)
}

// cancelAllCodeRuns 取消所有 agent 的 code_run/code_run_test 执行。
func (s *Server) cancelAllCodeRuns() int {
	s.codeRunMu.Lock()
	all := s.activeCodeRuns
	s.activeCodeRuns = make(map[string]map[string]context.CancelFunc)
	s.codeRunMu.Unlock()

	total := 0
	for _, runs := range all {
		for _, cancel := range runs {
			cancel()
			total++
		}
	}
	return total
}

// ========================================
// 审批 (双通道 + fail-close)
// ========================================

// codeRunApprovalNonce 进程内唯一计数器 (callID 为空时生成 nonce)。
var codeRunApprovalNonce atomic.Int64

// awaitCodeRunApproval 等待用户审批代码执行。
//
// 设计:
//   - 协议 method 固定: item/commandExecution/requestApproval (不拼接 callID)
//   - 去重键独立: agentID + method + approvalID, 避免同一 agent 并发请求互相吞掉
//   - 双通道: WebSocket → Wails 降级
//   - fail-close: 无前端/超时/错误统一返回 false
//   - 不自带心跳 — 外层 handleDynamicToolCall 已有心跳
func (s *Server) awaitCodeRunApproval(agentID, callID, mode, command string, isDangerous bool) bool {
	const method = "item/commandExecution/requestApproval"

	// approvalID: callID 优先 → nonce 兜底
	approvalID := callID
	if approvalID == "" {
		approvalID = fmt.Sprintf("coderun-%d", codeRunApprovalNonce.Add(1))
	}

	// 独立去重键 (含 approvalID, 不和原审批冲突)
	inflightKey := agentID + ":" + method + ":" + approvalID
	if _, loaded := s.approvalInFlight.LoadOrStore(inflightKey, struct{}{}); loaded {
		logger.Debug("code-run: approval dedup — skipping",
			logger.FieldAgentID, agentID, logger.FieldCallID, callID)
		return false
	}
	defer s.approvalInFlight.Delete(inflightKey)

	// 构建审批 payload
	payload := map[string]any{
		"type":         "code_run_approval",
		"agent_id":     agentID,
		"mode":         mode,
		"command":      executor.TruncateForAudit(command, 2048),
		"is_dangerous": isDangerous,
	}

	// 双通道等待
	return s.waitForFrontendDecision(method, payload)
}

// waitForFrontendDecision 抽取双通道等待逻辑 (WebSocket → Wails → fail-close)。
//
// 共享于 handleApprovalRequest 和 awaitCodeRunApproval:
//   - 优先 WebSocket SendRequestToAll
//   - 降级 AllocPendingRequest + broadcastNotification
//   - 超时/无前端 → false (fail-close)
func (s *Server) waitForFrontendDecision(method string, payload map[string]any) bool {
	// 尝试 WebSocket
	resp, wsErr := s.SendRequestToAll(method, payload)
	if wsErr == nil && resp != nil && resp.Result != nil {
		if m, ok := resp.Result.(map[string]any); ok {
			if approved, ok := m["approved"].(bool); ok {
				return approved
			}
		}
	}

	// 降级: Wails 模式
	s.notifyHookMu.RLock()
	hasHook := s.notifyHook != nil
	s.notifyHookMu.RUnlock()

	if !hasHook {
		logger.Warn("code-run: approval auto-denied — no frontend", "method", method)
		return false
	}

	reqID, ch, cleanup := s.AllocPendingRequest()
	defer cleanup()

	if payload == nil {
		payload = make(map[string]any)
	}
	payload["requestId"] = reqID
	s.broadcastNotification(method, payload)

	// 5 分钟超时
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()

	select {
	case wailsResp := <-ch:
		if wailsResp != nil && wailsResp.Result != nil {
			if m, ok := wailsResp.Result.(map[string]any); ok {
				if approved, ok := m["approved"].(bool); ok {
					return approved
				}
			}
		}
	case <-timer.C:
		logger.Warn("code-run: approval timed out", "method", method)
	}
	return false
}

// ========================================
// 审计
// ========================================

// writeCodeRunAudit 写入 code_run 审计事件。
func (s *Server) writeCodeRunAudit(agentID, language, mode, result string, exitCode int, durationMS int64, code, command, output string) {
	if s.auditLogStore == nil {
		return
	}

	extra := map[string]any{
		"exit_code":        exitCode,
		"duration_ms":      durationMS,
		"language":         language,
		"output_truncated": len(output) > executor.MaxAuditPayloadSize(),
	}
	// 安全裁剪: 代码/命令/输出 ≤ 4KB
	if code != "" {
		extra["code"] = executor.TruncateForAudit(code, 0)
	}
	if command != "" {
		extra["command"] = executor.TruncateForAudit(command, 0)
	}
	if output != "" {
		extra["output"] = executor.TruncateForAudit(output, 0)
	}

	event := &store.AuditEvent{
		EventType: "code_run",
		Action:    mode,
		Result:    result,
		Actor:     agentID,
		Target:    language + "/" + mode,
		Detail:    fmt.Sprintf("exit_code=%d duration_ms=%d", exitCode, durationMS),
		Level:     "INFO",
		Extra:     extra,
	}
	if err := s.auditLogStore.Append(context.Background(), event); err != nil {
		logger.Warn("code-run: audit write failed", logger.FieldAgentID, agentID, logger.FieldError, err)
	}
}

// ========================================
// 工具函数
// ========================================

// resultStatus 从 RunResult 生成审计 result 字段。
func resultStatus(r *executor.RunResult) string {
	if r.Success {
		return "success"
	}
	return "failed"
}

// codeRunResultJSON 将 RunResult 序列化为 JSON 响应。
func codeRunResultJSON(r *executor.RunResult) string {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal result: %s"}`, err.Error())
	}
	return string(data)
}

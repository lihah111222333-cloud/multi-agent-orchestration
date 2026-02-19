// debug_server.go — 调试模式 HTTP 服务: 浏览器直接访问 UI。
//
// 启动方式: ./agent-terminal --debug
// 访问地址: http://localhost:4501
//
// 功能:
//   - 从磁盘 frontend/ 目录提供静态文件 (修改即刷新)
//   - 注入 wails-shim.js 替代 Wails runtime, 通过 HTTP 调用 apiserver
//   - 独立于 Wails 窗口, 可在 Chrome DevTools 中调试
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const debugPort = 4501

type debugBridgeEvent struct {
	ID      int64          `json:"id"`
	Type    string         `json:"type"`
	AgentID string         `json:"agent_id,omitempty"`
	Data    string         `json:"data"`
	Payload map[string]any `json:"payload"`
}

var debugBridgeHub = struct {
	mu     sync.RWMutex
	nextID int64
	events []debugBridgeEvent
}{
	events: make([]debugBridgeEvent, 0, 256),
}

const debugBridgeMaxEvents = 20000
const debugBridgePublishSampleEvery int64 = 120
const debugBridgePollSampleEvery int64 = 120

// debugBridgeDroppableSampleRate 可丢弃高频事件的入队采样率 (每 N 条入队 1 条)。
const debugBridgeDroppableSampleRate int64 = 5

// debugBridgeOverflowLogEvery 溢出日志采样间隔 — 第 1 次和每第 N 次溢出打 WARN。
const debugBridgeOverflowLogEvery int64 = 1000

var debugBridgeMetrics = struct {
	publishedTotal     atomic.Int64
	droppedTotal       atomic.Int64
	overflowCount      atomic.Int64
	droppableSkipTotal atomic.Int64
	pollRequestTotal   atomic.Int64
	pollResponseTotal  atomic.Int64
	pollEventOutTotal  atomic.Int64
	pollWriteFailTotal atomic.Int64
}{}

// shouldLogOverflow 判断是否应该打印溢出 WARN 日志。
// 高频事件 (delta/output/stream/state/rateLimits): 第 1 次 + 每 N 次采样。
// 低频事件: 始终打印。
func shouldLogOverflow(method string, overflowCount int64) bool {
	if !isHighFreqMethod(method) {
		return true
	}
	return overflowCount == 1 || overflowCount%debugBridgeOverflowLogEvery == 0
}

// isHighFreqMethod 判断是否为高频事件方法 (与 shouldLogBridgePublish 共享模式).
func isHighFreqMethod(method string) bool {
	lower := strings.ToLower(method)
	return strings.Contains(lower, "delta") ||
		strings.Contains(lower, "output") ||
		strings.Contains(lower, "stream") ||
		strings.Contains(lower, "state/changed") ||
		strings.Contains(lower, "ratelimits")
}

// isDroppableHighFreqMethod 判断是否为可安全丢弃的高频方法。
// 仅含 streaming 类事件 (delta/output/stream)。
// 注意: ui/state/changed 和 tokenUsage/updated 不可丢弃 — 前端依赖它们触发状态同步。
func isDroppableHighFreqMethod(method string) bool {
	lower := strings.ToLower(method)
	return strings.Contains(lower, "delta") ||
		strings.Contains(lower, "output") ||
		strings.Contains(lower, "stream")
}

var debugBridgeEnabled atomic.Bool

func shouldLogBridgePublish(method string, seq int64) bool {
	if isHighFreqMethod(method) {
		return seq%debugBridgePublishSampleEvery == 0
	}
	return true
}

// publishDebugBridgeEvent 将 Go 桥接事件写入调试队列。
// 约束：前端不直接连 SSE，仅通过此 Go 队列拉取事件。
func publishDebugBridgeEvent(method string, params any) {
	if !debugBridgeEnabled.Load() {
		return
	}

	// 可丢弃高频事件: 每 N 条只入队 1 条 (真正减少队列压力)。
	if isDroppableHighFreqMethod(method) {
		n := debugBridgeMetrics.droppableSkipTotal.Add(1)
		if n%debugBridgeDroppableSampleRate != 0 {
			return
		}
	}

	payloadMap := util.ToMapAny(params)

	rawPayload, err := json.Marshal(payloadMap)
	if err != nil {
		logger.Debug("debug: marshal bridge payload", logger.FieldError, err)
		rawPayload = []byte("{}")
	}
	threadID, _ := payloadMap["threadId"].(string)

	debugBridgeHub.mu.Lock()
	defer debugBridgeHub.mu.Unlock()

	debugBridgeHub.nextID++
	eventID := debugBridgeHub.nextID
	debugBridgeHub.events = append(debugBridgeHub.events, debugBridgeEvent{
		ID:      eventID,
		Type:    method,
		AgentID: threadID,
		Data:    string(rawPayload),
		Payload: payloadMap,
	})

	dropped := 0
	if len(debugBridgeHub.events) > debugBridgeMaxEvents {
		dropped = len(debugBridgeHub.events) - debugBridgeMaxEvents
		debugBridgeHub.events = append([]debugBridgeEvent(nil), debugBridgeHub.events[dropped:]...)
		totalDropped := debugBridgeMetrics.droppedTotal.Add(int64(dropped))
		overflowSeq := debugBridgeMetrics.overflowCount.Add(1)
		if shouldLogOverflow(method, overflowSeq) {
			logger.Warn("debug bridge: queue overflow, dropped oldest events",
				logger.FieldMethod, method,
				"dropped", dropped,
				"queue_depth", len(debugBridgeHub.events),
				"max_events", debugBridgeMaxEvents,
				"dropped_total", totalDropped,
				"overflow_seq", overflowSeq)
		}
	}

	publishedTotal := debugBridgeMetrics.publishedTotal.Add(1)
	if shouldLogBridgePublish(method, publishedTotal) {
		logger.Info("debug bridge: event queued",
			logger.FieldMethod, method,
			"event_id", eventID,
			logger.FieldAgentID, threadID,
			"queue_depth", len(debugBridgeHub.events),
			"published_total", publishedTotal,
			"dropped_total", debugBridgeMetrics.droppedTotal.Load())
	}
}

func readDebugBridgeEvents(after int64, limit int) ([]debugBridgeEvent, int64, int, int64) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	debugBridgeHub.mu.RLock()
	defer debugBridgeHub.mu.RUnlock()

	lastID := debugBridgeHub.nextID
	queueDepth := len(debugBridgeHub.events)
	oldestID := int64(0)
	if queueDepth > 0 {
		oldestID = debugBridgeHub.events[0].ID
	}
	if len(debugBridgeHub.events) == 0 {
		return nil, lastID, queueDepth, oldestID
	}

	out := make([]debugBridgeEvent, 0, limit)
	for _, evt := range debugBridgeHub.events {
		if evt.ID <= after {
			continue
		}
		out = append(out, evt)
		if len(out) >= limit {
			break
		}
	}
	return out, lastID, queueDepth, oldestID
}

// debugWailsRuntimeModule 提供 /wails/runtime.js，兼容 Wails v3 runtime 接口。
//
// 目标：让前端优先走 Call.ByID / Events.On，保持与桌面端路径一致。
//
//go:embed shim/wails-runtime.js
var debugWailsRuntimeModule string

// shimScriptTemplate 注入到 HTML 的 Wails runtime 兼容层。
//
// 功能:
//   - window.go.main.App.CallAPI → fetch ${apiBaseURL}/rpc
//   - window.runtime.EventsOn → SSE 连接 (简化)
//
//go:embed shim/bridge-shim.html
var shimScriptTemplate string

func buildDebugShimScript(apiBaseURL string) string {
	return strings.ReplaceAll(shimScriptTemplate, "__APP_SERVER_BASE_URL__", apiBaseURL)
}

// startDebugServer 启动调试 HTTP 服务器, 提供前端静态文件。
func startDebugServer(ctx context.Context, uiPort int, apiBaseURL string) {
	// 查找 frontend 目录
	frontendDir := findFrontendDir()
	if frontendDir == "" {
		logger.Error("debug: frontend directory not found")
		return
	}
	debugBridgeEnabled.Store(true)

	mux := http.NewServeMux()
	mux.HandleFunc("/select-project-dir", handleDebugSelectProjectDir)
	mux.HandleFunc("/select-files", handleDebugSelectFiles)
	mux.HandleFunc("/build-info", handleDebugBuildInfo)
	mux.HandleFunc("/bridge/events", handleDebugBridgeEvents)
	mux.HandleFunc("/wails/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprint(w, debugWailsRuntimeModule)
	})

	// 注入 shim 的 index.html handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Debug UI 一律禁用静态资源缓存，避免前端回滚/重构后浏览器继续使用旧模块。
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			data, err := os.ReadFile(filepath.Join(frontendDir, "index.html"))
			if err != nil {
				http.Error(w, "index.html not found", 404)
				return
			}
			html := string(data)
			shimScript := buildDebugShimScript(apiBaseURL)
			// 在 </head> 前注入 shim
			html = strings.Replace(html, "</head>", shimScript+"\n</head>", 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
			return
		}
		// 其他静态文件
		http.FileServer(http.Dir(frontendDir)).ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", uiPort),
		Handler: mux,
	}

	util.SafeGo(func() {
		logger.Info("debug: UI server started",
			logger.FieldURL, fmt.Sprintf("http://localhost:%d", uiPort),
			"api_url", apiBaseURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("debug server failed", logger.FieldError, err)
		}
	})

	util.SafeGo(func() {
		<-ctx.Done()
		server.Close()
	})
}

// handleDebugBridgeEvents 返回 Go 统一收集的桥接事件。
//
// GET /bridge/events?after=<id>&limit=<n>
// debugPollParams 解析后的轮询参数。
type debugPollParams struct {
	after          int64
	effectiveLimit int
}

// parseDebugPollParams 从请求解析 after/limit 查询参数。
func parseDebugPollParams(r *http.Request, pollID int64) debugPollParams {
	q := r.URL.Query()
	afterRaw := strings.TrimSpace(q.Get("after"))
	limitRaw := strings.TrimSpace(q.Get("limit"))

	var after int64
	if afterRaw != "" {
		v, err := strconv.ParseInt(afterRaw, 10, 64)
		if err != nil {
			logger.Warn("debug bridge: invalid poll query 'after'",
				"poll_id", pollID,
				"after", afterRaw,
				logger.FieldRemote, r.RemoteAddr,
				logger.FieldError, err)
		} else {
			after = v
		}
	}

	limit := 0
	if limitRaw != "" {
		v, err := strconv.Atoi(limitRaw)
		if err != nil {
			logger.Warn("debug bridge: invalid poll query 'limit'",
				"poll_id", pollID,
				"limit", limitRaw,
				logger.FieldRemote, r.RemoteAddr,
				logger.FieldError, err)
		} else {
			limit = v
		}
	}
	effectiveLimit := limit
	if effectiveLimit <= 0 || effectiveLimit > 500 {
		effectiveLimit = 200
	}

	return debugPollParams{after: after, effectiveLimit: effectiveLimit}
}

// writeDebugPollJSON 编码 JSON 响应, 写入失败时记录日志。返回 true 表示成功。
func writeDebugPollJSON(w http.ResponseWriter, resp map[string]any, pollID int64, start time.Time, logFields ...any) bool {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		writeFailTotal := debugBridgeMetrics.pollWriteFailTotal.Add(1)
		fields := append([]any{
			"poll_id", pollID,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
			"write_fail_total", writeFailTotal,
			logger.FieldError, err,
		}, logFields...)
		logger.Warn("debug bridge: poll write failed", fields...)
		return false
	}
	debugBridgeMetrics.pollResponseTotal.Add(1)
	return true
}

func handleFirstPoll(w http.ResponseWriter, pollID int64, pp debugPollParams, start time.Time) bool {
	if pp.after > 0 {
		return false
	}
	_, lastID, queueDepth, _ := readDebugBridgeEvents(0, 1)
	resp := map[string]any{
		"events":  []debugBridgeEvent{},
		"last_id": lastID,
	}
	if !writeDebugPollJSON(w, resp, pollID, start, "after", pp.after, "limit", pp.effectiveLimit, "last_id", lastID, "queue_depth", queueDepth) {
		return true
	}
	if queueDepth > 0 || pollID%debugBridgePollSampleEvery == 0 {
		logger.Info("debug bridge: poll cursor sync",
			"poll_id", pollID,
			"after", pp.after,
			"limit", pp.effectiveLimit,
			"last_id", lastID,
			"queue_depth", queueDepth,
			logger.FieldDurationMS, time.Since(start).Milliseconds())
	}
	return true
}

func handleDebugBridgeEvents(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	pollID := debugBridgeMetrics.pollRequestTotal.Add(1)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pp := parseDebugPollParams(r, pollID)

	// 首次连接（after<=0）只返回游标，不回放历史事件，防止刷新后事件洪峰卡死前端。
	if handleFirstPoll(w, pollID, pp, start) {
		return
	}
	serveDebugPollEvents(w, pollID, pp, start)
}

func serveDebugPollEvents(w http.ResponseWriter, pollID int64, pp debugPollParams, start time.Time) {
	events, lastID, queueDepth, oldestID := readDebugBridgeEvents(pp.after, pp.effectiveLimit)
	if events == nil {
		events = []debugBridgeEvent{}
	}
	eventCount := len(events)
	outTotal := debugBridgeMetrics.pollEventOutTotal.Add(int64(eventCount))

	servedLastID := int64(0)
	servedFirstID := int64(0)
	if eventCount > 0 {
		servedFirstID = events[0].ID
		servedLastID = events[eventCount-1].ID
	}
	laggingCursor := pp.after > 0 && oldestID > 0 && pp.after < oldestID-1
	truncated := eventCount > 0 && eventCount >= pp.effectiveLimit && servedLastID < lastID
	if laggingCursor {
		logger.Warn("debug bridge: poll cursor behind queue head",
			"poll_id", pollID,
			"after", pp.after,
			"oldest_id", oldestID,
			"last_id", lastID,
			"queue_depth", queueDepth,
			"event_count", eventCount)
	}

	resp := map[string]any{
		"events":  events,
		"last_id": lastID,
	}

	if !writeDebugPollJSON(w, resp, pollID, start, "after", pp.after, "limit", pp.effectiveLimit, "event_count", eventCount, "last_id", lastID, "queue_depth", queueDepth) {
		return
	}

	if laggingCursor || truncated || pollID%debugBridgePollSampleEvery == 0 {
		logger.Info("debug bridge: poll served",
			"poll_id", pollID,
			"after", pp.after,
			"limit", pp.effectiveLimit,
			"event_count", eventCount,
			"served_first_id", servedFirstID,
			"served_last_id", servedLastID,
			"last_id", lastID,
			"queue_depth", queueDepth,
			"lagging_cursor", laggingCursor,
			"truncated", truncated,
			"events_out_total", outTotal,
			logger.FieldDurationMS, time.Since(start).Milliseconds())
	}
}

// handleDebugSelectProjectDir 调试模式目录选择接口。
//
// POST /select-project-dir -> {"path":"..."}，取消返回空字符串。
func handleDebugSelectProjectDir(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info("debug: select project dir invoked")
	path, err := selectProjectDirNative()
	if err != nil {
		logger.Warn("debug: select project dir failed", logger.FieldError, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		}); err != nil {
			logger.Debug("debug: encode response", logger.FieldError, err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"path": path,
	}); err != nil {
		logger.Debug("debug: encode response", logger.FieldError, err)
	}
}

// handleDebugSelectFiles 调试模式文件选择接口。
//
// POST /select-files -> {"paths":["/a","/b"]}
func handleDebugSelectFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info("debug: select files invoked")
	paths, err := selectFilesNative()
	if err != nil {
		logger.Warn("debug: select files failed", logger.FieldError, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		}); err != nil {
			logger.Debug("debug: encode response", logger.FieldError, err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"paths": paths,
	}); err != nil {
		logger.Debug("debug: encode response", logger.FieldError, err)
	}
}

// handleDebugBuildInfo 返回当前构建信息。
func handleDebugBuildInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(currentBuildInfo()); err != nil {
		logger.Debug("debug: encode response", logger.FieldError, err)
	}
}

// selectProjectDirNative 打开系统目录选择对话框。
//
// 用户取消返回空字符串和 nil error。
func selectProjectDirNative() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		script := `try
POSIX path of (choose folder with prompt "选择项目目录")
on error number -128
return ""
end try`
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		if err != nil {
			detail := strings.TrimSpace(string(out))
			if detail != "" {
				return "", apperrors.Wrapf(err, "selectProjectDirNative", "osascript choose folder failed (%s)", detail)
			}
			return "", apperrors.Wrap(err, "selectProjectDirNative", "osascript choose folder failed")
		}
		return strings.TrimSpace(string(out)), nil
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms | Out-Null; $d = New-Object System.Windows.Forms.FolderBrowserDialog; if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){ Write-Output $d.SelectedPath }`
		out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", script).Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return "", nil
			}
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	default:
		for _, picker := range []struct {
			name string
			args []string
		}{
			{name: "zenity", args: []string{"--file-selection", "--directory", "--title=选择项目目录"}},
			{name: "kdialog", args: []string{"--getexistingdirectory", "~", "选择项目目录"}},
		} {
			out, err := exec.Command(picker.name, picker.args...).Output()
			if err == nil {
				return strings.TrimSpace(string(out)), nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				continue
			}
			if errors.Is(err, exec.ErrNotFound) {
				continue
			}
		}
		return "", apperrors.Newf("selectProjectDirNative", "no supported folder picker found on %s", runtime.GOOS)
	}
}

// selectFilesNative 打开系统文件选择对话框(支持多选)。
//
// 用户取消返回空数组和 nil error。
func selectFilesNative() ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		script := `try
set picked to choose file with prompt "选择附件文件" with multiple selections allowed
if class of picked is list then
set out to ""
repeat with f in picked
set out to out & POSIX path of f & linefeed
end repeat
return out
else
return POSIX path of picked
end if
on error number -128
return ""
end try`
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		if err != nil {
			detail := strings.TrimSpace(string(out))
			if detail != "" {
				return nil, apperrors.Wrapf(err, "selectFilesNative", "osascript choose file failed (%s)", detail)
			}
			return nil, apperrors.Wrap(err, "selectFilesNative", "osascript choose file failed")
		}
		return splitPickerOutput(string(out)), nil
	case "windows":
		script := `$ofd = New-Object System.Windows.Forms.OpenFileDialog; $ofd.Multiselect = $true; if($ofd.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){ $ofd.FileNames | ForEach-Object { Write-Output $_ } }`
		out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", script).Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return []string{}, nil
			}
			return nil, err
		}
		return splitPickerOutput(string(out)), nil
	default:
		for _, picker := range []struct {
			name string
			args []string
		}{
			{name: "zenity", args: []string{"--file-selection", "--multiple", "--separator=\n", "--title=选择附件文件"}},
			{name: "kdialog", args: []string{"--getopenfilename", "~", "*", "--multiple", "--separate-output"}},
		} {
			out, err := exec.Command(picker.name, picker.args...).Output()
			if err == nil {
				return splitPickerOutput(string(out)), nil
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				continue
			}
			if errors.Is(err, exec.ErrNotFound) {
				continue
			}
		}
		return nil, apperrors.Newf("selectFilesNative", "no supported file picker found on %s", runtime.GOOS)
	}
}

func splitPickerOutput(raw string) []string {
	lines := strings.Split(raw, "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// findFrontendDir 查找 frontend 目录 (相对于可执行文件或 cwd)。
func findFrontendDir() string {
	// 尝试 1: 相对于 cwd
	candidates := []string{
		"cmd/agent-terminal/frontend",
		"frontend",
	}

	// 尝试 2: 相对于可执行文件
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "frontend"),
			filepath.Join(exeDir, "cmd", "agent-terminal", "frontend"),
		)
	}

	// 尝试 3: 相对于源文件
	_, file, _, ok := runtime.Caller(0)
	if ok {
		srcDir := filepath.Dir(file)
		candidates = append(candidates, filepath.Join(srcDir, "frontend"))
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(dir)
			logger.Info("debug: frontend directory found", logger.FieldPath, abs)
			return abs
		}
	}

	return ""
}

// 确保 fs.FS 接口满足 (供 embed 使用)。
var _ fs.FS = assets

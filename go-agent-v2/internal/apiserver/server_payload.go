// server_payload.go — payload 提取、事件分发、通知广播、UI 状态同步 & HTTP-RPC 兼容层。
package apiserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// uiStateThrottleEntry 全局节流状态。
type uiStateThrottleEntry struct {
	lastEmit time.Time      // 上次实际发送时间
	timer    *time.Timer    // trailing timer (保证最终一致)
	pending  map[string]any // 最新 payload (合并)
}

// SetNotifyHook 设置 Notify 事件钩子。
//
// 用于桌面端桥接: apiserver 事件 -> Wails runtime event。
func (s *Server) SetNotifyHook(h func(method string, params any)) {
	s.notifyHookMu.Lock()
	s.notifyHook = h
	s.notifyHookMu.Unlock()
}

// Notify 向所有连接广播 JSON-RPC 通知 (WebSocket + SSE)。
func (s *Server) Notify(method string, params any) {
	s.syncUIRuntimeFromNotify(method, params)
	payload := util.ToMapAny(params)
	s.broadcastNotification(method, payload)

	if shouldEmitUIStateChanged(method, payload) {
		statePayload := map[string]any{"source": method}
		if tid, _ := payload["threadId"].(string); tid != "" {
			statePayload["threadId"] = tid
		}
		if aid, _ := payload["agent_id"].(string); aid != "" {
			statePayload["agent_id"] = aid
		}
		s.throttledUIStateChanged(statePayload)
	}
}

func shouldEmitUIStateChanged(method string, payload map[string]any) bool {
	if method == "" || method == "ui/state/changed" {
		return false
	}
	if strings.HasPrefix(method, "workspace/run/") {
		return true
	}
	threadID, _ := payload["threadId"].(string)
	if strings.TrimSpace(threadID) != "" {
		return true
	}
	agentID, _ := payload["agent_id"].(string)
	return strings.TrimSpace(agentID) != ""
}

// throttledUIStateChanged 全局节流发送 ui/state/changed。
//
// 使用全局统一节流 (不再 per-thread): 多 agent 并行时也只发一条,
// 前端只需要一个信号触发 syncRuntimeState 拉取最新快照。
func (s *Server) throttledUIStateChanged(payload map[string]any) {
	key := "_global"

	now := time.Now()
	interval := time.Duration(uiStateThrottleMs) * time.Millisecond

	s.uiThrottleMu.Lock()
	if s.uiThrottleEntries == nil {
		s.uiThrottleEntries = make(map[string]*uiStateThrottleEntry)
	}
	entry, ok := s.uiThrottleEntries[key]
	if !ok {
		entry = &uiStateThrottleEntry{}
		s.uiThrottleEntries[key] = entry
	}

	// 保存最新 payload (合并/覆盖)
	entry.pending = payload

	// 节流窗口内: 只安排 trailing timer
	if now.Sub(entry.lastEmit) < interval {
		if entry.timer == nil {
			entry.timer = time.AfterFunc(interval, func() {
				s.flushUIStateChanged(key)
			})
		}
		s.uiThrottleMu.Unlock()
		return
	}

	// 节流窗口外: 立即发送
	entry.lastEmit = now
	pending := entry.pending
	entry.pending = nil
	// 取消 trailing timer (刚发了, 不需要了)
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	s.uiThrottleMu.Unlock()

	s.broadcastNotification("ui/state/changed", pending)
}

// flushUIStateChanged trailing timer 回调: 发送最后一个 pending payload。
func (s *Server) flushUIStateChanged(key string) {
	s.uiThrottleMu.Lock()
	entry, ok := s.uiThrottleEntries[key]
	if !ok || entry.pending == nil {
		if ok {
			entry.timer = nil
		}
		s.uiThrottleMu.Unlock()
		return
	}
	entry.lastEmit = time.Now()
	pending := entry.pending
	entry.pending = nil
	entry.timer = nil
	s.uiThrottleMu.Unlock()

	s.broadcastNotification("ui/state/changed", pending)
}

func (s *Server) syncUIRuntimeFromNotify(method string, params any) {
	if s.uiRuntime == nil {
		return
	}
	payload := util.ToMapAny(params)
	switch method {
	case "workspace/run/created", "workspace/run/aborted":
		run := util.ToMapAny(payload["run"])
		if len(run) == 0 {
			return
		}
		s.uiRuntime.UpsertWorkspaceRun(run)
	case "workspace/run/merged":
		runKey, _ := payload["runKey"].(string)
		result := util.ToMapAny(payload["result"])
		if len(result) == 0 {
			return
		}
		s.uiRuntime.ApplyWorkspaceMergeResult(runKey, result)
	}
	if shouldReplayThreadNotifyToUIRuntime(method, payload) {
		threadID, _ := payload["threadId"].(string)
		normalized := uistate.NormalizeEventFromPayload(method, method, payload)
		s.uiRuntime.ApplyAgentEvent(strings.TrimSpace(threadID), normalized, payload)
	}
}

func shouldReplayThreadNotifyToUIRuntime(method string, payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if _, hasUIType := payload["uiType"]; hasUIType {
		return false
	}
	threadID, _ := payload["threadId"].(string)
	if strings.TrimSpace(threadID) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "turn/completed", "turn/aborted", "error", "codex/event/stream_error":
		return true
	default:
		return false
	}
}

var payloadExtractKeys = []string{
	// legacy fields
	"delta", "content", "message", "command",
	"cmd", "command_display", "commandDisplay", "displayCommand",
	"exit_code", "reason", "name", "status",
	"file", "files", "diff", "tool_name",
	"item", "process",
	"turn", "last_agent_message", "lastAgentMessage",
	// v2 protocol fields
	"text", "summary", "args", "arguments", "output",
	"id", "type", "item_id", "callId", "call_id",
	"file_path", "path", "chunk", "stream",
	"plan", "explanation",
	"phase", "recoverable", "willRetry", "will_retry",
	"additional_details", "additionalDetails",
	"threadId", "thread_id", "turnId", "turn_id",
	"activeTurnId", "active_turn_id", "attempt", "max_retries",
	"error",
	// token usage fields (keep nested shapes for runtime parser)
	"tokenUsage", "token_usage", "usage", "info",
	"total_tokens", "totalTokens", "used_tokens", "usedTokens",
	"input_tokens", "inputTokens", "output_tokens", "outputTokens",
	"context_window_tokens", "contextWindowTokens",
	"model_context_window", "modelContextWindow",
}

func parseMapAny(raw any) map[string]any {
	switch value := raw.(type) {
	case map[string]any:
		return value
	case string:
		var out map[string]any
		if json.Unmarshal([]byte(value), &out) == nil {
			return out
		}
	case json.RawMessage:
		var out map[string]any
		if json.Unmarshal(value, &out) == nil {
			return out
		}
	case []byte:
		var out map[string]any
		if json.Unmarshal(value, &out) == nil {
			return out
		}
	}
	return nil
}

func mergePayloadFromMap(payload map[string]any, data map[string]any) {
	if data == nil {
		return
	}

	for _, key := range payloadExtractKeys {
		v, ok := data[key]
		if !ok {
			continue
		}
		payload[key] = v
	}

	if v, ok := data["call_id"]; ok {
		if _, exists := payload["id"]; !exists {
			payload["id"] = v
		}
	}
	if v, ok := data["item_id"]; ok {
		if _, exists := payload["id"]; !exists {
			payload["id"] = v
		}
	}
	if v, ok := data["file_path"]; ok {
		if _, exists := payload["file"]; !exists {
			payload["file"] = v
		}
	}
	if v, ok := data["path"]; ok {
		if _, exists := payload["file"]; !exists {
			payload["file"] = v
		}
	}
	if errObj, ok := data["error"].(map[string]any); ok && errObj != nil {
		if _, exists := payload["message"]; !exists {
			if msg, ok := errObj["message"]; ok {
				payload["message"] = msg
			}
		}
		if _, exists := payload["additional_details"]; !exists {
			if details, ok := errObj["additional_details"]; ok {
				payload["additional_details"] = details
			} else if details, ok := errObj["additionalDetails"]; ok {
				payload["additional_details"] = details
			}
		}
	}
	if item := parseMapAny(data["item"]); item != nil {
		mergePayloadFromMap(payload, item)
	}
}

// walkNestedJSON 遍历 msg/data/payload 嵌套层, 对每个解析出的 map[string]any 调用 fn。
//
// 统一处理四种嵌套类型: map[string]any / string / json.RawMessage / []byte。
// mergePayloadFields 使用此逻辑。
func walkNestedJSON(m map[string]any, fn func(map[string]any)) {
	for _, key := range []string{"msg", "data", "payload"} {
		v, ok := m[key]
		if !ok {
			continue
		}
		switch nested := v.(type) {
		case map[string]any:
			fn(nested)
		case string:
			var nm map[string]any
			if json.Unmarshal([]byte(nested), &nm) == nil {
				fn(nm)
			}
		case json.RawMessage:
			var nm map[string]any
			if json.Unmarshal(nested, &nm) == nil {
				fn(nm)
			}
		case []byte:
			var nm map[string]any
			if json.Unmarshal(nested, &nm) == nil {
				fn(nm)
			}
		}
	}
}

func mergePayloadFields(payload map[string]any, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}

	var dataMap map[string]any
	if err := json.Unmarshal(raw, &dataMap); err != nil {
		return
	}

	mergePayloadFromMap(payload, dataMap)
	walkNestedJSON(dataMap, func(nested map[string]any) {
		mergePayloadFromMap(payload, nested)
	})
}

func normalizeFiles(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		value := strings.TrimSpace(v)
		if value == "" {
			return nil
		}
		return []string{value}
	case []string:
		return uniqueStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return uniqueStrings(out)
	default:
		return nil
	}
}

func uniqueStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		s := strings.TrimSpace(item)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func parseFilesFromPatchDelta(delta string) []string {
	if delta == "" {
		return nil
	}
	lines := strings.Split(delta, "\n")
	files := make([]string, 0, 4)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "diff --git ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 4 {
				path := strings.TrimPrefix(parts[3], "b/")
				if path != "" {
					files = append(files, path)
				}
			}
			continue
		}
		if len(trimmed) > 2 && (strings.HasPrefix(trimmed, "M ") || strings.HasPrefix(trimmed, "A ") || strings.HasPrefix(trimmed, "D ")) {
			path := strings.TrimSpace(trimmed[2:])
			if path != "" {
				files = append(files, path)
			}
		}
	}
	return uniqueStrings(files)
}

func toolResultSuccess(result string) bool {
	value := strings.TrimSpace(strings.ToLower(result))
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "error") ||
		strings.HasPrefix(value, "failed") ||
		strings.HasPrefix(value, "unknown tool") {
		return false
	}
	if strings.HasPrefix(value, `{"error"`) ||
		strings.Contains(value, `"error":`) {
		return false
	}
	return true
}

func (s *Server) rememberFileChanges(threadID string, files []string) {
	if threadID == "" {
		return
	}
	files = uniqueStrings(files)
	if len(files) == 0 {
		return
	}
	s.fileChangeMu.Lock()
	defer s.fileChangeMu.Unlock()
	s.fileChangeByThread[threadID] = files
}

func (s *Server) consumeRememberedFileChanges(threadID string) []string {
	if threadID == "" {
		return nil
	}
	s.fileChangeMu.Lock()
	defer s.fileChangeMu.Unlock()
	files := s.fileChangeByThread[threadID]
	delete(s.fileChangeByThread, threadID)
	return append([]string(nil), files...)
}

func (s *Server) enrichFileChangePayload(threadID, eventType, method string, payload map[string]any) {
	if payload == nil {
		return
	}
	isFileChangeEvent := strings.Contains(strings.ToLower(eventType), "filechange") ||
		strings.Contains(strings.ToLower(eventType), "patch_apply")
	isFileChangeMethod := strings.Contains(method, "fileChange")
	if !isFileChangeEvent && !isFileChangeMethod {
		return
	}

	files := normalizeFiles(payload["files"])
	if len(files) == 0 {
		files = normalizeFiles(payload["file"])
	}
	if len(files) == 0 {
		delta := ""
		if value, ok := payload["delta"].(string); ok {
			delta = value
		} else if value, ok := payload["output"].(string); ok {
			delta = value
		}
		files = parseFilesFromPatchDelta(delta)
	}

	switch method {
	case "item/fileChange/outputDelta", "item/started":
		if len(files) > 0 {
			payload["files"] = files
			payload["file"] = files[0]
			payload["type"] = "fileChange"
			s.rememberFileChanges(threadID, files)
		}
	case "item/completed":
		if len(files) == 0 {
			files = s.consumeRememberedFileChanges(threadID)
		}
		if len(files) > 0 {
			payload["files"] = files
			payload["file"] = files[0]
			payload["type"] = "fileChange"
		}
	}
}

// AgentEventHandler 返回一个 codex.EventHandler，将 Agent 事件转为 JSON-RPC 通知/请求。
//
// 普通事件: 广播为通知 (无需客户端回复)。
// 审批事件: 发送 Server→Client 请求, 等待客户端回复, 回传 codex (§ 二)。
func (s *Server) AgentEventHandler(agentID string) codex.EventHandler {
	return func(event codex.Event) {
		method := mapEventToMethod(event.Type)

		// 统一日志: 记录所有 codex 事件
		threadID := ""
		if proc := s.mgr.Get(agentID); proc != nil {
			threadID = proc.Client.GetThreadID()
		}
		logger.Debug("codex event",
			logger.FieldSource, "codex",
			logger.FieldComponent, "event",
			logger.FieldAgentID, agentID,
			logger.FieldThreadID, threadID,
			logger.FieldEventType, event.Type,
		)

		// 构建通知参数: threadId 始终在顶层以便前端路由
		payload := map[string]any{
			"threadId": agentID,
		}

		// 从 event.Data 提取前端常用字段到顶层 (含嵌套 msg/data/payload)。
		mergePayloadFields(payload, event.Data)

		// mergePayloadFields 可能用 Codex 原始 threadId (UUID) 覆盖了 agentID,
		// 前端 ConversationManager 使用 Go agentID (thread-*) 作为 key, 必须还原。
		if rawTID, _ := payload["threadId"].(string); rawTID != "" && rawTID != agentID {
			payload["codexThreadId"] = rawTID
		}
		payload["threadId"] = agentID
		s.enrichFileChangePayload(agentID, event.Type, method, payload)
		s.captureAndInjectTurnSummary(agentID, event.Type, method, payload)
		if method == "error" {
			willRetry, hasWillRetry := extractBoolFromPayload(payload, "willRetry", "will_retry", "recoverable")
			if !hasWillRetry {
				willRetry = strings.EqualFold(strings.TrimSpace(event.Type), codex.EventStreamError)
			}
			payload["willRetry"] = willRetry
			payload["will_retry"] = willRetry
			if _, exists := payload["error"]; !exists {
				payload["error"] = map[string]any{
					"message":           extractFirstString(payload, "message", "reason"),
					"additionalDetails": extractFirstString(payload, "additional_details", "additionalDetails"),
				}
			}
		}

		// Normalize event for UI
		normalized := uistate.NormalizeEventFromPayload(event.Type, method, payload)
		payload["uiType"] = string(normalized.UIType)
		if normalized.Text != "" {
			payload["uiText"] = normalized.Text
		}
		if normalized.Command != "" {
			payload["uiCommand"] = normalized.Command
		}
		if len(normalized.Files) > 0 {
			payload["uiFiles"] = normalized.Files
		}
		if normalized.ExitCode != nil {
			payload["uiExitCode"] = *normalized.ExitCode
		}
		if s.uiRuntime != nil {
			s.uiRuntime.ApplyAgentEvent(agentID, normalized, payload)
		}

		s.touchTrackedTurnLastEvent(agentID)
		s.maybeFinalizeTrackedTurn(agentID, event.Type, method, payload)
		s.maybeAutoReportOrchestrationCompletion(agentID, event.Type, method, payload)

		// § 二 审批事件: 需要客户端回复 (双向请求)
		switch event.Type {
		case "exec_approval_request":
			util.SafeGo(func() { s.handleApprovalRequest(agentID, "item/commandExecution/requestApproval", payload, event) })
			return
		case "file_change_approval_request":
			util.SafeGo(func() { s.handleApprovalRequest(agentID, "item/fileChange/requestApproval", payload, event) })
			return
		case codex.EventDynamicToolCall:
			util.SafeGo(func() { s.handleDynamicToolCall(agentID, event) })
			return
		}

		// 普通事件: 广播通知
		s.Notify(method, payload)
	}
}

// ========================================
// HTTP JSON-RPC (调试模式)
// ========================================

// handleHTTPRPC 处理 HTTP POST /rpc 请求 (调试模式用)。
//
// 接收标准 JSON-RPC 2.0 请求, 调用 InvokeMethod, 返回 JSON-RPC 响应。
func (s *Server) handleHTTPRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, -32700, "Parse error: "+err.Error())
		return
	}

	if req.Method == "" {
		writeJSONRPCError(w, req.ID, -32600, "Invalid Request: method is required")
		return
	}

	// 如果 params 为 null, 用空对象
	params := req.Params
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage("{}")
	}

	result, err := s.InvokeMethod(r.Context(), req.Method, params)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, err.Error())
		return
	}

	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"result":  result,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Debug("http-rpc: encode response", logger.FieldError, err)
	}
}

// writeJSONRPCError 写 JSON-RPC 错误响应。
func writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC 错误仍返回 200
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Debug("http-rpc: encode error response", logger.FieldError, err)
	}
}

// recoveryMiddleware 捕获 HTTP handler panic，防止单个请求崩溃导致整个服务端退出。
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				logger.Error("http: handler panicked",
					logger.FieldMethod, r.Method,
					logger.FieldPath, r.URL.Path,
					logger.FieldRemote, r.RemoteAddr,
					logger.FieldError, rv,
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware 添加 CORS 头 (调试模式允许跨域)。
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleSSE 处理 SSE 事件流 (debug 模式浏览器实时接收 agent 事件)。
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 64)

	s.sseMu.Lock()
	s.sseClients[ch] = struct{}{}
	s.sseMu.Unlock()

	defer func() {
		s.sseMu.Lock()
		delete(s.sseClients, ch)
		s.sseMu.Unlock()
	}()

	logger.Info("sse: client connected", logger.FieldRemote, r.RemoteAddr)

	for {
		select {
		case data := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			logger.Info("sse: client disconnected", logger.FieldRemote, r.RemoteAddr)
			return
		}
	}
}

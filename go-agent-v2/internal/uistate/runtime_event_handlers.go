// runtime_event_handlers.go — 事件处理器、overlay 与状态派生逻辑。
package uistate

import (
	"fmt"
	"strings"
	"time"
)

type resolvedFields struct {
	text     string
	command  string
	file     string
	files    []string
	exitCode *int
	planDone *bool
	planSet  bool
}

type runtimeEventHandler func(*RuntimeManager, string, resolvedFields, map[string]any, time.Time)

var runtimeEventHandlers = map[UIType]runtimeEventHandler{
	UITypeTurnStarted:     handleTurnStartedEvent,
	UITypeTurnComplete:    handleTurnCompleteEvent,
	UITypeAssistantDelta:  handleAssistantDeltaEvent,
	UITypeAssistantDone:   handleAssistantDoneEvent,
	UITypeReasoningDelta:  handleReasoningDeltaEvent,
	UITypeCommandStart:    handleCommandStartEvent,
	UITypeCommandOutput:   handleCommandOutputEvent,
	UITypeCommandDone:     handleCommandDoneEvent,
	UITypeFileEditStart:   handleFileEditStartEvent,
	UITypeFileEditDone:    handleFileEditDoneEvent,
	UITypeToolCall:        handleToolCallEvent,
	UITypeApprovalRequest: handleApprovalRequestEvent,
	UITypePlanDelta:       handlePlanDeltaEvent,
	UITypeDiffUpdate:      handleDiffUpdateEvent,
	UITypeUserMessage:     handleUserMessageEvent,
	UITypeError:           handleErrorEvent,
}

func resolveEventFields(normalized NormalizedEvent, payload map[string]any) resolvedFields {
	fields := resolvedFields{
		text:    strings.TrimSpace(normalized.Text),
		command: strings.TrimSpace(normalized.Command),
		file:    strings.TrimSpace(normalized.File),
		files:   nil,
	}
	if fields.text == "" {
		fields.text = extractFirstString(payload, "uiText", "delta", "text", "content", "output", "message")
	}
	if fields.command == "" {
		fields.command = extractFirstString(payload, "uiCommand", "command")
	}
	if fields.file == "" {
		fields.file = extractFirstString(payload, "uiFile", "file")
	}
	fields.files = normalizeFilesAny(payload["uiFiles"])
	if len(fields.files) == 0 {
		fields.files = normalizeFilesAny(payload["files"])
	}
	if len(fields.files) == 0 && len(normalized.Files) > 0 {
		fields.files = append(fields.files, normalized.Files...)
	}
	if fields.file != "" && len(fields.files) == 0 {
		fields.files = []string{fields.file}
	}
	fields.exitCode = normalized.ExitCode
	if fields.exitCode != nil {
		return fields
	}
	if code, ok := extractExitCode(payload["uiExitCode"]); ok {
		fields.exitCode = &code
		return fields
	}
	if code, ok := extractExitCode(payload["exit_code"]); ok {
		fields.exitCode = &code
	}
	if planText, planDone, ok := extractPlanSnapshot(payload); ok {
		fields.text = planText
		fields.planSet = true
		fields.planDone = &planDone
	}
	return fields
}

func (m *RuntimeManager) applyAgentEventLocked(threadID string, normalized NormalizedEvent, payload map[string]any, ts time.Time) {
	m.markAgentActiveLocked(threadID, ts)
	rt := m.runtime[threadID]
	rt.hasDerivedState = true
	fields := resolveEventFields(normalized, payload)
	m.applyLifecycleStateLocked(threadID, normalized, payload, fields, ts)
	if handler, ok := runtimeEventHandlers[normalized.UIType]; ok {
		handler(m, threadID, fields, payload, ts)
	}
	nextState := m.deriveThreadStateLocked(threadID)
	m.setThreadStateLocked(threadID, nextState)
	m.snapshot.StatusHeadersByThread[threadID] = m.deriveThreadStatusHeaderLocked(threadID, nextState)
	m.snapshot.StatusDetailsByThread[threadID] = m.deriveThreadStatusDetailsLocked(threadID, nextState)
}

func (m *RuntimeManager) applyLifecycleStateLocked(threadID string, normalized NormalizedEvent, payload map[string]any, fields resolvedFields, ts time.Time) {
	rt := m.runtime[threadID]
	eventType := strings.ToLower(strings.TrimSpace(normalized.RawType))
	method := strings.TrimSpace(normalized.Method)

	m.applyErrorOverlayLocked(rt, threadID, normalized.UIType, eventType, fields.text, payload)
	applyOverlays(rt, eventType, method, payload)
	applyCollabDepth(rt, eventType)

	if eventType == "token_count" || eventType == "context_compacted" || method == "thread/tokenUsage/updated" || method == "thread/compacted" {
		m.updateTokenUsageLocked(threadID, payload, eventType, method, ts)
	}
	if isThreadStatusChangedEvent(eventType, method) {
		m.applyThreadStatusChangedLocked(threadID, payload)
	}

	m.applyUITypeDepthsLocked(threadID, rt, normalized.UIType, eventType, method, fields.text)
}

// applyErrorOverlayLocked handles the 3-way error overlay branch:
//   - UITypeError → set error text (and details + alert for stream_error)
//   - non-Error AND non-stream_error → clear error state
//   - non-Error BUT stream_error → keep existing error state
func (m *RuntimeManager) applyErrorOverlayLocked(rt *threadRuntime, threadID string, uiType UIType, eventType, text string, payload map[string]any) {
	if uiType == UITypeError {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			trimmed = "发生错误"
		}
		rt.streamErrorText = trimmed
		if eventType == "stream_error" {
			rt.streamErrorDetails = deriveStreamErrorDetails(payload)
			m.pushAlertLocked(threadID, "error", trimmed)
		} else {
			rt.streamErrorDetails = ""
		}
	} else if eventType != "stream_error" {
		rt.streamErrorText = ""
		rt.streamErrorDetails = ""
	}
}

// applyOverlays updates terminal-wait, MCP-startup, and background overlays.
func applyOverlays(rt *threadRuntime, eventType, method string, payload map[string]any) {
	if isTerminalInteractionEvent(eventType, method) {
		if isTerminalWaitPayload(payload) {
			rt.terminalWaitOverlay = true
			rt.terminalWaitLabel = deriveTerminalWaitLabel(payload)
		} else {
			rt.terminalWaitOverlay = false
			rt.terminalWaitLabel = ""
		}
	}
	if isMCPStartupUpdateEvent(eventType, method) {
		rt.mcpStartupOverlay = true
		rt.mcpStartupLabel = deriveMCPStartupLabel(payload)
	}
	if isMCPStartupCompleteEvent(eventType, method) {
		rt.mcpStartupOverlay = false
		rt.mcpStartupLabel = ""
	}
	if isBackgroundEvent(eventType, method) {
		if shouldClearBackgroundOverlay(payload) {
			rt.backgroundOverlay = false
			rt.backgroundLabel = ""
			rt.backgroundDetails = ""
		} else {
			rt.backgroundOverlay = true
			rt.backgroundLabel = deriveBackgroundLabel(payload)
			rt.backgroundDetails = deriveBackgroundDetails(payload)
		}
	}
}

// applyCollabDepth adjusts the collaboration depth counter.
func applyCollabDepth(rt *threadRuntime, eventType string) {
	if eventType == "collab_agent_spawn_begin" || eventType == "collab_agent_interaction_begin" || eventType == "collab_waiting_begin" {
		rt.collabDepth += 1
	} else if eventType == "collab_agent_spawn_end" || eventType == "collab_agent_interaction_end" || eventType == "collab_waiting_end" {
		rt.collabDepth = max(0, rt.collabDepth-1)
	}
}

// applyUITypeDepthsLocked updates turn/command/edit/approval/tool-call depth
// counters based on the UIType. Must be called with m.mu held.
func (m *RuntimeManager) applyUITypeDepthsLocked(threadID string, rt *threadRuntime, uiType UIType, eventType, method, text string) {
	switch uiType {
	case UITypeTurnStarted:
		m.clearTurnLifecycleLocked(threadID)
		rt.turnDepth = 1
		rt.approvalDepth = 0
		rt.userInputDepth = 0
		rt.terminalWaitOverlay = false
		rt.terminalWaitLabel = ""
		rt.statusHeader = "工作中"
	case UITypeTurnComplete:
		m.clearTurnLifecycleLocked(threadID)
		// mcp_startup_complete 为主清理信号，但历史/重连链路可能丢失 complete，
		// turn 收敛时兜底清理，避免 "MCP 启动中" 状态残留。
		rt.mcpStartupOverlay = false
		rt.mcpStartupLabel = ""
	case UITypeReasoningDelta:
		if rt.turnDepth == 0 {
			rt.turnDepth = 1
		}
		if isReasoningSectionBreakEvent(eventType, method) {
			rt.reasoningHeaderBuf = ""
		}
		m.captureReasoningHeaderLocked(threadID, text)
	case UITypeCommandStart:
		rt.commandDepth += 1
		rt.approvalDepth = 0
		rt.terminalWaitOverlay = false
		rt.terminalWaitLabel = ""
		m.incrActivityStatLocked(threadID, "command", "")
	case UITypeCommandOutput:
		if rt.commandDepth == 0 {
			rt.commandDepth = 1
		}
		rt.terminalWaitOverlay = false
		rt.terminalWaitLabel = ""
	case UITypeCommandDone:
		rt.commandDepth = max(0, rt.commandDepth-1)
		rt.terminalWaitOverlay = false
		rt.terminalWaitLabel = ""
	case UITypeFileEditStart:
		rt.fileEditDepth += 1
		rt.approvalDepth = 0
		m.incrActivityStatLocked(threadID, "fileEdit", "")
	case UITypeFileEditDone:
		rt.fileEditDepth = max(0, rt.fileEditDepth-1)
	case UITypeApprovalRequest:
		rt.approvalDepth += 1
	case UITypeToolCall:
		if eventType == "mcp_tool_call_begin" {
			rt.toolCallDepth += 1
			toolName := ""
			if text != "" {
				toolName = text
			}
			m.incrActivityStatLocked(threadID, "toolCall", toolName)
		}
		if eventType == "mcp_tool_call_end" {
			rt.toolCallDepth = max(0, rt.toolCallDepth-1)
		}
	}
}

func (m *RuntimeManager) clearTurnLifecycleLocked(threadID string) {
	rt := m.runtime[threadID]
	rt.turnDepth = 0
	rt.approvalDepth = 0
	rt.userInputDepth = 0
	rt.commandDepth = 0
	rt.fileEditDepth = 0
	rt.toolCallDepth = 0
	rt.collabDepth = 0
	rt.terminalWaitOverlay = false
	rt.terminalWaitLabel = ""
	rt.backgroundOverlay = false
	rt.backgroundLabel = ""
	rt.backgroundDetails = ""
	rt.streamErrorText = ""
	rt.streamErrorDetails = ""
	rt.statusHeader = ""
	rt.reasoningHeaderBuf = ""
}

// incrActivityStatLocked increments a per-thread activity counter.
// Must be called with m.mu held.
func (m *RuntimeManager) incrActivityStatLocked(threadID, kind, toolName string) {
	stats, ok := m.snapshot.ActivityStatsByThread[threadID]
	if !ok {
		stats = ActivityStats{ToolCalls: map[string]int64{}}
	}
	switch kind {
	case "command":
		stats.Commands++
	case "fileEdit":
		stats.FileEdits++
	case "toolCall":
		name := toolName
		if name == "" {
			name = "unknown"
		}
		if stats.ToolCalls == nil {
			stats.ToolCalls = map[string]int64{}
		}
		stats.ToolCalls[name]++
		if strings.HasPrefix(name, "lsp_") || strings.HasPrefix(name, "lsp/") {
			stats.LSPCalls++
		}
	}
	m.snapshot.ActivityStatsByThread[threadID] = stats
}

// IncrActivityStat increments a per-thread activity counter (goroutine-safe).
//
// Used by apiserver for dynamic tool calls that bypass the normal event pipeline.
func (m *RuntimeManager) IncrActivityStat(threadID, kind, toolName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.incrActivityStatLocked(threadID, kind, toolName)
}

// PushAlert appends a high-priority alert for the given thread.
func (m *RuntimeManager) PushAlert(threadID, level, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pushAlertLocked(threadID, level, message)
}

// pushAlertLocked appends an alert; must be called with m.mu held.
func (m *RuntimeManager) pushAlertLocked(threadID, level, message string) {
	alerts := m.snapshot.AlertsByThread[threadID]
	entry := AlertEntry{
		ID:      fmt.Sprintf("a-%d", m.seq),
		Time:    time.Now().Format("15:04"),
		Level:   level,
		Message: message,
	}
	alerts = append(alerts, entry)
	// 保留最近 20 条
	if len(alerts) > 20 {
		alerts = alerts[len(alerts)-20:]
	}
	m.snapshot.AlertsByThread[threadID] = alerts
	m.seq++
}

func (m *RuntimeManager) deriveThreadStateLocked(threadID string) string {
	rt := m.runtime[threadID]
	switch {
	case strings.TrimSpace(rt.streamErrorText) != "":
		return "error"
	case rt.terminalWaitOverlay:
		return "waiting"
	case rt.userInputDepth > 0:
		return "waiting"
	case rt.approvalDepth > 0:
		return "waiting"
	case rt.fileEditDepth > 0:
		return "editing"
	case rt.commandDepth > 0 || rt.toolCallDepth > 0 || rt.collabDepth > 0:
		return "running"
	case rt.turnDepth > 0:
		return "thinking"
	case rt.mcpStartupOverlay:
		return "syncing"
	default:
		return "idle"
	}
}

func (m *RuntimeManager) deriveThreadStatusHeaderLocked(threadID, state string) string {
	rt := m.runtime[threadID]
	switch {
	case strings.TrimSpace(rt.streamErrorText) != "":
		return rt.streamErrorText
	case rt.terminalWaitOverlay:
		if strings.TrimSpace(rt.terminalWaitLabel) != "" {
			return rt.terminalWaitLabel
		}
		return "等待后台终端"
	case shouldShowMCPStartupOverlay(rt):
		if strings.TrimSpace(rt.mcpStartupLabel) != "" {
			return rt.mcpStartupLabel
		}
		return "MCP 启动中"
	case rt.backgroundOverlay:
		if strings.TrimSpace(rt.backgroundLabel) != "" {
			return rt.backgroundLabel
		}
		return "后台处理中"
	case rt.userInputDepth > 0:
		return "等待输入"
	case rt.approvalDepth > 0:
		return "等待确认"
	case shouldUseReasoningHeader(rt):
		return rt.statusHeader
	}
	switch state {
	case "running":
		return "工作中"
	case "editing":
		return "工作中"
	case "thinking":
		return "工作中"
	case "responding":
		return "工作中"
	case "starting":
		return "启动中"
	case "waiting":
		return "等待确认"
	case "syncing":
		return "同步中"
	case "error":
		return "异常"
	default:
		return "等待指示"
	}
}

func defaultStatusHeaderForState(state string) string {
	switch normalizeThreadState(state) {
	case "starting":
		return "启动中"
	case "waiting":
		return "等待确认"
	case "syncing":
		return "同步中"
	case "error":
		return "异常"
	case "running", "editing", "thinking", "responding":
		return "工作中"
	default:
		return "等待指示"
	}
}

func (m *RuntimeManager) deriveThreadStatusDetailsLocked(threadID, state string) string {
	rt := m.runtime[threadID]
	switch {
	case strings.TrimSpace(rt.streamErrorText) != "":
		return strings.TrimSpace(rt.streamErrorDetails)
	case rt.terminalWaitOverlay:
		return "命令正在等待终端输入"
	case shouldShowMCPStartupOverlay(rt):
		return "正在初始化 MCP 服务"
	case rt.backgroundOverlay:
		if strings.TrimSpace(rt.backgroundDetails) != "" {
			return rt.backgroundDetails
		}
		return "后台事件处理中"
	case rt.userInputDepth > 0:
		return "等待用户输入后继续"
	case rt.approvalDepth > 0:
		return "等待用户审批后继续"
	case shouldUseReasoningHeader(rt):
		return "根据推理标题展示当前阶段"
	}
	switch state {
	case "running":
		return "命令或工具正在执行"
	case "editing":
		return "文件修改进行中"
	case "thinking":
		return "模型推理中"
	case "syncing":
		return "后台同步中"
	case "error":
		return "运行出现异常"
	default:
		return ""
	}
}

func shouldShowMCPStartupOverlay(rt *threadRuntime) bool {
	if rt == nil || !rt.mcpStartupOverlay {
		return false
	}
	return rt.turnDepth == 0 &&
		rt.approvalDepth == 0 &&
		rt.userInputDepth == 0 &&
		rt.commandDepth == 0 &&
		rt.fileEditDepth == 0 &&
		rt.toolCallDepth == 0 &&
		rt.collabDepth == 0
}

func isThreadStatusChangedEvent(eventType, method string) bool {
	return eventType == "thread/status/changed" || strings.EqualFold(method, "thread/status/changed")
}

func (m *RuntimeManager) applyThreadStatusChangedLocked(threadID string, payload map[string]any) {
	rt := m.runtime[threadID]
	if rt == nil {
		return
	}

	statusType := ""
	activeFlags := []string{}
	switch status := payload["status"].(type) {
	case string:
		statusType = strings.ToLower(strings.TrimSpace(status))
	case map[string]any:
		statusType = strings.ToLower(strings.TrimSpace(extractFirstString(status, "type")))
		activeFlags = extractStringList(status["activeFlags"])
		if len(activeFlags) == 0 {
			activeFlags = extractStringList(status["active_flags"])
		}
	}
	if statusType == "" {
		return
	}

	switch statusType {
	case "active":
		if rt.turnDepth == 0 {
			rt.turnDepth = 1
		}
		rt.approvalDepth = 0
		rt.userInputDepth = 0
		for _, flag := range activeFlags {
			switch strings.ToLower(strings.TrimSpace(flag)) {
			case "waitingonapproval":
				rt.approvalDepth = 1
			case "waitingonuserinput":
				rt.userInputDepth = 1
			}
		}
	case "idle":
		m.clearTurnLifecycleLocked(threadID)
	case "systemerror", "system_error", "error":
		m.clearTurnLifecycleLocked(threadID)
		rt.streamErrorText = "系统异常"
		rt.streamErrorDetails = "线程状态变为 systemError"
	case "notloaded", "not_loaded":
		m.clearTurnLifecycleLocked(threadID)
	}
}

func extractStringList(raw any) []string {
	switch value := raw.(type) {
	case []string:
		items := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(item)
			if text != "" {
				items = append(items, text)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				trimmed := strings.TrimSpace(text)
				if trimmed != "" {
					items = append(items, trimmed)
				}
			}
		}
		return items
	default:
		return nil
	}
}

func isTerminalInteractionEvent(eventType, method string) bool {
	return eventType == "exec_terminal_interaction" || eventType == "item/commandexecution/terminalinteraction" || strings.EqualFold(method, "item/commandExecution/terminalInteraction")
}

func isMCPStartupUpdateEvent(eventType, method string) bool {
	return eventType == "mcp_startup_update" ||
		eventType == "codex/event/mcp_startup_update" ||
		eventType == "agent/event/mcp_startup_update" ||
		strings.EqualFold(method, "codex/event/mcp_startup_update") ||
		strings.EqualFold(method, "agent/event/mcp_startup_update")
}

func isMCPStartupCompleteEvent(eventType, method string) bool {
	return eventType == "mcp_startup_complete" ||
		eventType == "codex/event/mcp_startup_complete" ||
		eventType == "agent/event/mcp_startup_complete" ||
		strings.EqualFold(method, "codex/event/mcp_startup_complete") ||
		strings.EqualFold(method, "agent/event/mcp_startup_complete")
}

func isTerminalWaitPayload(payload map[string]any) bool {
	if payload == nil {
		return true
	}
	if value, ok := payload["stdin"]; ok {
		switch v := value.(type) {
		case nil:
			return true
		case string:
			return strings.TrimSpace(v) == ""
		case []any:
			return len(v) == 0
		case []string:
			return len(v) == 0
		default:
			return false
		}
	}
	return true
}

func deriveTerminalWaitLabel(payload map[string]any) string {
	command := strings.TrimSpace(extractFirstString(payload, "command", "command_display", "displayCommand"))
	if command == "" {
		command = strings.TrimSpace(extractNestedFirstString(payload, []string{"process", "command_display"}, []string{"process", "command"}))
	}
	if command == "" {
		return "等待后台终端"
	}
	return "等待后台终端 · " + command
}

func deriveMCPStartupLabel(payload map[string]any) string {
	server := strings.TrimSpace(extractFirstString(payload, "server", "name"))
	if server == "" {
		server = strings.TrimSpace(extractNestedFirstString(payload, []string{"status", "server"}, []string{"msg", "server"}))
	}
	if server == "" {
		return "MCP 启动中"
	}
	return "MCP 启动中 · " + server
}

func isReasoningSectionBreakEvent(eventType, method string) bool {
	return eventType == "agent_reasoning_section_break" ||
		strings.EqualFold(method, "agent/reasoningSectionBreak")
}

func isBackgroundEvent(eventType, method string) bool {
	return eventType == "background_event" ||
		eventType == "codex/event/background_event" ||
		strings.EqualFold(method, "codex/event/background_event")
}

func shouldClearBackgroundOverlay(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if done, ok := payload["done"].(bool); ok {
		return done
	}
	if active, ok := payload["active"].(bool); ok {
		return !active
	}
	status := strings.ToLower(strings.TrimSpace(extractFirstString(payload, "status", "state", "phase")))
	switch status {
	case "done", "completed", "complete", "finished", "success", "succeeded", "idle", "stopped", "closed", "ended":
		return true
	default:
		return false
	}
}

func deriveBackgroundLabel(payload map[string]any) string {
	text := extractFirstString(payload, "uiHeader", "statusHeader", "title", "event", "name")
	if strings.TrimSpace(text) == "" {
		text = extractFirstString(payload, "message", "text", "content")
	}
	if strings.TrimSpace(text) == "" {
		text = extractNestedFirstString(
			payload,
			[]string{"msg", "title"},
			[]string{"msg", "text"},
			[]string{"data", "title"},
			[]string{"data", "text"},
		)
	}
	text = compactOneLine(text, 48)
	if text == "" {
		return "后台处理中"
	}
	if strings.HasPrefix(text, "后台") {
		return text
	}
	return "后台处理中 · " + text
}

func deriveBackgroundDetails(payload map[string]any) string {
	text := extractFirstString(payload, "details", "detail", "description", "message", "text", "content")
	if strings.TrimSpace(text) == "" {
		text = extractNestedFirstString(
			payload,
			[]string{"msg", "details"},
			[]string{"msg", "text"},
			[]string{"data", "details"},
			[]string{"data", "text"},
		)
	}
	text = compactOneLine(text, 120)
	if text == "" {
		return "后台事件处理中"
	}
	return text
}

func deriveStreamErrorDetails(payload map[string]any) string {
	text := extractFirstString(payload, "additional_details", "additionalDetails", "details")
	if strings.TrimSpace(text) == "" {
		text = extractNestedFirstString(
			payload,
			[]string{"msg", "additional_details"},
			[]string{"msg", "details"},
			[]string{"data", "additional_details"},
			[]string{"data", "details"},
		)
	}
	return compactOneLine(text, 180)
}

func shouldUseReasoningHeader(rt *threadRuntime) bool {
	if rt == nil {
		return false
	}
	if strings.TrimSpace(rt.statusHeader) == "" {
		return false
	}
	if rt.turnDepth <= 0 {
		return false
	}
	if rt.userInputDepth > 0 || rt.approvalDepth > 0 || rt.commandDepth > 0 || rt.fileEditDepth > 0 || rt.toolCallDepth > 0 || rt.collabDepth > 0 {
		return false
	}
	if rt.terminalWaitOverlay || rt.mcpStartupOverlay || rt.backgroundOverlay {
		return false
	}
	return strings.TrimSpace(rt.streamErrorText) == ""
}

func (m *RuntimeManager) captureReasoningHeaderLocked(threadID, delta string) {
	rt := m.runtime[threadID]
	if rt == nil {
		return
	}
	header, buf := extractReasoningHeader(rt.reasoningHeaderBuf, delta)
	rt.reasoningHeaderBuf = buf
	if strings.TrimSpace(header) == "" {
		return
	}
	rt.statusHeader = header
}

func extractReasoningHeader(buffer, delta string) (string, string) {
	merged := buffer + delta
	merged = compactOneLine(merged, 512)
	if merged == "" {
		return "", ""
	}
	start := strings.Index(merged, "**")
	if start < 0 {
		return "", merged
	}
	rest := merged[start+2:]
	end := strings.Index(rest, "**")
	if end < 0 {
		return "", merged[start:]
	}
	header := compactOneLine(rest[:end], 80)
	if header == "" {
		return "", compactOneLine(rest[end+2:], 512)
	}
	return header, ""
}

func compactOneLine(text string, limit int) string {
	cleaned := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if cleaned == "" {
		return ""
	}
	if limit <= 0 {
		return cleaned
	}
	runes := []rune(cleaned)
	if len(runes) <= limit {
		return cleaned
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func handleTurnStartedEvent(m *RuntimeManager, threadID string, _ resolvedFields, _ map[string]any, ts time.Time) {
	m.completeTurnLocked(threadID, ts)
	m.startThinkingLocked(threadID, ts)
}

func handleTurnCompleteEvent(m *RuntimeManager, threadID string, _ resolvedFields, _ map[string]any, ts time.Time) {
	m.completeTurnLocked(threadID, ts)
}

func handleAssistantDeltaEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.appendAssistantLocked(threadID, fields.text, ts)
}

func handleAssistantDoneEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	doneText := strings.TrimSpace(fields.text)
	if doneText != "" {
		backfillText := doneText
		shouldBackfill := false

		rt := m.runtime[threadID]
		if rt == nil || rt.assistantIndex < 0 {
			shouldBackfill = true
		} else {
			timeline := m.timelineLocked(threadID)
			idx := rt.assistantIndex
			if idx < 0 || idx >= len(timeline) {
				shouldBackfill = true
			} else {
				current := strings.TrimSpace(timeline[idx].Text)
				if current == "" {
					shouldBackfill = true
				} else if idx != len(timeline)-1 {
					if doneText == current {
						backfillText = ""
					} else if strings.HasPrefix(doneText, current) {
						backfillText = strings.TrimSpace(strings.TrimPrefix(doneText, current))
					}
					if backfillText != "" {
						m.finishAssistantLocked(threadID)
						shouldBackfill = true
					}
				}
			}
		}

		if shouldBackfill && backfillText != "" {
			m.appendAssistantLocked(threadID, backfillText, ts)
		}
	}
	m.finishAssistantLocked(threadID)
}

func handleReasoningDeltaEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.appendThinkingLocked(threadID, fields.text, ts)
}

func handleCommandStartEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.startCommandLocked(threadID, fields.command, ts)
}

func handleCommandOutputEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.appendCommandOutputLocked(threadID, fields.text, ts)
}

func handleCommandDoneEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, _ time.Time) {
	m.finishCommandLocked(threadID, fields.exitCode)
}

func handleFileEditStartEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	for _, file := range fields.files {
		m.fileEditingLocked(threadID, file, ts)
	}
	m.rememberEditingFilesLocked(threadID, fields.files)
}

func handleFileEditDoneEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	saved := fields.files
	if len(saved) == 0 {
		saved = m.consumeEditingFilesLocked(threadID)
	}
	for _, file := range saved {
		m.fileSavedLocked(threadID, file, ts)
	}
}

func handleToolCallEvent(m *RuntimeManager, threadID string, _ resolvedFields, payload map[string]any, ts time.Time) {
	m.appendToolCallLocked(threadID, payload, ts)
}

func handleApprovalRequestEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.showApprovalLocked(threadID, fields.command, ts)
}

func handlePlanDeltaEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	if fields.planSet {
		planDone := false
		if fields.planDone != nil {
			planDone = *fields.planDone
		}
		m.setPlanLocked(threadID, fields.text, planDone, ts)
		return
	}
	m.appendPlanLocked(threadID, fields.text, ts)
}

func handleDiffUpdateEvent(m *RuntimeManager, threadID string, _ resolvedFields, payload map[string]any, _ time.Time) {
	diff := extractFirstString(payload, "diff", "uiText", "text", "content")
	m.snapshot.DiffTextByThread[threadID] = diff
}

func handleUserMessageEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.appendUserLocked(threadID, fields.text, nil, ts)
}

func handleErrorEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	text := fields.text
	if text == "" {
		text = "发生错误"
	}
	m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind: "error",
		Text: text,
	}, ts)
}

func (m *RuntimeManager) ensureThreadLocked(threadID string) {
	if m.snapshot.StatusHeadersByThread == nil {
		m.snapshot.StatusHeadersByThread = map[string]string{}
	}
	if m.snapshot.StatusDetailsByThread == nil {
		m.snapshot.StatusDetailsByThread = map[string]string{}
	}
	if m.snapshot.TokenUsageByThread == nil {
		m.snapshot.TokenUsageByThread = map[string]TokenUsageSnapshot{}
	}
	if _, ok := m.snapshot.TimelinesByThread[threadID]; !ok {
		m.snapshot.TimelinesByThread[threadID] = []TimelineItem{}
	}
	if _, ok := m.snapshot.DiffTextByThread[threadID]; !ok {
		m.snapshot.DiffTextByThread[threadID] = ""
	}
	if _, ok := m.snapshot.Statuses[threadID]; !ok {
		m.snapshot.Statuses[threadID] = "idle"
	}
	if _, ok := m.snapshot.StatusHeadersByThread[threadID]; !ok {
		m.snapshot.StatusHeadersByThread[threadID] = "等待指示"
	}
	if _, ok := m.snapshot.StatusDetailsByThread[threadID]; !ok {
		m.snapshot.StatusDetailsByThread[threadID] = ""
	}
	if _, ok := m.snapshot.AgentMetaByID[threadID]; !ok {
		m.snapshot.AgentMetaByID[threadID] = AgentMeta{}
	}
	if _, ok := m.runtime[threadID]; !ok {
		m.runtime[threadID] = newThreadRuntime()
	}
}

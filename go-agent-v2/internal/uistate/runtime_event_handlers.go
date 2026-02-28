package uistate

import (
	"fmt"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type resolvedFields struct {
	text      string
	command   string
	file      string
	files     []string
	exitCode  *int
	requestID int64
	planDone  *bool
	planSet   bool
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
		text:    normalized.Text,
		command: strings.TrimSpace(normalized.Command),
		file:    strings.TrimSpace(normalized.File),
		files:   nil,
	}
	if fields.text == "" {
		fields.text = util.ExtractFirstString(payload, "uiText", "delta", "text", "content", "output", "message")
	}
	if fields.command == "" {
		fields.command = extractNormalizedCommand(payload)
	}
	if fields.file == "" {
		fields.file = util.ExtractFirstString(payload, "uiFile", "file")
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
	if requestID, ok := extractFirstIntDeep(payload, "requestId", "request_id"); ok && requestID > 0 {
		fields.requestID = int64(requestID)
	} else if requestID, ok := extractFirstIntByPaths(
		payload,
		[]string{"item", "requestId"},
		[]string{"item", "request_id"},
		[]string{"msg", "requestId"},
		[]string{"msg", "request_id"},
		[]string{"data", "requestId"},
		[]string{"data", "request_id"},
		[]string{"payload", "requestId"},
		[]string{"payload", "request_id"},
		[]string{"args", "requestId"},
		[]string{"args", "request_id"},
		[]string{"params", "requestId"},
		[]string{"params", "request_id"},
	); ok && requestID > 0 {
		fields.requestID = int64(requestID)
	}
	return fields
}

func (m *RuntimeManager) applyAgentEventLocked(threadID string, normalized NormalizedEvent, payload map[string]any, ts time.Time) {
	m.markAgentActiveLocked(threadID, ts)
	rt := m.runtime[threadID]
	rt.hasDerivedState = true
	rt.lastEventAt = ts
	fields := resolveEventFields(normalized, payload)
	m.applyLifecycleStateLocked(threadID, normalized, payload, fields, ts)
	if handler, ok := runtimeEventHandlers[normalized.UIType]; ok {
		handler(m, threadID, fields, payload, ts)
	} else {
		m.logIgnoredRuntimeEvent(threadID, normalized)
	}
	nextState := m.deriveThreadStateLocked(threadID)
	m.setThreadStateLocked(threadID, nextState)
	header, details := m.deriveThreadStatusTextsLocked(threadID, nextState)
	m.snapshot.StatusHeadersByThread[threadID] = header
	m.snapshot.StatusDetailsByThread[threadID] = details
}

func (m *RuntimeManager) logIgnoredRuntimeEvent(threadID string, normalized NormalizedEvent) {
	eventType := strings.TrimSpace(normalized.RawType)
	method := strings.TrimSpace(normalized.Method)
	if eventType == "" && method == "" {
		return
	}
	logger.Debug("uistate: ignored runtime event (no UI handler)",
		logger.FieldThreadID, threadID,
		"ui_type", normalized.UIType,
		"event_type", eventType,
		logger.FieldMethod, method,
	)
}

func (m *RuntimeManager) applyLifecycleStateLocked(threadID string, normalized NormalizedEvent, payload map[string]any, fields resolvedFields, ts time.Time) {
	rt := m.runtime[threadID]
	eventType := strings.ToLower(strings.TrimSpace(normalized.RawType))
	method := strings.TrimSpace(normalized.Method)

	m.applyErrorOverlayLocked(rt, threadID, normalized.UIType, eventType, fields.text, payload)
	applyOverlays(rt, eventType, method, payload)
	applyCollabDepth(rt, eventType)

	isTokenEvent := eventType == "token_count" || eventType == "context_compacted" || method == "thread/tokenUsage/updated" || method == "thread/compacted"
	if isTokenEvent {
		if eventType == "context_compacted" || method == "thread/compacted" {
			keys := make([]string, 0, len(payload))
			for k := range payload {
				keys = append(keys, k)
			}
			logger.Info("uistate: compact event received → entering token update",
				logger.FieldThreadID, threadID,
				"event_type", eventType,
				"method", method,
				"payload_keys", keys,
			)
		}
		m.updateTokenUsageLocked(threadID, payload, eventType, method, ts)
	}
	if eventType == "thread/status/changed" || strings.EqualFold(method, "thread/status/changed") {
		m.applyThreadStatusChangedLocked(threadID, payload)
	}

	m.applyUITypeDepthsLocked(threadID, rt, normalized.UIType, eventType, method, fields.text, payload)
}

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
		clearStreamErrorOverlay(rt)
	}
}

func clearTerminalWaitOverlay(rt *threadRuntime) { rt.terminalWaitOverlay, rt.terminalWaitLabel = false, "" }
func clearMCPStartupOverlay(rt *threadRuntime) { rt.mcpStartupOverlay, rt.mcpStartupLabel = false, "" }
func clearBackgroundOverlay(rt *threadRuntime) { rt.backgroundOverlay, rt.backgroundLabel, rt.backgroundDetails = false, "", "" }
func clearStreamErrorOverlay(rt *threadRuntime) { rt.streamErrorText, rt.streamErrorDetails = "", "" }

func applyOverlays(rt *threadRuntime, eventType, method string, payload map[string]any) {
	if eventType == "exec_terminal_interaction" ||
		eventType == "item/commandexecution/terminalinteraction" ||
		strings.EqualFold(method, "item/commandExecution/terminalInteraction") {
		if isTerminalWaitPayload(payload) {
			rt.terminalWaitOverlay = true
			rt.terminalWaitLabel = deriveTerminalWaitLabel(payload)
		} else {
			clearTerminalWaitOverlay(rt)
		}
	}
	if isMCPStartupEvent(eventType, method, "update") {
		rt.mcpStartupOverlay = true
		rt.mcpStartupLabel = deriveMCPStartupLabel(payload)
	}
	if isMCPStartupEvent(eventType, method, "complete") {
		clearMCPStartupOverlay(rt)
	}
	if isBackgroundEvent(eventType, method) {
		if shouldClearBackgroundOverlay(payload) {
			clearBackgroundOverlay(rt)
			clearStreamErrorOverlay(rt)
		} else {
			rt.backgroundOverlay = true
			rt.backgroundLabel = deriveBackgroundLabel(payload)
			rt.backgroundDetails = deriveBackgroundDetails(payload)
		}
	}
	if rt.backgroundOverlay && !isBackgroundEvent(eventType, method) {
		clearBackgroundOverlay(rt)
	}
}

func applyCollabDepth(rt *threadRuntime, eventType string) {
	if eventType == "collab_agent_spawn_begin" || eventType == "collab_agent_interaction_begin" || eventType == "collab_waiting_begin" {
		rt.collabDepth += 1
	} else if eventType == "collab_agent_spawn_end" || eventType == "collab_agent_interaction_end" || eventType == "collab_waiting_end" {
		rt.collabDepth = max(0, rt.collabDepth-1)
	}
}

type uiTypeDepthHandler func(*RuntimeManager, string, *threadRuntime, string, string, string, map[string]any)

var uiTypeDepthHandlers = map[UIType]uiTypeDepthHandler{
	UITypeTurnStarted:     handleTurnStartedDepth,
	UITypeTurnComplete:    handleTurnCompleteDepth,
	UITypeReasoningDelta:  handleReasoningDeltaDepth,
	UITypeCommandStart:    handleCommandStartDepth,
	UITypeCommandOutput:   handleCommandOutputDepth,
	UITypeCommandDone:     handleCommandDoneDepth,
	UITypeFileEditStart:   handleFileEditStartDepth,
	UITypeFileEditDone:    handleFileEditDoneDepth,
	UITypeApprovalRequest: handleApprovalRequestDepth,
	UITypeToolCall:        handleToolCallDepth,
}

func (m *RuntimeManager) applyUITypeDepthsLocked(threadID string, rt *threadRuntime, uiType UIType, eventType, method, text string, payload map[string]any) {
	if handler, ok := uiTypeDepthHandlers[uiType]; ok {
		handler(m, threadID, rt, eventType, method, text, payload)
	}
}

func handleTurnStartedDepth(m *RuntimeManager, threadID string, rt *threadRuntime, _, _, _ string, _ map[string]any) {
	if rt.turnDepth > 0 || rt.commandDepth > 0 || rt.fileEditDepth > 0 || rt.toolCallDepth > 0 || rt.approvalDepth > 0 {
		logger.Warn("uistate: stale turn detected on new turn_started — forcibly resetting depth counters",
			logger.FieldThreadID, threadID,
			"prev_turn", rt.turnDepth,
			"prev_cmd", rt.commandDepth,
			"prev_edit", rt.fileEditDepth,
			"prev_tool", rt.toolCallDepth,
			"prev_approval", rt.approvalDepth,
		)
	}
	m.clearTurnLifecycleLocked(threadID)
	rt.turnDepth = 1
	rt.approvalDepth = 0
	rt.userInputDepth = 0
	clearTerminalWaitOverlay(rt)
	rt.statusHeader = "工作中"
	rt.approvalContext = ""
}

func handleTurnCompleteDepth(m *RuntimeManager, threadID string, rt *threadRuntime, _, _, _ string, _ map[string]any) {
	m.clearTurnLifecycleLocked(threadID)
	clearMCPStartupOverlay(rt)
}

func handleReasoningDeltaDepth(m *RuntimeManager, threadID string, rt *threadRuntime, eventType, method, text string, _ map[string]any) {
	if rt.turnDepth == 0 {
		rt.turnDepth = 1
	}
	if isReasoningSectionBreakEvent(eventType, method) {
		rt.reasoningHeaderBuf = ""
	}
	m.captureReasoningHeaderLocked(threadID, text)
}

func handleCommandStartDepth(m *RuntimeManager, threadID string, rt *threadRuntime, _, _, _ string, _ map[string]any) {
	rt.commandDepth += 1
	rt.approvalDepth = 0
	clearTerminalWaitOverlay(rt)
	m.incrActivityStatLocked(threadID, "command", "")
}

func handleCommandOutputDepth(_ *RuntimeManager, _ string, rt *threadRuntime, _, _, _ string, _ map[string]any) {
	if rt.commandDepth == 0 {
		rt.commandDepth = 1
	}
	clearTerminalWaitOverlay(rt)
}

func handleCommandDoneDepth(_ *RuntimeManager, _ string, rt *threadRuntime, _, _, _ string, _ map[string]any) {
	rt.commandDepth = max(0, rt.commandDepth-1)
	clearTerminalWaitOverlay(rt)
}

func handleFileEditStartDepth(m *RuntimeManager, threadID string, rt *threadRuntime, _, _, _ string, _ map[string]any) {
	rt.fileEditDepth += 1
	rt.approvalDepth = 0
	m.incrActivityStatLocked(threadID, "fileEdit", "")
}

func handleFileEditDoneDepth(_ *RuntimeManager, _ string, rt *threadRuntime, _, _, _ string, _ map[string]any) {
	rt.fileEditDepth = max(0, rt.fileEditDepth-1)
}

func handleApprovalRequestDepth(_ *RuntimeManager, _ string, rt *threadRuntime, _, _, _ string, payload map[string]any) {
	rt.approvalDepth += 1
	rt.approvalContext = extractApprovalContext(payload)
}

func extractApprovalContext(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	for _, key := range []string{"command", "displayCommand", "command_display"} {
		if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
			cmd := compactOneLine(v, 60)
			return "执行: " + cmd
		}
	}
	for _, key := range []string{"file", "path", "filePath"} {
		if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
			return "编辑: " + compactOneLine(v, 60)
		}
	}
	for _, wrapper := range []string{"msg", "data", "item"} {
		if nested, ok := payload[wrapper].(map[string]any); ok {
			result := extractApprovalContext(nested)
			if result != "" {
				return result
			}
		}
	}
	return ""
}

func handleToolCallDepth(m *RuntimeManager, threadID string, rt *threadRuntime, eventType, _, text string, payload map[string]any) {
	switch eventType {
	case "mcp_tool_call_begin":
		rt.toolCallDepth += 1
		toolName := resolveActivityToolName(text, payload)
		m.incrActivityStatLocked(threadID, "toolCall", toolName)
	case "mcp_tool_call_end":
		rt.toolCallDepth = max(0, rt.toolCallDepth-1)
	}
}

func resolveActivityToolName(text string, payload map[string]any) string {
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		return trimmed
	}
	if payload == nil {
		return ""
	}
	if topLevel := strings.TrimSpace(util.ExtractFirstString(payload, "tool", "tool_name", "name")); topLevel != "" {
		return topLevel
	}
	return strings.TrimSpace(extractNestedFirstString(
		payload,
		[]string{"item", "tool"},
		[]string{"item", "tool_name"},
		[]string{"item", "name"},
		[]string{"msg", "tool"},
		[]string{"msg", "tool_name"},
		[]string{"msg", "name"},
		[]string{"data", "tool"},
		[]string{"data", "tool_name"},
		[]string{"data", "name"},
		[]string{"payload", "tool"},
		[]string{"payload", "tool_name"},
		[]string{"payload", "name"},
	))
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
	clearTerminalWaitOverlay(rt)
	clearBackgroundOverlay(rt)
	clearStreamErrorOverlay(rt)
	rt.statusHeader = ""
	rt.reasoningHeaderBuf = ""
	rt.approvalContext = ""
}

func normalizeActivityToolName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return ""
	}
	normalized = strings.NewReplacer(
		"/", "_",
		".", "_",
		":", "_",
		"-", "_",
	).Replace(normalized)
	normalized = strings.Trim(normalized, "_")
	for _, prefix := range []string{
		"functions_",
		"function_",
		"tools_",
		"tool_",
	} {
		normalized = strings.TrimPrefix(normalized, prefix)
	}
	return normalized
}

func isLSPActivityToolName(name string) bool {
	return strings.HasPrefix(normalizeActivityToolName(name), "lsp_")
}

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
		if isLSPActivityToolName(name) {
			stats.LSPCalls++
		}
	}
	m.snapshot.ActivityStatsByThread[threadID] = stats
}

func (m *RuntimeManager) IncrActivityStat(threadID, kind, toolName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.incrActivityStatLocked(threadID, kind, toolName)
}
func (m *RuntimeManager) PushAlert(threadID, level, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pushAlertLocked(threadID, level, message)
}

func (m *RuntimeManager) pushAlertLocked(threadID, level, message string) {
	alerts := m.snapshot.AlertsByThread[threadID]
	entry := AlertEntry{
		ID:      fmt.Sprintf("a-%d", m.seq),
		Time:    time.Now().Format("15:04"),
		Level:   level,
		Message: message,
	}
	alerts = append(alerts, entry)
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
	case rt.userInputDepth > 0 || rt.approvalDepth > 0:
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

func deriveThreadStatusOverlay(rt *threadRuntime) (string, string, bool) {
	switch {
	case strings.TrimSpace(rt.streamErrorText) != "":
		return rt.streamErrorText, strings.TrimSpace(rt.streamErrorDetails), true
	case rt.terminalWaitOverlay:
		if strings.TrimSpace(rt.terminalWaitLabel) != "" {
			return rt.terminalWaitLabel, "命令正在等待终端输入", true
		}
		return "等待后台终端", "命令正在等待终端输入", true
	case shouldShowMCPStartupOverlay(rt):
		if strings.TrimSpace(rt.mcpStartupLabel) != "" {
			return rt.mcpStartupLabel, "正在初始化 MCP 服务", true
		}
		return "MCP 启动中", "正在初始化 MCP 服务", true
	case rt.backgroundOverlay:
		header := "后台处理中"
		if strings.TrimSpace(rt.backgroundLabel) != "" {
			header = rt.backgroundLabel
		}
		details := "后台事件处理中"
		if strings.TrimSpace(rt.backgroundDetails) != "" {
			details = rt.backgroundDetails
		}
		return header, details, true
	case rt.userInputDepth > 0:
		return "等待输入", "等待用户输入后继续", true
	case rt.approvalDepth > 0:
		if strings.TrimSpace(rt.approvalContext) != "" {
			return "等待确认 · " + rt.approvalContext, "等待用户审批后继续", true
		}
		return "等待确认", "等待用户审批后继续", true
	case shouldUseReasoningHeader(rt):
		return rt.statusHeader, "根据推理标题展示当前阶段", true
	default:
		return "", "", false
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

func (m *RuntimeManager) deriveThreadStatusTextsLocked(threadID, state string) (string, string) {
	rt := m.runtime[threadID]
	if header, details, ok := deriveThreadStatusOverlay(rt); ok {
		return header, details
	}
	header := defaultStatusHeaderForState(state)
	switch state {
	case "running":
		return header, "命令或工具正在执行"
	case "editing":
		return header, "文件修改进行中"
	case "thinking":
		return header, "模型推理中"
	case "syncing":
		return header, "后台同步中"
	case "error":
		return header, "运行出现异常"
	default:
		return header, ""
	}
}

func shouldShowMCPStartupOverlay(rt *threadRuntime) bool {
	return rt != nil && rt.mcpStartupOverlay &&
		rt.turnDepth == 0 && rt.approvalDepth == 0 && rt.userInputDepth == 0 &&
		rt.commandDepth == 0 && rt.fileEditDepth == 0 && rt.toolCallDepth == 0 && rt.collabDepth == 0
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
		statusType = strings.ToLower(strings.TrimSpace(util.ExtractFirstString(status, "type")))
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
	case "systemerror", "system_error", "error":
		m.clearTurnLifecycleLocked(threadID)
		rt.streamErrorText = "系统异常"
		rt.streamErrorDetails = "线程状态变为 systemError"
	case "idle", "notloaded", "not_loaded":
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

func isMCPStartupEvent(eventType, method, kind string) bool {
	base := "mcp_startup_" + kind
	codex := "codex/event/" + base
	agent := "agent/event/" + base
	return eventType == base ||
		eventType == codex ||
		eventType == agent ||
		strings.EqualFold(method, codex) ||
		strings.EqualFold(method, agent)
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
	command := strings.TrimSpace(util.ExtractFirstString(payload, "command", "command_display", "displayCommand"))
	if command == "" {
		command = strings.TrimSpace(extractNestedFirstString(payload, []string{"process", "command_display"}, []string{"process", "command"}))
	}
	if command == "" {
		return "等待后台终端"
	}
	return "等待后台终端 · " + command
}

func deriveMCPStartupLabel(payload map[string]any) string {
	server := strings.TrimSpace(util.ExtractFirstString(payload, "server", "name"))
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
	status := strings.ToLower(strings.TrimSpace(util.ExtractFirstString(payload, "status", "state", "phase")))
	switch status {
	case "done", "completed", "complete", "finished", "success", "succeeded", "idle", "stopped", "closed", "ended":
		return true
	default:
		return false
	}
}

func deriveBackgroundLabel(payload map[string]any) string {
	text := util.ExtractFirstString(payload, "uiHeader", "statusHeader", "title", "event", "name")
	if strings.TrimSpace(text) == "" {
		text = util.ExtractFirstString(payload, "message", "text", "content")
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
	text := util.ExtractFirstString(payload, "details", "detail", "description", "message", "text", "content")
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
	text := util.ExtractFirstString(payload, "additional_details", "additionalDetails", "details")
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
	return rt != nil &&
		strings.TrimSpace(rt.statusHeader) != "" && rt.turnDepth > 0 &&
		rt.userInputDepth == 0 && rt.approvalDepth == 0 && rt.commandDepth == 0 &&
		rt.fileEditDepth == 0 && rt.toolCallDepth == 0 && rt.collabDepth == 0 &&
		!rt.terminalWaitOverlay &&
		!rt.mcpStartupOverlay &&
		!rt.backgroundOverlay &&
		strings.TrimSpace(rt.streamErrorText) == ""
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
	doneText := fields.text
	if strings.TrimSpace(doneText) != "" {
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
				current := timeline[idx].Text
				if strings.TrimSpace(current) == "" {
					shouldBackfill = true
				} else if idx != len(timeline)-1 {
					if doneText == current {
						backfillText = ""
					} else if strings.HasPrefix(doneText, current) {
						backfillText = strings.TrimPrefix(doneText, current)
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
	m.showApprovalLocked(threadID, fields.command, fields.requestID, ts)
}

func handlePlanDeltaEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	if fields.planSet {
		m.setPlanLocked(threadID, fields.text, fields.planDone != nil && *fields.planDone, ts)
		return
	}
	m.appendPlanLocked(threadID, fields.text, ts)
}

func handleDiffUpdateEvent(m *RuntimeManager, threadID string, _ resolvedFields, payload map[string]any, _ time.Time) {
	diff := util.ExtractFirstString(payload, "diff", "uiText", "text", "content")
	prev := m.snapshot.DiffTextByThread[threadID]
	m.snapshot.DiffTextByThread[threadID] = diff
	if prev == diff {
		return
	}
	logger.Info("uistate: diff text updated",
		logger.FieldThreadID, threadID,
		"old_len", len(prev),
		"new_len", len(diff),
	)
}

func handleUserMessageEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	if text := sanitizeUserMessageTextWithMode(fields.text, m.sanitizeInjectedUserMessage); strings.TrimSpace(text) != "" {
		m.appendUserLocked(threadID, text, nil, ts)
	}
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

func sanitizeUserMessageTextWithMode(text string, trimInjected bool) string {
	text = util.StripLeadingSystemNoise(text)
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if !trimInjected {
		return text
	}
	trimmed := util.TrimInjectedSkillBlock(text)
	trimmed = util.TrimInjectedLSPHint(trimmed)
	return trimmed
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

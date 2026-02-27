package codex

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (c *AppServerClient) readLoop() {
	if !c.readLoopRunning.CompareAndSwap(false, true) {
		return
	}
	connectionDead := false // 标记非正常退出, defer 时发射 connection_dead 事件
	defer func() {
		c.readLoopRunning.Store(false)
		c.wsMu.Lock()
		if c.ws != nil {
			_ = c.ws.Close()
		}
		c.wsMu.Unlock()
		c.failPendingCalls(apperrors.New("AppServerClient.readLoop", "connection closed"))

		select {
		case <-c.wsDone:
		default:
			close(c.wsDone)
		}

		if connectionDead {
			c.emitConnectionDeadEvent()
		}
	}()

	for !c.stopped.Load() {
		conn := c.currentWSConn()
		if conn == nil {
			if c.stopped.Load() {
				return
			}
			if !c.reconnectWS("ws_missing", apperrors.New("AppServerClient.readLoop", "ws not connected")) {
				connectionDead = true
				return
			}
			continue
		}
		_, message, err := conn.ReadMessage()
		if err == nil {
			_ = conn.SetReadDeadline(time.Now().Add(currentAppServerReadIdleTimeout()))
		}
		if err != nil {
			readErr := apperrors.Wrap(err, "AppServerClient.readLoop", "read message")
			health, shouldRespawn, openedCircuit := c.noteReadFailure(time.Now())
			c.failPendingCalls(readErr)
			if !c.stopped.Load() {
				willRetry := appServerStreamMaxRetries > 0
				reconnectingMessage := "Reconnecting..."
				if !willRetry {
					reconnectingMessage = "Stream disconnected"
				}
				details := map[string]any{
					"message":     reconnectingMessage,
					"attempt":     0,
					"max_retries": appServerStreamMaxRetries,
					"trigger":     "read_error",
				}
				maps.Copy(details, health.asDetailsMap())
				c.emitStreamError(readErr, "read", isIdleTimeoutError(err), willRetry, details)
			}
			if c.stopped.Load() && isShutdownReadError(err) {
				logger.Debug("codex: readLoop read failed (shutdown)", logger.FieldAgentID, c.AgentID, logger.FieldError, readErr)
			} else {
				logger.Warn("codex: readLoop read failed",
					logger.FieldAgentID, c.AgentID,
					"idle_timeout", isIdleTimeoutError(err),
					"should_respawn", shouldRespawn,
					"opened_circuit", openedCircuit,
					"failure_streak", health.ReadFailureStreak,
					logger.FieldError, readErr,
				)
			}
			if c.stopped.Load() {
				return
			}
			trigger := "read_error"
			if shouldRespawn {
				trigger = "read_error_burst"
			}
			if c.reconnectWS(trigger, readErr) {
				continue
			}
			connectionDead = true
			return
		}

		var msg jsonRPCMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Warn("codex: readLoop unparseable JSON-RPC message", logger.FieldAgentID, c.AgentID, logger.FieldError, err, "raw_prefix", truncateBytes(message, 200))
			continue
		}
		if dropped, preview, convID := shouldDropLegacyMirrorNotification(msg); dropped {
			seq := c.legacyMirrorDropCount.Add(1)
			if shouldLogLegacyMirrorDrop(seq) {
				logger.Info("codex: dropped legacy mirror stream notification", logger.FieldAgentID, c.AgentID, logger.FieldMethod, msg.Method, "conversation_id", convID, "preview", preview, "drop_count", seq)
			} else {
				logger.Debug("codex: dropped legacy mirror stream notification", logger.FieldAgentID, c.AgentID, logger.FieldMethod, msg.Method, "conversation_id", convID, "preview", preview)
			}
			continue
		}

		if c.handleRPCResponse(msg) {
			continue
		}

		if c.handleRPCEvent(msg) {
			return
		}
	}
}

func (c *AppServerClient) pingLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(appServerPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.wsDone:
			return
		case <-ticker.C:
			c.wsMu.Lock()
			if c.ws != conn {
				c.wsMu.Unlock()
				return
			}
			err := c.ws.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(appServerWriteTimeout))
			if err != nil {
				_ = c.ws.Close()
				c.ws = nil
				c.wsMu.Unlock()
				return
			}
			c.wsMu.Unlock()
		}
	}
}

func (c *AppServerClient) handleRPCResponse(msg jsonRPCMessage) bool {
	if msg.ID == nil || msg.Method != "" {
		return false
	}
	reqID := msg.ID.clone()
	value, ok := c.pending.Load(reqID.pendingKey())
	if !ok {
		logger.Warn("codex: orphan RPC response (no pending call)", logger.FieldAgentID, c.AgentID, logger.FieldID, reqID.logValue())
		return true
	}
	pc := value.(*pendingCall)
	if msg.Error != nil {
		pc.resolve(nil, apperrors.Newf("AppServerClient.readLoop", "rpc error: %s (code %d)", msg.Error.Message, msg.Error.Code))
		notInitialized := msg.Error.Code == -32600 && strings.Contains(strings.ToLower(strings.TrimSpace(msg.Error.Message)), "not initialized")
		if notInitialized {
			health, shouldRecover := c.noteNotInitializedRPC(time.Now())
			logger.Warn("codex: RPC not initialized", logger.FieldAgentID, c.AgentID, logger.FieldID, reqID.logValue(), "not_initialized_streak", health.NotInitializedStreak, "should_recover", shouldRecover)
			if shouldRecover && !c.stopped.Load() {
				recoverErr := apperrors.Newf("AppServerClient.readLoop", "rpc not initialized (code %d)", msg.Error.Code)
				if c.reconnectWS("rpc_not_initialized", recoverErr) {
					logger.Info("codex: recovered after repeated not initialized rpc", logger.FieldAgentID, c.AgentID)
				}
			}
		}
		logger.Warn("codex: RPC error response", logger.FieldAgentID, c.AgentID, logger.FieldID, reqID.logValue(), "code", msg.Error.Code, "message", msg.Error.Message)
	} else {
		pc.resolve(msg.Result, nil)
	}
	return true
}

func (c *AppServerClient) handleRPCEvent(msg jsonRPCMessage) bool {
	event := c.jsonRPCToEvent(msg)
	event.DenyFunc = func() error { return c.Submit("no", nil, nil, nil) }
	if event.Type == "" {
		logger.Warn("codex: readLoop skipped message with empty event type", logger.FieldAgentID, c.AgentID, logger.FieldMethod, msg.Method)
		return false
	}
	if msg.ID != nil && msg.Method != "" {
		reqID := msg.ID.clone()
		event.RequestID = reqID.int64Ptr()
		event.RequestIDRaw = reqID.rawCopy()
		event.RespondFunc = func(code int, message string) error {
			return c.respondErrorWithID(reqID, code, message)
		}
		event.RespondResultFunc = func(result any) error {
			return c.respondWithID(reqID, result)
		}
		logger.Debug("codex: server request received", logger.FieldAgentID, c.AgentID, logger.FieldID, reqID.logValue(), logger.FieldMethod, msg.Method, logger.FieldEventType, event.Type)
	}
	conversationID := extractConversationIDFromEventParams(msg.Params)
	boundThreadID := strings.TrimSpace(c.ThreadID)
	mismatch := conversationID != "" && boundThreadID != "" && !strings.EqualFold(conversationID, boundThreadID)
	if mismatch {
		logFn := logger.Warn
		if isMCPStartupMethod(msg.Method) {
			logFn = logger.Debug
		}
		logFn("codex: incoming event conversation mismatch", logger.FieldAgentID, c.AgentID, logger.FieldMethod, msg.Method, logger.FieldThreadID, boundThreadID, "conversation_id", conversationID)
		if !isMCPStartupMethod(msg.Method) {
			if shouldRecoverLifecycleOnMismatchedConversation(
				event,
				msg.Method,
				c.getActiveTurnID(),
				c.listenerEnsureNeeded.Load(),
			) {
				c.trackTurnLifecycle(event, msg.Method)
				logger.Warn("codex: recovered turn lifecycle from mismatched event", logger.FieldAgentID, c.AgentID, logger.FieldMethod, msg.Method, logger.FieldThreadID, boundThreadID, "conversation_id", conversationID)
			}
			logger.Warn("codex: dropping mismatched thread-scoped event", logger.FieldAgentID, c.AgentID, logger.FieldMethod, msg.Method, logger.FieldThreadID, boundThreadID, "conversation_id", conversationID)
			return false
		}
	}
	c.trackTurnLifecycle(event, msg.Method)

	c.handlerMu.RLock()
	handler := c.handler
	c.handlerMu.RUnlock()
	if handler == nil {
		logger.Warn("codex: readLoop dropping event (no handler registered)", logger.FieldAgentID, c.AgentID, logger.FieldEventType, event.Type, logger.FieldMethod, msg.Method)
		return false
	}
	handler(event)
	return event.Type == EventShutdownComplete
}

func (c *AppServerClient) trackTurnLifecycle(event Event, method string) {
	activeTurnID := c.getActiveTurnID()
	switch event.Type {
	case EventTurnStarted:
		turnID := extractTurnIDFromEventData(event.Data)
		if turnID != "" {
			c.setActiveTurnID(turnID)
		}
	case EventTurnComplete, "turn_aborted", EventIdle, EventError, EventShutdownComplete:
		if activeTurnID != "" {
			c.clearActiveTurnID()
		}
	case EventStreamError:
		if streamErrorWillRetry(event.Data) || activeTurnID == "" {
			return
		}
		c.clearActiveTurnID()
	case "thread/status/changed":
		if activeTurnID == "" {
			return
		}
		if _, terminal := threadStatusChangedTerminalState(event.Data); terminal {
			c.clearActiveTurnID()
		}
	}
}

func shouldRecoverLifecycleOnMismatchedConversation(
	event Event,
	method,
	activeTurnID string,
	allowThreadTerminalWithoutTurnID bool,
) bool {
	activeTurnID = strings.TrimSpace(activeTurnID)
	if activeTurnID == "" {
		return false
	}
	turnID := strings.TrimSpace(extractTurnIDFromEventData(event.Data))
	targetsActiveTurn := turnID != "" && strings.EqualFold(turnID, activeTurnID)
	switch event.Type {
	case EventTurnComplete, "turn_aborted", EventIdle, EventError, EventShutdownComplete:
		return targetsActiveTurn
	case EventStreamError:
		return targetsActiveTurn && !streamErrorWillRetry(event.Data)
	}
	if strings.EqualFold(strings.TrimSpace(method), "thread/status/changed") {
		_, terminal := threadStatusChangedTerminalState(event.Data)
		if !terminal {
			return false
		}
		turnID := strings.TrimSpace(extractTurnIDFromEventData(event.Data))
		if turnID != "" {
			return strings.EqualFold(turnID, strings.TrimSpace(activeTurnID))
		}
		return allowThreadTerminalWithoutTurnID
	}
	return false
}

func threadStatusChangedTerminalState(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return "", false
	}

	var statusType string
	switch status := payload["status"].(type) {
	case string:
		statusType = strings.ToLower(strings.TrimSpace(status))
	case map[string]any:
		statusType = strings.ToLower(strings.TrimSpace(trimmedStringValue(status["type"])))
	}

	if statusType == "" {
		return "", false
	}

	switch statusType {
	case "idle", "systemerror", "system_error", "error", "notloaded", "not_loaded":
		return statusType, true
	default:
		return statusType, false
	}
}

func extractTurnIDFromEventData(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return extractTurnIDFromPayload(payload)
}

func extractTurnIDFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if id := trimmedStringValue(payload["turnId"]); id != "" {
		return id
	}
	if id := trimmedStringValue(payload["turn_id"]); id != "" {
		return id
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if id := trimmedStringValue(turn["id"]); id != "" {
			return id
		}
		if id := trimmedStringValue(turn["turnId"]); id != "" {
			return id
		}
	}
	for _, key := range []string{"msg", "data", "payload"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if id := extractTurnIDFromPayload(nested); id != "" {
			return id
		}
	}
	return ""
}

func trimmedStringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func (c *AppServerClient) getActiveTurnID() string {
	v := c.activeTurnID.Load()
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func (c *AppServerClient) setActiveTurnID(id string) {
	c.activeTurnID.Store(id)
}

func (c *AppServerClient) clearActiveTurnID() {
	c.activeTurnID.Store("")
}

type methodEventAlias struct {
	method    string
	eventType string
}

type prefixedEventAlias struct {
	suffix    string
	eventType string
}

var baseMethodAliases = [...]methodEventAlias{
	{"error", EventError},
	{"thread/started", EventSessionConfigured},
	{"thread/name/updated", EventThreadNameUpdated},
	{"thread/tokenUsage/updated", EventTokenCount},
	{"turn/started", EventTurnStarted},
	{"turn/completed", EventTurnComplete},
	{"turn/aborted", "turn_aborted"},
	{"turn/diff/updated", EventTurnDiff},
	{"turn/plan/updated", EventPlanUpdate},
	{"item/agentMessage/delta", EventAgentMessageDelta},
	{"item/plan/delta", EventPlanDelta},
	{"item/commandExecution/outputDelta", EventExecCommandOutputDelta},
	{"item/reasoning/summaryTextDelta", EventAgentReasoningDelta},
	{"item/reasoning/summaryPartAdded", EventAgentReasoningSectionBreak},
	{"item/reasoning/textDelta", EventAgentReasoningRawDelta},
	{"thread/compacted", EventContextCompacted},
	{"deprecationNotice", "deprecationNotice"},
	{"configWarning", EventWarning},
	{"windows/worldWritableWarning", EventWarning},
	{"authStatusChange", "authStatusChange"},
	{"loginChatGptComplete", "loginChatGptComplete"},
	{"sessionConfigured", EventSessionConfigured},
	{"item/commandExecution/requestApproval", EventExecApprovalRequest},
	{"item/tool/call", EventDynamicToolCall},
	{"applyPatchApproval", "applyPatchApproval"},
	{"execCommandApproval", EventExecApprovalRequest},
}

var sharedLegacyAliases = [...]prefixedEventAlias{
	{"agent_message_content_delta", EventAgentMessageDelta},
	{"agent_message_delta", EventAgentMessageDelta},
	{"agent_message", EventAgentMessage},
	{"agent_reasoning", EventAgentReasoning},
	{"agent_reasoning_raw", EventAgentReasoningRaw},
	{"agent_reasoning_raw_delta", EventAgentReasoningRawDelta},
	{"agent_reasoning_section_break", EventAgentReasoningSectionBreak},
	{"agent_reasoning_delta", EventAgentReasoningDelta},
	{"agent_message_completed", EventAgentMessageCompleted},
	{"turn_started", EventTurnStarted},
	{"turn_completed", EventTurnComplete},
	{"turn_aborted", "turn_aborted"},
	{"session_configured", EventSessionConfigured},
	{"mcp_startup_complete", EventMCPStartupComplete},
	{"shutdown_complete", EventShutdownComplete},
	{"error", EventError},
	{"stream_error", EventStreamError},
	{"warning", EventWarning},
	{"exec_approval_request", EventExecApprovalRequest},
	{"exec_command_begin", EventExecCommandBegin},
	{"exec_command_end", EventExecCommandEnd},
	{"exec_command_output_delta", EventExecCommandOutputDelta},
	{"patch_apply_begin", EventPatchApplyBegin},
	{"patch_apply_end", EventPatchApplyEnd},
	{"mcp_tool_call_begin", EventMCPToolCallBegin},
	{"mcp_tool_call_end", EventMCPToolCallEnd},
	{"mcp_list_tools_response", EventMCPListToolsResponse},
	{"list_skills_response", EventListSkillsResponse},
	{"dynamic_tool_call", EventDynamicToolCall},
	{"collab_agent_spawn_begin", EventCollabAgentSpawnBegin},
	{"collab_agent_spawn_end", EventCollabAgentSpawnEnd},
	{"collab_agent_interaction_begin", EventCollabAgentInteractionBegin},
	{"collab_agent_interaction_end", EventCollabAgentInteractionEnd},
}

var codexOnlyLegacyAliases = [...]prefixedEventAlias{
	{"task_started", EventTurnStarted},
	{"reasoning_content_delta", EventAgentReasoningDelta},
	{"token_count", EventTokenCount},
	{"context_compacted", EventContextCompacted},
	{"thread_name_updated", EventThreadNameUpdated},
	{"thread_rolled_back", EventThreadRolledBack},
	{"plan_delta", EventPlanDelta},
	{"plan_update", EventPlanUpdate},
	{"item_started", "item/started"},
	{"item_completed", "item/completed"},
	{"raw_response_item", "rawResponseItem/completed"},
}

func putMethodEventAliases(target map[string]string, aliases []methodEventAlias) {
	for _, alias := range aliases {
		target[alias.method] = alias.eventType
	}
}

func putPrefixedEventAliases(target map[string]string, prefix string, aliases []prefixedEventAlias) {
	for _, alias := range aliases {
		target[prefix+alias.suffix] = alias.eventType
	}
}

func buildMethodToEventMap() map[string]string {
	methodMap := make(map[string]string, 96)
	putMethodEventAliases(methodMap, baseMethodAliases[:])
	putPrefixedEventAliases(methodMap, "agent/event/", sharedLegacyAliases[:])
	putPrefixedEventAliases(methodMap, "codex/event/", sharedLegacyAliases[:])
	putPrefixedEventAliases(methodMap, "codex/event/", codexOnlyLegacyAliases[:])
	return methodMap
}

var methodToEventMap = buildMethodToEventMap()

var mappedMethodPrefixes = [...]string{
	"thread/",
	"turn/",
	"item/",
	"account/",
	"app/",
	"mcpServer/",
	"fuzzyFileSearch/",
	"rawResponseItem/",
	"windows/",
	"codex/event/",
	"agent/event/",
}

func mapMethodToEventType(method string) (string, bool) {
	if eventType, ok := methodToEventMap[method]; ok {
		return eventType, true
	}

	for _, prefix := range mappedMethodPrefixes {
		if strings.HasPrefix(method, prefix) {
			return method, true
		}
	}

	return "", false
}

func (c *AppServerClient) jsonRPCToEvent(msg jsonRPCMessage) Event {
	eventType, ok := mapMethodToEventType(msg.Method)
	if !ok {
		eventType = msg.Method
		logger.Warn("codex: unmapped JSON-RPC method → using raw method as event type",
			logger.FieldAgentID, c.AgentID,
			logger.FieldMethod, msg.Method,
			logger.FieldParamsLen, len(msg.Params),
		)
	}
	normalizedParams := msg.Params
	if strings.EqualFold(strings.TrimSpace(msg.Method), "error") {
		normalizedParams = normalizeErrorNotificationPayload(msg.Params)
		if streamErrorWillRetry(normalizedParams) {
			eventType = EventStreamError
		}
	}

	return Event{Type: eventType, Data: normalizedParams}
}

func normalizeErrorNotificationPayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw
	}
	if payload == nil {
		return raw
	}

	if errObj, ok := payload["error"].(map[string]any); ok && errObj != nil {
		if _, exists := payload["message"]; !exists {
			if msg := strings.TrimSpace(trimmedStringValue(errObj["message"])); msg != "" {
				payload["message"] = msg
			}
		}
		if _, exists := payload["additional_details"]; !exists {
			if details := strings.TrimSpace(trimmedStringValue(errObj["additionalDetails"])); details != "" {
				payload["additional_details"] = details
			} else if details := strings.TrimSpace(trimmedStringValue(errObj["additional_details"])); details != "" {
				payload["additional_details"] = details
			}
		}
	}

	syncKeys(payload, "willRetry", "will_retry")

	normalized, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return normalized
}

func syncKeys(m map[string]any, k1, k2 string) {
	if _, exists := m[k1]; !exists {
		if v, ok := m[k2]; ok {
			m[k1] = v
		}
	}
	if _, exists := m[k2]; !exists {
		if v, ok := m[k1]; ok {
			m[k2] = v
		}
	}
}

func truncateBytes(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}

func isShutdownReadError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

const legacyMirrorDropLogSampleInterval int64 = 100

func shouldLogLegacyMirrorDrop(seq int64) bool {
	return seq == 1 || seq%legacyMirrorDropLogSampleInterval == 0
}

var legacyMirrorStreamMethods = map[string]struct{}{
	"agent/event/agent_message_delta":         {},
	"agent/event/agent_message_content_delta": {},
	"codex/event/agent_message_delta":         {},
	"codex/event/agent_message_content_delta": {},
	"agent/event/agent_reasoning_delta":       {},
	"agent/event/agent_reasoning_raw_delta":   {},
	"codex/event/agent_reasoning_delta":       {},
	"codex/event/agent_reasoning_raw_delta":   {},
	"codex/event/reasoning_content_delta":     {},
	"agent/event/exec_command_output_delta":   {},
	"codex/event/exec_command_output_delta":   {},
	"codex/event/plan_delta":                  {},
}

func shouldDropLegacyMirrorNotification(msg jsonRPCMessage) (bool, string, string) {
	if msg.ID != nil {
		return false, "", ""
	}

	var payload map[string]any
	if len(msg.Params) == 0 || json.Unmarshal(msg.Params, &payload) != nil {
		return false, "", ""
	}
	if payload == nil {
		return false, "", ""
	}

	conversationID, hasConversationID := payload["conversationId"].(string)
	if !hasConversationID || strings.TrimSpace(conversationID) == "" {
		return false, "", ""
	}

	msgObj, ok := payload["msg"].(map[string]any)
	if !ok {
		return false, "", ""
	}
	preview := extractLegacyMirrorPreview(msgObj)
	if preview == "" {
		return false, "", ""
	}

	if !isLegacyMirrorEnvelope(msg.Method, payload) {
		return false, "", ""
	}
	return true, preview, conversationID
}

func extractConversationIDFromEventParams(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return ""
	}
	if value := trimmedStringValue(payload["conversationId"]); value != "" {
		return value
	}
	if value := trimmedStringValue(payload["conversation_id"]); value != "" {
		return value
	}
	if value := trimmedStringValue(payload["threadId"]); value != "" {
		return value
	}
	if value := trimmedStringValue(payload["thread_id"]); value != "" {
		return value
	}
	if thread, ok := payload["thread"].(map[string]any); ok {
		if value := trimmedStringValue(thread["id"]); value != "" {
			return value
		}
		if value := trimmedStringValue(thread["threadId"]); value != "" {
			return value
		}
		if value := trimmedStringValue(thread["thread_id"]); value != "" {
			return value
		}
	}
	return ""
}

func isLegacyMirrorEnvelope(method string, payload map[string]any) bool {
	if _, ok := legacyMirrorStreamMethods[method]; ok {
		return true
	}
	for _, key := range [...]string{"threadId", "turnId", "itemId", "outputIndex", "contentIndex"} {
		if _, exists := payload[key]; exists {
			return false
		}
	}
	_, hasLegacyID := payload["id"]
	return hasLegacyID
}

func extractLegacyMirrorPreview(msgObj map[string]any) string {
	for _, key := range []string{"delta", "text", "content", "output", "message"} {
		value, ok := msgObj[key].(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		return truncateString(trimmed, 80)
	}
	return ""
}

func truncateString(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "...(truncated)"
}

func isMCPStartupMethod(method string) bool {
	m := strings.ToLower(strings.TrimSpace(method))
	switch m {
	case "agent/event/mcp_startup_update",
		"agent/event/mcp_startup_complete",
		"codex/event/mcp_startup_update",
		"codex/event/mcp_startup_complete":
		return true
	default:
		return false
	}
}

func (c *AppServerClient) asWriteJSON(v any) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.ws == nil {
		err := apperrors.New("AppServerClient.asWriteJSON", "ws not connected")
		c.failPendingCalls(err)
		return err
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(appServerWriteTimeout))
	if err := c.ws.WriteJSON(v); err != nil {
		writeErr := apperrors.Wrap(err, "AppServerClient.asWriteJSON", "ws write")
		_ = c.ws.Close()
		c.ws = nil
		c.failPendingCalls(writeErr)
		return writeErr
	}
	return nil
}

func (c *AppServerClient) failPendingCalls(err error) {
	if err == nil {
		err = apperrors.New("AppServerClient.failPendingCalls", "connection unavailable")
	}
	c.pending.Range(func(_, value any) bool {
		if call, ok := value.(*pendingCall); ok {
			call.resolve(nil, err)
		}
		return true
	})
}

func (c *AppServerClient) emitStreamError(err error, phase string, idleTimeout bool, willRetry bool, details map[string]any) {
	if err == nil {
		return
	}
	c.handlerMu.RLock()
	handler := c.handler
	c.handlerMu.RUnlock()
	if handler == nil {
		return
	}

	message := strings.TrimSpace(err.Error())
	payload := map[string]any{
		"message":     message,
		"phase":       strings.TrimSpace(phase),
		"recoverable": willRetry,
		"willRetry":   willRetry,
		"will_retry":  willRetry,
	}
	if details != nil {
		maps.Copy(payload, details)
		if override := strings.TrimSpace(trimmedStringValue(details["message"])); override != "" {
			payload["message"] = override
			if message != "" && !strings.EqualFold(message, override) {
				payload["additional_details"] = message
			}
		}
	}
	if c.AgentID != "" {
		payload["agentId"] = c.AgentID
	}
	if c.Port > 0 {
		payload["port"] = c.Port
	}
	if activeTurnID := c.getActiveTurnID(); activeTurnID != "" {
		payload["activeTurnId"] = activeTurnID
	}
	if idleTimeout {
		payload["reason"] = "idle_timeout"
	}
	data, _ := json.Marshal(payload)
	handler(Event{Type: EventStreamError, Data: data})
}

func (c *AppServerClient) emitConnectionDeadEvent() {
	c.handlerMu.RLock()
	h := c.handler
	c.handlerMu.RUnlock()
	if h == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"message": "Connection permanently lost — all recovery attempts exhausted",
		"status":  "dead",
		"agentId": c.AgentID,
	})
	logger.Warn("codex: emitting connection_dead event",
		logger.FieldAgentID, c.AgentID,
	)
	h(Event{Type: EventConnectionDead, Data: payload})
}

func streamErrorWillRetry(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return false
	}
	if value, ok := extractBoolValue(payload, "willRetry", "will_retry", "recoverable"); ok {
		return value
	}
	return false
}

func extractBoolValue(payload map[string]any, keys ...string) (bool, bool) {
	if payload == nil {
		return false, false
	}
	for _, key := range keys {
		value, exists := payload[key]
		if !exists {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "y":
				return true, true
			case "false", "0", "no", "n":
				return false, true
			}
		}
	}
	return false, false
}

func isIdleTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "i/o timeout") || strings.Contains(text, "read timeout")
}

func (c *AppServerClient) SpawnAndConnect(ctx context.Context, prompt, cwd, model, instructions string, dynamicTools []DynamicTool) error {
	if err := c.Spawn(ctx); err != nil {
		return err
	}

	if err := c.connectWS(); err != nil {
		_ = c.Kill()
		return err
	}

	if err := c.Initialize(); err != nil {
		_ = c.Kill()
		return apperrors.Wrap(err, "AppServerClient.SpawnAndConnect", "initialize")
	}

	threadID, err := c.ThreadStart(cwd, model, instructions, dynamicTools)
	if err != nil {
		_ = c.Kill()
		return err
	}

	logger.Info("codex: app-server thread started",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		logger.FieldThreadID, threadID,
		"dynamic_tools", len(dynamicTools),
	)
	return nil
}

func (c *AppServerClient) Shutdown() error {
	if c.stopped.Swap(true) {
		return nil
	}
	c.cancel()

	if err := c.notify("shutdown", nil); err != nil {
		logger.Debug("codex: shutdown notify failed (best-effort)",
			logger.FieldAgentID, c.AgentID, logger.FieldError, err)
	}

	c.wsMu.Lock()
	if c.ws != nil {
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown")
		_ = c.ws.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
		_ = c.ws.Close()
	}
	c.wsMu.Unlock()

	select {
	case <-c.wsDone:
	case <-time.After(3 * time.Second):
	}

	if err := c.Kill(); err != nil {
		return err
	}

	if c.stderrCollector != nil {
		_ = c.stderrCollector.Close()
	}
	return nil
}

func (c *AppServerClient) Kill() error {
	if c.Cmd == nil || c.Cmd.Process == nil {
		return nil
	}
	pid := c.Cmd.Process.Pid
	killErr := syscall.Kill(-pid, syscall.SIGKILL)
	if killErr != nil {
		killErr = c.Cmd.Process.Kill()
	}
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return killErr
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- c.Cmd.Wait() }()
	select {
	case waitErr := <-waitDone:
		if waitErr == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return nil
		}
		waitMsg := waitErr.Error()
		if strings.Contains(waitMsg, "Wait was already called") || strings.Contains(waitMsg, "no child processes") {
			return nil
		}
		return waitErr
	case <-time.After(5 * time.Second):
		logger.Warn("codex: Kill() Cmd.Wait timed out after 5s, abandoning",
			logger.FieldAgentID, c.AgentID,
			"pid", c.Cmd.Process.Pid,
		)
		return nil
	}
}

func (c *AppServerClient) Running() bool {
	return !c.stopped.Load() && c.Cmd != nil && c.Cmd.ProcessState == nil
}

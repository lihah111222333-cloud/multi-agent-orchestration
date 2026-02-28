package codex

import (
	"encoding/json"
	"errors"
	"maps"
	"net"
	"strings"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const threadStatusChangedMethod = "thread/status/changed"

var lifecycleCompletionEventTypes = map[string]struct{}{
	EventTurnComplete:     {},
	"turn_aborted":        {},
	EventIdle:             {},
	EventError:            {},
	EventShutdownComplete: {},
}

var threadStatusChangedTerminalTypes = map[string]struct{}{
	"idle":         {},
	"systemerror":  {},
	"system_error": {},
	"error":        {},
	"notloaded":    {},
	"not_loaded":   {},
}

func (c *AppServerClient) readLoop() {
	if !c.readLoopRunning.CompareAndSwap(false, true) {
		return
	}
	connectionDead := false
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
		message, shouldStop, deadConnection := c.readLoopReadMessage()
		if deadConnection {
			connectionDead = true
		}
		if shouldStop {
			return
		}
		if message == nil {
			continue
		}
		if c.readLoopDispatchMessage(message) {
			return
		}
	}
}

func (c *AppServerClient) readLoopReadMessage() ([]byte, bool, bool) {
	conn := c.currentWSConn()
	if conn == nil {
		if c.stopped.Load() {
			return nil, true, false
		}
		if !c.reconnectWS("ws_missing", apperrors.New("AppServerClient.readLoop", "ws not connected")) {
			return nil, true, true
		}
		return nil, false, false
	}

	_, message, err := conn.ReadMessage()
	if err == nil {
		_ = conn.SetReadDeadline(time.Now().Add(currentAppServerReadIdleTimeout()))
		return message, false, false
	}
	return c.handleReadLoopReadError(err)
}

func (c *AppServerClient) handleReadLoopReadError(err error) ([]byte, bool, bool) {
	readErr := apperrors.Wrap(err, "AppServerClient.readLoop", "read message")
	health, shouldRespawn, openedCircuit := c.noteReadFailure(time.Now())
	c.failPendingCalls(readErr)

	if !c.stopped.Load() {
		willRetry := appServerStreamMaxRetries > 0
		if !willRetry {
			// 不可恢复的断连，发射 stream_error 通知 UI。
			details := map[string]any{
				"message":     "Stream disconnected",
				"attempt":     0,
				"max_retries": appServerStreamMaxRetries,
				"trigger":     "read_error",
			}
			maps.Copy(details, health.asDetailsMap())
			c.emitStreamError(readErr, "read", isIdleTimeoutError(err), false, details)
		}
		// willRetry=true 时不发射 stream_error：
		// 重连状态通过 attemptSingleReconnect 中的 background_event 展示，
		// 避免 stream_error → "Reconnecting..." 在 UI 永久残留。
	}

	if c.stopped.Load() && err != nil && strings.Contains(err.Error(), "use of closed network connection") {
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
		return nil, true, false
	}
	trigger := "read_error"
	if shouldRespawn {
		trigger = "read_error_burst"
	}
	if c.reconnectWS(trigger, readErr) {
		return nil, false, false
	}
	return nil, true, true
}

func (c *AppServerClient) readLoopDispatchMessage(message []byte) bool {
	var msg jsonRPCMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("codex: readLoop unparseable JSON-RPC message", logger.FieldAgentID, c.AgentID, logger.FieldError, err, "raw_prefix", truncateBytes(message, 200))
		return false
	}
	if dropped, preview, convID := shouldDropLegacyMirrorNotification(msg); dropped {
		logLegacyMirrorDrop(c.AgentID, msg.Method, convID, preview, c.legacyMirrorDropCount.Add(1))
		return false
	}
	if c.handleRPCResponse(msg) {
		return false
	}
	return c.handleRPCEvent(msg)
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
		logFn("codex: incoming event conversation mismatch", logger.FieldAgentID, c.AgentID, logger.FieldMethod, msg.Method, logger.FieldEventType, event.Type, logger.FieldThreadID, boundThreadID, "conversation_id", conversationID)
		if !isMCPStartupMethod(msg.Method) {
			recovered := shouldRecoverLifecycleOnMismatchedConversation(
				event,
				msg.Method,
				c.getActiveTurnID(),
				c.listenerEnsureNeeded.Load(),
			)
			if recovered {
				c.trackTurnLifecycle(event, msg.Method)
			}
			logger.Warn("codex: dropping mismatched thread-scoped event", logger.FieldAgentID, c.AgentID, logger.FieldMethod, msg.Method, logger.FieldThreadID, boundThreadID, "conversation_id", conversationID, "lifecycle_recovered", recovered)
			return false
		}
	}
	c.cancelStreamErrorRecoveryTimer()
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
	case EventStreamError:
		if streamErrorWillRetry(event.Data) || activeTurnID == "" {
			if streamErrorWillRetry(event.Data) && activeTurnID != "" {
				c.startStreamErrorRecoveryTimer(activeTurnID)
			}
			return
		}
		c.clearActiveTurnID()
	default:
		if isLifecycleCompletionEvent(event.Type) {
			if activeTurnID != "" {
				c.clearActiveTurnID()
			}
			return
		}
		if !strings.EqualFold(strings.TrimSpace(method), threadStatusChangedMethod) || activeTurnID == "" {
			return
		}
		if _, terminal := threadStatusChangedTerminalState(event.Data); terminal {
			c.clearActiveTurnID()
		}
	}
}

func isLifecycleCompletionEvent(eventType string) bool {
	_, ok := lifecycleCompletionEventTypes[eventType]
	return ok
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
	if isLifecycleCompletionEvent(event.Type) {
		return targetsActiveTurn
	}
	if event.Type == EventStreamError {
		return targetsActiveTurn && !streamErrorWillRetry(event.Data)
	}
	if !strings.EqualFold(strings.TrimSpace(method), threadStatusChangedMethod) {
		return false
	}
	_, terminal := threadStatusChangedTerminalState(event.Data)
	if !terminal {
		return false
	}
	if turnID != "" {
		return strings.EqualFold(turnID, activeTurnID)
	}
	return allowThreadTerminalWithoutTurnID
}

func threadStatusChangedTerminalState(raw json.RawMessage) (string, bool) {
	payload, ok := decodeJSONObject(raw)
	if !ok {
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
	_, terminal := threadStatusChangedTerminalTypes[statusType]
	return statusType, terminal
}

func extractTurnIDFromEventData(data json.RawMessage) string {
	payload, ok := decodeJSONObject(data)
	if !ok {
		return ""
	}
	return extractTurnIDFromPayload(payload)
}

var recursiveTurnPayloadKeys = [...]string{"msg", "data", "payload"}

func extractTurnIDFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if id := firstTrimmedString(payload, "turnId", "turn_id"); id != "" {
		return id
	}
	if turn := nestedPayload(payload, "turn"); turn != nil {
		if id := firstTrimmedString(turn, "id", "turnId"); id != "" {
			return id
		}
	}
	for _, key := range recursiveTurnPayloadKeys {
		if nested := nestedPayload(payload, key); nested != nil {
			if id := extractTurnIDFromPayload(nested); id != "" {
				return id
			}
		}
	}
	return ""
}

func firstTrimmedString(payload map[string]any, keys ...string) string {
	if payload == nil {
		return ""
	}
	for _, key := range keys {
		if value := trimmedStringValue(payload[key]); value != "" {
			return value
		}
	}
	return ""
}

func nestedPayload(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	nested, _ := payload[key].(map[string]any)
	return nested
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

var baseMethodToEventMap = map[string]string{
	"error":                                 EventError,
	"thread/started":                        EventSessionConfigured,
	"thread/name/updated":                   EventThreadNameUpdated,
	"thread/tokenUsage/updated":             EventTokenCount,
	"turn/started":                          EventTurnStarted,
	"turn/completed":                        EventTurnComplete,
	"turn/aborted":                          "turn_aborted",
	"turn/diff/updated":                     EventTurnDiff,
	"turn/plan/updated":                     EventPlanUpdate,
	"item/agentMessage/delta":               EventAgentMessageDelta,
	"item/plan/delta":                       EventPlanDelta,
	"item/commandExecution/outputDelta":     EventExecCommandOutputDelta,
	"item/reasoning/summaryTextDelta":       EventAgentReasoningDelta,
	"item/reasoning/summaryPartAdded":       EventAgentReasoningSectionBreak,
	"item/reasoning/textDelta":              EventAgentReasoningRawDelta,
	"thread/compacted":                      EventContextCompacted,
	"deprecationNotice":                     "deprecationNotice",
	"configWarning":                         EventWarning,
	"windows/worldWritableWarning":          EventWarning,
	"authStatusChange":                      "authStatusChange",
	"loginChatGptComplete":                  "loginChatGptComplete",
	"sessionConfigured":                     EventSessionConfigured,
	"item/commandExecution/requestApproval": EventExecApprovalRequest,
	"item/tool/call":                        EventDynamicToolCall,
	"applyPatchApproval":                    "applyPatchApproval",
	"execCommandApproval":                   EventExecApprovalRequest,
}

var sharedLegacySuffixToEventMap = map[string]string{
	"agent_message_content_delta":    EventAgentMessageDelta,
	"agent_message_delta":            EventAgentMessageDelta,
	"agent_message":                  EventAgentMessage,
	"agent_reasoning":                EventAgentReasoning,
	"agent_reasoning_raw":            EventAgentReasoningRaw,
	"agent_reasoning_raw_delta":      EventAgentReasoningRawDelta,
	"agent_reasoning_section_break":  EventAgentReasoningSectionBreak,
	"agent_reasoning_delta":          EventAgentReasoningDelta,
	"agent_message_completed":        EventAgentMessageCompleted,
	"turn_started":                   EventTurnStarted,
	"turn_completed":                 EventTurnComplete,
	"turn_aborted":                   "turn_aborted",
	"session_configured":             EventSessionConfigured,
	"mcp_startup_complete":           EventMCPStartupComplete,
	"shutdown_complete":              EventShutdownComplete,
	"error":                          EventError,
	"stream_error":                   EventStreamError,
	"warning":                        EventWarning,
	"exec_approval_request":          EventExecApprovalRequest,
	"exec_command_begin":             EventExecCommandBegin,
	"exec_command_end":               EventExecCommandEnd,
	"exec_command_output_delta":      EventExecCommandOutputDelta,
	"patch_apply_begin":              EventPatchApplyBegin,
	"patch_apply_end":                EventPatchApplyEnd,
	"mcp_tool_call_begin":            EventMCPToolCallBegin,
	"mcp_tool_call_end":              EventMCPToolCallEnd,
	"mcp_list_tools_response":        EventMCPListToolsResponse,
	"list_skills_response":           EventListSkillsResponse,
	"dynamic_tool_call":              EventDynamicToolCall,
	"collab_agent_spawn_begin":       EventCollabAgentSpawnBegin,
	"collab_agent_spawn_end":         EventCollabAgentSpawnEnd,
	"collab_agent_interaction_begin": EventCollabAgentInteractionBegin,
	"collab_agent_interaction_end":   EventCollabAgentInteractionEnd,
}

var codexOnlyLegacySuffixToEventMap = map[string]string{
	"task_started":            EventTurnStarted,
	"reasoning_content_delta": EventAgentReasoningDelta,
	"token_count":             EventTokenCount,
	"context_compacted":       EventContextCompacted,
	"thread_name_updated":     EventThreadNameUpdated,
	"thread_rolled_back":      EventThreadRolledBack,
	"plan_delta":              EventPlanDelta,
	"plan_update":             EventPlanUpdate,
	"item_started":            "item/started",
	"item_completed":          "item/completed",
	"raw_response_item":       "rawResponseItem/completed",
}

func buildMethodToEventMap() map[string]string {
	methodMap := make(map[string]string, 96)
	maps.Copy(methodMap, baseMethodToEventMap)
	for _, prefix := range [...]string{"agent/event/", "codex/event/"} {
		for suffix, eventType := range sharedLegacySuffixToEventMap {
			methodMap[prefix+suffix] = eventType
		}
	}
	for suffix, eventType := range codexOnlyLegacySuffixToEventMap {
		methodMap["codex/event/"+suffix] = eventType
	}
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
	payload, ok := decodeJSONObject(raw)
	if !ok {
		return raw
	}

	if errObj, ok := payload["error"].(map[string]any); ok && errObj != nil {
		if _, exists := payload["message"]; !exists {
			if msg := firstTrimmedString(errObj, "message"); msg != "" {
				payload["message"] = msg
			}
		}
		if _, exists := payload["additional_details"]; !exists {
			if details := firstTrimmedString(errObj, "additionalDetails", "additional_details"); details != "" {
				payload["additional_details"] = details
			}
		}
	}

	if _, exists := payload["willRetry"]; !exists {
		if v, ok := payload["will_retry"]; ok {
			payload["willRetry"] = v
		}
	}
	if _, exists := payload["will_retry"]; !exists {
		if v, ok := payload["willRetry"]; ok {
			payload["will_retry"] = v
		}
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return normalized
}

func decodeJSONObject(raw json.RawMessage) (map[string]any, bool) {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil || payload == nil {
		return nil, false
	}
	return payload, true
}

func truncateBytes(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}

const legacyMirrorDropLogSampleInterval int64 = 100

func logLegacyMirrorDrop(agentID, method, conversationID, preview string, seq int64) {
	if seq == 1 || seq%legacyMirrorDropLogSampleInterval == 0 {
		logger.Info("codex: dropped legacy mirror stream notification", logger.FieldAgentID, agentID, logger.FieldMethod, method, "conversation_id", conversationID, "preview", preview, "drop_count", seq)
		return
	}
	logger.Debug("codex: dropped legacy mirror stream notification", logger.FieldAgentID, agentID, logger.FieldMethod, method, "conversation_id", conversationID, "preview", preview)
}

func shouldDropLegacyMirrorNotification(msg jsonRPCMessage) (bool, string, string) {
	if msg.ID != nil {
		return false, "", ""
	}
	payload, ok := decodeJSONObject(msg.Params)
	if !ok {
		return false, "", ""
	}

	conversationID := firstTrimmedString(payload, "conversationId")
	if conversationID == "" {
		return false, "", ""
	}

	msgObj, ok := payload["msg"].(map[string]any)
	preview := extractLegacyMirrorPreview(msgObj)
	if !ok || preview == "" {
		return false, "", ""
	}

	if !isLegacyMirrorEnvelope(msg.Method, payload) {
		return false, "", ""
	}
	return true, preview, conversationID
}

func extractConversationIDFromEventParams(raw json.RawMessage) string {
	payload, ok := decodeJSONObject(raw)
	if !ok {
		return ""
	}
	if value := firstTrimmedString(payload, "conversationId", "conversation_id", "threadId", "thread_id"); value != "" {
		return value
	}
	return firstTrimmedString(nestedPayload(payload, "thread"), "id", "threadId", "thread_id")
}

func isLegacyMirrorEnvelope(method string, payload map[string]any) bool {
	if strings.HasPrefix(method, "agent/event/") || strings.HasPrefix(method, "codex/event/") {
		switch strings.TrimPrefix(strings.TrimPrefix(method, "agent/event/"), "codex/event/") {
		case "agent_message_delta", "agent_message_content_delta", "agent_reasoning_delta", "agent_reasoning_raw_delta", "exec_command_output_delta":
			return true
		case "reasoning_content_delta", "plan_delta":
			return strings.HasPrefix(method, "codex/event/")
		}
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
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "agent/event/mcp_startup_update", "agent/event/mcp_startup_complete", "codex/event/mcp_startup_update", "codex/event/mcp_startup_complete":
		return true
	default:
		return false
	}
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
	payload, ok := decodeJSONObject(raw)
	if !ok {
		return false
	}
	value, _ := extractBoolValue(payload, "willRetry", "will_retry", "recoverable")
	return value
}

func (c *AppServerClient) startStreamErrorRecoveryTimer(turnID string) {
	c.streamErrorRecoveryMu.Lock()
	defer c.streamErrorRecoveryMu.Unlock()

	if c.streamErrorRecoveryTimer != nil {
		c.streamErrorRecoveryTimer.Stop()
	}

	logger.Info("codex: stream_error recovery timer started",
		logger.FieldAgentID, c.AgentID,
		"turn_id", turnID,
		"timeout_s", streamErrorRecoveryTimeout.Seconds(),
	)

	c.streamErrorRecoveryTimer = time.AfterFunc(streamErrorRecoveryTimeout, func() {
		currentTurnID := c.getActiveTurnID()
		if currentTurnID == "" || currentTurnID != turnID {
			return
		}

		logger.Warn("codex: stream_error recovery timeout — no events received, aborting turn",
			logger.FieldAgentID, c.AgentID,
			"turn_id", turnID,
			"timeout_s", streamErrorRecoveryTimeout.Seconds(),
		)

		c.clearActiveTurnID()

		c.emitStreamError(
			errors.New("stream_error recovery timeout: no events received within "+streamErrorRecoveryTimeout.String()),
			"recovery_timeout",
			false,
			false,
			map[string]any{
				"message":        "Stream recovery failed — no events received after reconnection",
				"originalTurnId": turnID,
				"trigger":        "recovery_timeout",
			},
		)
	})
}

func (c *AppServerClient) cancelStreamErrorRecoveryTimer() {
	c.streamErrorRecoveryMu.Lock()
	defer c.streamErrorRecoveryMu.Unlock()

	if c.streamErrorRecoveryTimer != nil {
		c.streamErrorRecoveryTimer.Stop()
		c.streamErrorRecoveryTimer = nil
	}
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

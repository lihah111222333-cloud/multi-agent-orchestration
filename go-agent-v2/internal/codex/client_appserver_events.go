// client_appserver_events.go — 消息读取循环、事件处理、生命周期管理。
package codex

import (
	"context"
	"encoding/json"
	"errors"
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
	defer func() {
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
	}()

	for !c.stopped.Load() {
		conn := c.currentWSConn()
		if conn == nil {
			if c.stopped.Load() {
				return
			}
			if !c.reconnectWS("ws_missing", apperrors.New("AppServerClient.readLoop", "ws not connected")) {
				return
			}
			continue
		}
		_, message, err := conn.ReadMessage()
		if err == nil {
			// 收到有效消息 = 连接活跃, 重置 idle deadline。
			// 注意: 必须用循环内的 conn 局部变量, 不能用 c.currentWSConn(),
			// 因为 reconnect 后 c.ws 已指向新 conn。
			_ = conn.SetReadDeadline(time.Now().Add(appServerReadIdleTimeout))
		}
		if err != nil {
			readErr := apperrors.Wrap(err, "AppServerClient.readLoop", "read message")
			c.failPendingCalls(readErr)
			if !c.stopped.Load() {
				willRetry := appServerStreamMaxRetries > 0
				reconnectingMessage := "Reconnecting..."
				if !willRetry {
					reconnectingMessage = "Stream disconnected"
				}
				c.emitStreamError(readErr, "read", isIdleTimeoutError(err), willRetry, map[string]any{
					"message":     reconnectingMessage,
					"attempt":     0,
					"max_retries": appServerStreamMaxRetries,
					"trigger":     "read_error",
				})
			}
			if c.stopped.Load() && isShutdownReadError(err) {
				logger.Debug("codex: readLoop read failed (shutdown)",
					logger.FieldAgentID, c.AgentID,
					logger.FieldError, readErr,
				)
			} else {
				logger.Warn("codex: readLoop read failed",
					logger.FieldAgentID, c.AgentID,
					"active_turn_id", c.getActiveTurnID(),
					"idle_timeout", isIdleTimeoutError(err),
					logger.FieldError, readErr,
				)
			}
			if c.stopped.Load() {
				return
			}
			if c.reconnectWS("read_error", readErr) {
				continue
			}
			return
		}

		var msg jsonRPCMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Warn("codex: readLoop unparseable JSON-RPC message",
				logger.FieldAgentID, c.AgentID,
				logger.FieldError, err,
				"raw_len", len(message),
				"raw_prefix", truncateBytes(message, 200),
			)
			continue
		}
		if dropped, preview, convID := shouldDropLegacyMirrorNotification(msg); dropped {
			seq := c.legacyMirrorDropCount.Add(1)
			if shouldLogLegacyMirrorDrop(seq) {
				logger.Info("codex: dropped legacy mirror stream notification (sampled)",
					logger.FieldAgentID, c.AgentID,
					logger.FieldMethod, msg.Method,
					"conversation_id", convID,
					"preview", preview,
					"drop_count", seq,
				)
			} else {
				logger.Debug("codex: dropped legacy mirror stream notification",
					logger.FieldAgentID, c.AgentID,
					logger.FieldMethod, msg.Method,
					"conversation_id", convID,
					"preview", preview,
				)
			}
			continue
		}

		// Response: 交给 pending call
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
	value, ok := c.pending.Load(*msg.ID)
	if !ok {
		logger.Warn("codex: orphan RPC response (no pending call)",
			logger.FieldAgentID, c.AgentID,
			logger.FieldID, *msg.ID,
			"result_len", len(msg.Result),
		)
		return true
	}
	pc := value.(*pendingCall)
	if msg.Error != nil {
		pc.resolve(nil, apperrors.Newf("AppServerClient.readLoop", "rpc error: %s (code %d)", msg.Error.Message, msg.Error.Code))
		logger.Warn("codex: RPC error response",
			logger.FieldAgentID, c.AgentID,
			logger.FieldID, *msg.ID,
			"code", msg.Error.Code,
			"message", msg.Error.Message,
		)
	} else {
		pc.resolve(msg.Result, nil)
	}
	return true
}

func (c *AppServerClient) handleRPCEvent(msg jsonRPCMessage) bool {
	event := c.jsonRPCToEvent(msg)
	// DenyFunc: 审批事件 proc==nil 时自动拒绝, 闭包捕获当前 client。
	event.DenyFunc = func() error { return c.Submit("no", nil, nil, nil) }
	if event.Type == "" {
		logger.Warn("codex: readLoop skipped message with empty event type",
			logger.FieldAgentID, c.AgentID,
			logger.FieldMethod, msg.Method,
			"has_id", msg.ID != nil,
			logger.FieldParamsLen, len(msg.Params),
		)
		return false
	}
	if msg.ID != nil && msg.Method != "" {
		event.RequestID = msg.ID
		reqID := *msg.ID
		event.RespondFunc = func(code int, message string) error {
			return c.RespondError(reqID, code, message)
		}
		logger.Debug("codex: server request received",
			logger.FieldAgentID, c.AgentID,
			logger.FieldID, *msg.ID,
			logger.FieldMethod, msg.Method,
			logger.FieldEventType, event.Type,
		)
	}
	conversationID := extractConversationIDFromEventParams(msg.Params)
	boundThreadID := strings.TrimSpace(c.ThreadID)
	if conversationID != "" && boundThreadID != "" && !strings.EqualFold(conversationID, boundThreadID) {
		logger.Warn("codex: incoming event conversation mismatch",
			logger.FieldAgentID, c.AgentID,
			logger.FieldMethod, msg.Method,
			logger.FieldEventType, event.Type,
			logger.FieldThreadID, boundThreadID,
			"conversation_id", conversationID,
			"active_turn_id", c.getActiveTurnID(),
		)
	}
	// 跟踪活跃 turn 生命周期
	c.trackTurnLifecycle(event, msg.Method)

	c.handlerMu.RLock()
	handler := c.handler
	c.handlerMu.RUnlock()
	if handler == nil {
		logger.Warn("codex: readLoop dropping event (no handler registered)",
			logger.FieldAgentID, c.AgentID,
			logger.FieldEventType, event.Type,
			logger.FieldMethod, msg.Method,
		)
		return false
	}
	handler(event)
	return event.Type == EventShutdownComplete
}

// trackTurnLifecycle 从事件中提取并维护当前活跃 turnId。
func (c *AppServerClient) trackTurnLifecycle(event Event, method string) {
	method = strings.TrimSpace(method)
	activeTurnID := c.getActiveTurnID()

	switch event.Type {
	case EventTurnStarted:
		if turnID := extractTurnIDFromEventData(event.Data); turnID != "" {
			c.setActiveTurnID(turnID)
			logger.Debug("codex: active turn set",
				logger.FieldAgentID, c.AgentID,
				"turn_id", turnID,
			)
			return
		}
		logger.Warn("codex: turn started event missing turn id",
			logger.FieldAgentID, c.AgentID,
			logger.FieldMethod, method,
			logger.FieldEventType, event.Type,
			logger.FieldDataLen, len(event.Data),
			"active_turn_id_before", activeTurnID,
		)
	case EventTurnComplete, "turn_aborted", EventIdle, EventError, EventShutdownComplete:
		if activeTurnID != "" {
			c.clearActiveTurnID()
			logger.Debug("codex: active turn cleared",
				logger.FieldAgentID, c.AgentID,
				logger.FieldEventType, event.Type,
				logger.FieldMethod, method,
				"prev_turn_id", activeTurnID,
			)
		}
	case EventStreamError:
		if streamErrorWillRetry(event.Data) {
			return
		}
		if activeTurnID != "" {
			c.clearActiveTurnID()
			logger.Debug("codex: active turn cleared by non-retryable stream error",
				logger.FieldAgentID, c.AgentID,
				logger.FieldEventType, event.Type,
				logger.FieldMethod, method,
				"prev_turn_id", activeTurnID,
			)
		}
	default:
		if activeTurnID != "" && isTurnTailProgressEvent(event.Type, method) {
			logger.Info("codex: active turn observed progress event without terminal yet",
				logger.FieldAgentID, c.AgentID,
				"active_turn_id", activeTurnID,
				logger.FieldEventType, event.Type,
				logger.FieldMethod, method,
				logger.FieldDataLen, len(event.Data),
			)
		}
	}
}

func isTurnTailProgressEvent(eventType, method string) bool {
	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))

	switch methodKey {
	case "turn/diff/updated", "turn/plan/updated", "codex/event/turn_diff", "codex/event/plan_delta", "codex/event/plan_update":
		return true
	}
	switch eventKey {
	case EventTurnDiff, EventPlanDelta, EventPlanUpdate:
		return true
	default:
		return false
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

// jsonRPCToEvent 将 JSON-RPC notification 转为 codex Event。
//
// app-server 通知方法映射:
//
//	"agent/event/agent_message_content_delta" → "agent_message_delta"
//	"agent/event/turn_completed" → "turn_complete"
//	"agent/event/dynamic_tool_call" → "dynamic_tool_call"
//	"agent/event/mcp_startup_complete" → "mcp_startup_complete"
//	etc.
//
// methodToEventMap 包级 var — 零分配热路径查找。
//
// JSON-RPC notification method → codex Event type。
var methodToEventMap = map[string]string{
	// v2 server notifications
	"error":                                     EventError,
	"thread/started":                            EventSessionConfigured,
	"thread/name/updated":                       EventThreadNameUpdated,
	"thread/tokenUsage/updated":                 EventTokenCount,
	"turn/started":                              EventTurnStarted,
	"turn/completed":                            EventTurnComplete,
	"turn/aborted":                              "turn_aborted",
	"turn/diff/updated":                         EventTurnDiff,
	"turn/plan/updated":                         EventPlanUpdate,
	"item/started":                              "item/started",
	"item/completed":                            "item/completed",
	"rawResponseItem/completed":                 "rawResponseItem/completed",
	"item/agentMessage/delta":                   EventAgentMessageDelta,
	"item/plan/delta":                           EventPlanDelta,
	"item/commandExecution/outputDelta":         EventExecCommandOutputDelta,
	"item/commandExecution/terminalInteraction": "item/commandExecution/terminalInteraction",
	"item/fileChange/outputDelta":               "item/fileChange/outputDelta",
	"item/mcpToolCall/progress":                 "item/mcpToolCall/progress",
	"mcpServer/oauthLogin/completed":            "mcpServer/oauthLogin/completed",
	"account/updated":                           "account/updated",
	"account/rateLimits/updated":                "account/rateLimits/updated",
	"app/list/updated":                          "app/list/updated",
	"item/reasoning/summaryTextDelta":           EventAgentReasoningDelta,
	"item/reasoning/summaryPartAdded":           EventAgentReasoningSectionBreak,
	"item/reasoning/textDelta":                  EventAgentReasoningRawDelta,
	"thread/compacted":                          EventContextCompacted,
	"deprecationNotice":                         "deprecationNotice",
	"configWarning":                             EventWarning,
	"fuzzyFileSearch/sessionUpdated":            "fuzzyFileSearch/sessionUpdated",
	"fuzzyFileSearch/sessionCompleted":          "fuzzyFileSearch/sessionCompleted",
	"windows/worldWritableWarning":              EventWarning,
	"account/login/completed":                   "account/login/completed",
	"authStatusChange":                          "authStatusChange",
	"loginChatGptComplete":                      "loginChatGptComplete",
	"sessionConfigured":                         EventSessionConfigured,

	// v2 server requests
	"item/commandExecution/requestApproval": EventExecApprovalRequest,
	"item/fileChange/requestApproval":       "item/fileChange/requestApproval",
	"item/tool/requestUserInput":            "item/tool/requestUserInput",
	"item/tool/call":                        EventDynamicToolCall,
	"account/chatgptAuthTokens/refresh":     "account/chatgptAuthTokens/refresh",
	"applyPatchApproval":                    "applyPatchApproval",
	"execCommandApproval":                   EventExecApprovalRequest,

	// Agent 输出
	"agent/event/agent_message_content_delta":   EventAgentMessageDelta,
	"agent/event/agent_message_delta":           EventAgentMessageDelta,
	"agent/event/agent_message":                 EventAgentMessage,
	"agent/event/agent_reasoning":               EventAgentReasoning,
	"agent/event/agent_reasoning_raw":           EventAgentReasoningRaw,
	"agent/event/agent_reasoning_raw_delta":     EventAgentReasoningRawDelta,
	"agent/event/agent_reasoning_section_break": EventAgentReasoningSectionBreak,
	"agent/event/agent_reasoning_delta":         EventAgentReasoningDelta,
	"agent/event/agent_message_completed":       EventAgentMessageCompleted,

	// 生命周期
	"agent/event/turn_started":         EventTurnStarted,
	"agent/event/turn_completed":       EventTurnComplete,
	"agent/event/turn_aborted":         "turn_aborted",
	"agent/event/session_configured":   EventSessionConfigured,
	"agent/event/mcp_startup_complete": EventMCPStartupComplete,
	"agent/event/mcp_startup_update":   "agent/event/mcp_startup_update",
	"agent/event/shutdown_complete":    EventShutdownComplete,
	"agent/event/error":                EventError,
	"agent/event/stream_error":         EventStreamError,
	"agent/event/warning":              EventWarning,

	// 命令执行
	"agent/event/exec_approval_request":     EventExecApprovalRequest,
	"agent/event/exec_command_begin":        EventExecCommandBegin,
	"agent/event/exec_command_end":          EventExecCommandEnd,
	"agent/event/exec_command_output_delta": EventExecCommandOutputDelta,

	// 代码修改
	"agent/event/patch_apply_begin": EventPatchApplyBegin,
	"agent/event/patch_apply_end":   EventPatchApplyEnd,

	// MCP
	"agent/event/mcp_tool_call_begin":     EventMCPToolCallBegin,
	"agent/event/mcp_tool_call_end":       EventMCPToolCallEnd,
	"agent/event/mcp_list_tools_response": EventMCPListToolsResponse,
	"agent/event/list_skills_response":    EventListSkillsResponse,

	// Dynamic Tools
	// 注意:
	//   - `item/tool/call` 才是 v2 正式 Server Request（需 JSON-RPC response）。
	//   - `codex/event/dynamic_tool_call_request` 是 raw event 通知副本，不应驱动工具回传。
	//     否则会出现“处理了通知副本但未响应真实 request”，导致 turn 卡住。
	//
	// 兼容保留:
	//   - agent/event/dynamic_tool_call
	//   - codex/event/dynamic_tool_call
	"agent/event/dynamic_tool_call": EventDynamicToolCall,
	"codex/event/dynamic_tool_call": EventDynamicToolCall,

	// Collab
	"agent/event/collab_agent_spawn_begin":       EventCollabAgentSpawnBegin,
	"agent/event/collab_agent_spawn_end":         EventCollabAgentSpawnEnd,
	"agent/event/collab_agent_interaction_begin": EventCollabAgentInteractionBegin,
	"agent/event/collab_agent_interaction_end":   EventCollabAgentInteractionEnd,

	// legacy codex/event/*
	"codex/event/task_started": EventTurnStarted,
	// ⚠️ DO NOT DELETE / DO NOT MODIFY — 以下注释和行为是刻意设计。
	// `codex/event/task_complete` 故意不映射为 EventTurnComplete, 因为 v2 协议会单独发送 turn/completed。
	// 若映射则会导致 turn_complete 被双重触发 (一次来自此映射, 一次来自 turn/completed)。
	// runner 层通过 uistate.NormalizeEvent 独立处理 last_agent_message 提取, 不依赖此映射。
	"codex/event/session_configured":             EventSessionConfigured,
	"codex/event/agent_message":                  EventAgentMessage,
	"codex/event/agent_message_delta":            EventAgentMessageDelta,
	"codex/event/agent_message_content_delta":    EventAgentMessageDelta,
	"codex/event/agent_message_completed":        EventAgentMessageCompleted,
	"codex/event/agent_reasoning":                EventAgentReasoning,
	"codex/event/agent_reasoning_delta":          EventAgentReasoningDelta,
	"codex/event/agent_reasoning_raw":            EventAgentReasoningRaw,
	"codex/event/agent_reasoning_raw_delta":      EventAgentReasoningRawDelta,
	"codex/event/agent_reasoning_section_break":  EventAgentReasoningSectionBreak,
	"codex/event/reasoning_content_delta":        EventAgentReasoningDelta,
	"codex/event/exec_approval_request":          EventExecApprovalRequest,
	"codex/event/exec_command_begin":             EventExecCommandBegin,
	"codex/event/exec_command_end":               EventExecCommandEnd,
	"codex/event/exec_command_output_delta":      EventExecCommandOutputDelta,
	"codex/event/patch_apply_begin":              EventPatchApplyBegin,
	"codex/event/patch_apply_end":                EventPatchApplyEnd,
	"codex/event/mcp_tool_call_begin":            EventMCPToolCallBegin,
	"codex/event/mcp_tool_call_end":              EventMCPToolCallEnd,
	"codex/event/mcp_list_tools_response":        EventMCPListToolsResponse,
	"codex/event/list_skills_response":           EventListSkillsResponse,
	"codex/event/mcp_startup_complete":           EventMCPStartupComplete,
	"codex/event/mcp_startup_update":             "codex/event/mcp_startup_update",
	"codex/event/token_count":                    EventTokenCount,
	"codex/event/context_compacted":              EventContextCompacted,
	"codex/event/thread_name_updated":            EventThreadNameUpdated,
	"codex/event/thread_rolled_back":             EventThreadRolledBack,
	"codex/event/plan_delta":                     EventPlanDelta,
	"codex/event/plan_update":                    EventPlanUpdate,
	"codex/event/collab_agent_spawn_begin":       EventCollabAgentSpawnBegin,
	"codex/event/collab_agent_spawn_end":         EventCollabAgentSpawnEnd,
	"codex/event/collab_agent_interaction_begin": EventCollabAgentInteractionBegin,
	"codex/event/collab_agent_interaction_end":   EventCollabAgentInteractionEnd,
	"codex/event/item_started":                   "item/started",
	"codex/event/item_completed":                 "item/completed",
	"codex/event/raw_response_item":              "rawResponseItem/completed",
	"codex/event/error":                          EventError,
	"codex/event/stream_error":                   EventStreamError,
	"codex/event/warning":                        EventWarning,
	"codex/event/shutdown_complete":              EventShutdownComplete,
}

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
		// 未知方法 → 用 method 名作为 type (兼容) + 警告日志
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

// ========================================
// 辅助
// ========================================

// syncKeys 双向同步 map 中的两个键: 若 k1 缺失则从 k2 复制, 反之亦然。
// 用于 camelCase/snake_case 键兼容 (如 willRetry / will_retry)。
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

// truncateBytes 截断 []byte 用于日志展示, 避免超长。
func truncateBytes(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}

// isShutdownReadError 判断 readLoop 错误是否由正常关闭触发。
// shutdown 引起的 "use of closed network connection" 不需要 WARN, 降级为 DEBUG。
func isShutdownReadError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

// legacyMirrorDropLogSampleInterval 每 N 次 legacy mirror 丢弃打一次 INFO 采样日志。
const legacyMirrorDropLogSampleInterval int64 = 100

// shouldLogLegacyMirrorDrop 决定第 seq 次 legacy mirror 丢弃是否应打 INFO 日志。
// 策略: 第 1 次 + 每 100 次。其余降级为 DEBUG。
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
	if thread, ok := payload["thread"].(map[string]any); ok {
		if value := trimmedStringValue(thread["id"]); value != "" {
			return value
		}
	}
	return ""
}

func isLegacyMirrorEnvelope(method string, payload map[string]any) bool {
	if _, ok := legacyMirrorStreamMethods[method]; ok {
		return true
	}

	if _, ok := payload["threadId"]; ok {
		return false
	}
	if _, ok := payload["turnId"]; ok {
		return false
	}
	if _, ok := payload["itemId"]; ok {
		return false
	}
	if _, ok := payload["outputIndex"]; ok {
		return false
	}
	if _, ok := payload["contentIndex"]; ok {
		return false
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

// asWriteJSON 线程安全写入 WebSocket JSON。
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
		for key, value := range details {
			payload[key] = value
		}
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

// SpawnAndConnect 一键启动: spawn → ws connect → initialize → thread/start。
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

// Shutdown 优雅关闭。
func (c *AppServerClient) Shutdown() error {
	if c.stopped.Swap(true) {
		return nil
	}
	c.cancel()

	// 尝试发送 shutdown 通知 (best-effort)
	if err := c.notify("shutdown", nil); err != nil {
		logger.Debug("codex: shutdown notify failed (best-effort)",
			logger.FieldAgentID, c.AgentID, logger.FieldError, err)
	}

	// 主动关闭 ws 以立即中断 readLoop 的 ReadMessage 阻塞,
	// 避免等待 75s 的 read idle deadline。
	c.wsMu.Lock()
	if c.ws != nil {
		// 先发 WebSocket Close Frame, 给对端时间优雅关闭。
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

	// stderrCollector.Close() 必须在 Kill() 之后:
	// Close() 等待 scan goroutine 退出, 而 scan 阻塞在 pipe read 上。
	// 只有子进程退出后, OS 关闭 pipe 的写端, scan 才能读到 EOF 并退出。
	if c.stderrCollector != nil {
		_ = c.stderrCollector.Close()
	}
	return nil
}

// Kill 强制终止子进程。
func (c *AppServerClient) Kill() error {
	if c.Cmd == nil || c.Cmd.Process == nil {
		return nil
	}
	// 尝试杀掉整个进程组 (Setpgid=true 时 pgid == pid)。
	// 回退: 如果进程组 kill 失败, 直接 kill 进程本身。
	pid := c.Cmd.Process.Pid
	killErr := syscall.Kill(-pid, syscall.SIGKILL)
	if killErr != nil {
		killErr = c.Cmd.Process.Kill()
	}
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return killErr
	}
	// Cmd.Wait 可能因 pipe-copying goroutine 未退出而阻塞, 加超时保护。
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

// Running 返回是否在运行。
func (c *AppServerClient) Running() bool {
	return !c.stopped.Load() && c.Cmd != nil && c.Cmd.ProcessState == nil
}

// truncStr 截断字符串用于日志输出。
func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

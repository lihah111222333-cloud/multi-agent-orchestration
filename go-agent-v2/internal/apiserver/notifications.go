// notifications.go — Codex Agent 事件 → JSON-RPC Notification 映射。
//
// 完整对标 codex app-server-protocol v2 通知规范。
// 参考: APP-SERVER-PROTOCOL.md § 三、四
package apiserver

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// eventMethodMap Codex 事件类型 → JSON-RPC 通知方法名。
//
// 按协议分组:
//   - thread/* 线程/轮次事件
//   - item/*   Agent 动作事件
//   - account/* 账号推送
//   - 搜索/配置/错误
var eventMethodMap = map[string]string{
	// ── 线程/轮次事件 ──
	"session_configured":  "thread/started",
	"thread_name_updated": "thread/name/updated",
	"token_count":         "thread/tokenUsage/updated",
	"turn_started":        "turn/started",
	"turn_complete":       "turn/completed",
	"idle":                "turn/completed",
	"turn_diff":           "turn/diff/updated",
	"turn_plan":           "turn/plan/updated",
	"plan_update":         "turn/plan/updated",
	"context_compacted":   "thread/compacted",
	"thread_rolled_back":  "codex/event/thread_rolled_back",

	// ── Item 事件 ──
	"agent_message":                 "item/started",
	"agent_message_delta":           "item/agentMessage/delta",
	"agent_message_content_delta":   "item/agentMessage/delta",
	"agent_message_completed":       "item/completed",
	"agent_reasoning":               "item/reasoning/textDelta",
	"agent_reasoning_delta":         "item/reasoning/summaryTextDelta",
	"agent_reasoning_raw":           "item/reasoning/textDelta",
	"agent_reasoning_raw_delta":     "item/reasoning/textDelta",
	"agent_reasoning_section_break": "item/reasoning/summaryPartAdded",

	"exec_command_begin":           "item/started",
	"exec_command_end":             "item/completed",
	"exec_output_delta":            "item/commandExecution/outputDelta",
	"exec_command_output_delta":    "item/commandExecution/outputDelta",
	"exec_approval_request":        "item/commandExecution/requestApproval",
	"exec_terminal_interaction":    "item/commandExecution/terminalInteraction",
	"file_change_approval_request": "item/fileChange/requestApproval",

	"patch_apply":       "item/fileChange/outputDelta",
	"patch_apply_begin": "item/started",
	"patch_apply_end":   "item/completed",
	"file_read":         "item/started",
	"file_updated":      "item/completed",

	// ── Dynamic Tools ──
	"dynamic_tool_call": "item/tool/call",

	// ── 推理事件 ──
	"reasoning":              "item/reasoning/textDelta",
	"reasoning_delta":        "item/reasoning/summaryTextDelta",
	"reasoning_summary":      "item/reasoning/summaryTextDelta",
	"reasoning_summary_part": "item/reasoning/summaryPartAdded",

	// ── MCP ──
	"mcp_tool_call_begin":     "item/started",
	"mcp_tool_call_end":       "item/completed",
	"mcp_tool_call":           "item/started",
	"mcp_tool_progress":       "item/mcpToolCall/progress",
	"mcp_list_tools_response": "codex/event/mcp_list_tools_response",
	"mcp_startup_update":      "codex/event/mcp_startup_update",
	"mcp_startup_complete":    "codex/event/mcp_startup_complete",
	"mcp_oauth_completed":     "mcpServer/oauthLogin/completed",

	// ── 计划 ──
	"plan_delta": "item/plan/delta",

	// ── 协作 ──
	"collab_agent_spawn_begin":       "item/started",
	"collab_agent_spawn_end":         "item/completed",
	"collab_agent_interaction_begin": "item/started",
	"collab_agent_interaction_end":   "item/completed",
	"collab_waiting_begin":           "item/started",
	"collab_waiting_end":             "item/completed",
	"collab_agent_launched":          "item/started",
	"collab_agent_completed":         "item/completed",
	"entered_review_mode":            "item/started",
	"exited_review_mode":             "item/completed",

	// ── 账号/配置推送 ──
	"account_updated":     "account/updated",
	"login_completed":     "account/login/completed",
	"rate_limits_updated": "account/rateLimits/updated",
	"app_list_updated":    "app/list/updated",

	// ── 搜索推送 ──
	"fuzzy_search_updated":   "fuzzyFileSearch/sessionUpdated",
	"fuzzy_search_completed": "fuzzyFileSearch/sessionCompleted",

	// ── 错误/配置/弃用 ──
	"error":              "error",
	"warning":            "configWarning",
	"deprecation_notice": "deprecationNotice",
	"shutdown_complete":  "codex/event/shutdown_complete",
	"stream_error":       "codex/event/stream_error",
	"background_event":   "codex/event/background_event",

	// ── Skills ──
	"list_skills_response": "codex/event/list_skills_response",
}

var passthroughEventPrefixes = [...]string{
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

// mapEventToMethod 将 Codex 事件类型转换为 JSON-RPC 通知方法名。
//
// 未知事件: "agent/event/{type}" + WARN 日志。
func mapEventToMethod(eventType string) string {
	if method, ok := eventMethodMap[eventType]; ok {
		return method
	}
	for _, prefix := range passthroughEventPrefixes {
		if strings.HasPrefix(eventType, prefix) {
			return eventType
		}
	}
	if strings.Contains(eventType, "/") {
		return eventType
	}
	logger.Warn("app-server: unmapped event type → fallback to agent/event/ prefix",
		logger.FieldEventType, eventType,
	)
	return "agent/event/" + eventType
}

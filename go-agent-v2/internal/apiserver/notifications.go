// notifications.go — Codex Agent 事件 → JSON-RPC Notification 映射。
//
// 完整对标 codex app-server-protocol v2 通知规范。
// 参考: APP-SERVER-PROTOCOL.md § 三、四
package apiserver

import "log/slog"

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
	"context_compacted":   "thread/compacted",

	// ── Item 事件 ──
	"agent_message":       "item/started",
	"agent_message_delta": "item/agentMessage/delta",

	"exec_command_begin":        "item/started",
	"exec_command_end":          "item/completed",
	"exec_output_delta":         "item/commandExecution/outputDelta",
	"exec_approval_request":     "item/commandExecution/requestApproval",
	"exec_terminal_interaction": "item/commandExecution/terminalInteraction",

	"patch_apply":       "item/fileChange/outputDelta",
	"patch_apply_begin": "item/fileChange/started",
	"patch_apply_end":   "item/fileChange/completed",
	"file_read":         "item/started",
	"file_updated":      "item/completed",

	// ── Dynamic Tools ──
	"dynamic_tool_call": "item/dynamicToolCall",

	// ── 推理事件 ──
	"reasoning":              "item/reasoning/textDelta",
	"reasoning_delta":        "item/reasoning/textDelta",
	"reasoning_summary":      "item/reasoning/summaryTextDelta",
	"reasoning_summary_part": "item/reasoning/summaryPartAdded",

	// ── MCP ──
	"mcp_tool_call":           "item/started",
	"mcp_tool_progress":       "item/mcpToolCall/progress",
	"mcp_list_tools_response": "item/completed",
	"mcp_oauth_completed":     "mcpServer/oauthLogin/completed",

	// ── 计划 ──
	"plan_delta": "item/plan/delta",

	// ── 协作 ──
	"collab_agent_launched":  "item/started",
	"collab_agent_completed": "item/completed",

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
	"shutdown_complete":  "thread/shutdown",

	// ── Skills ──
	"list_skills_response": "skills/list/response",
}

// mapEventToMethod 将 Codex 事件类型转换为 JSON-RPC 通知方法名。
//
// 未知事件: "agent/event/{type}" + WARN 日志。
func mapEventToMethod(eventType string) string {
	if method, ok := eventMethodMap[eventType]; ok {
		return method
	}
	slog.Warn("app-server: unmapped event type → fallback to agent/event/ prefix",
		"event_type", eventType,
	)
	return "agent/event/" + eventType
}

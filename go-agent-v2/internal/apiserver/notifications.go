// notifications.go — Codex Agent 事件 → JSON-RPC Notification 映射。
//
// 完整对标 codex app-server-protocol v2 通知规范。
// 参考: APP-SERVER-PROTOCOL.md § 三、四
package apiserver

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
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
	agentcore.EventSessionConfigured: "thread/started",
	agentcore.EventThreadNameUpdated: "thread/name/updated",
	agentcore.EventTokenCount:        "thread/tokenUsage/updated",
	agentcore.EventTurnStarted:       "turn/started",
	agentcore.EventTurnComplete:      "turn/completed",
	agentcore.EventTurnAborted:       "turn/completed",
	agentcore.EventIdle:              "turn/completed",
	agentcore.EventTurnDiff:          "turn/diff/updated",
	agentcore.EventTurnPlan:          "turn/plan/updated",
	agentcore.EventPlanUpdate:        "turn/plan/updated",
	agentcore.EventContextCompacted:  "thread/compacted",
	agentcore.EventThreadRolledBack:  "codex/event/thread_rolled_back",

	// ── Item 事件 ──
	agentcore.EventAgentMessage:               "item/started",
	agentcore.EventAgentMessageDelta:          "item/agentMessage/delta",
	agentcore.EventAgentMessageContentDelta:   "item/agentMessage/delta",
	agentcore.EventAgentMessageCompleted:      "item/completed",
	agentcore.EventAgentReasoning:             "item/reasoning/textDelta",
	agentcore.EventAgentReasoningDelta:        "item/reasoning/summaryTextDelta",
	agentcore.EventAgentReasoningRaw:          "item/reasoning/textDelta",
	agentcore.EventAgentReasoningRawDelta:     "item/reasoning/textDelta",
	agentcore.EventAgentReasoningSectionBreak: "item/reasoning/summaryPartAdded",

	agentcore.EventExecCommandBegin:          "item/started",
	agentcore.EventExecCommandEnd:            "item/completed",
	"exec_output_delta":                      "item/commandExecution/outputDelta", // apiserver-only legacy alias, no agentcore constant
	agentcore.EventExecCommandOutputDelta:    "item/commandExecution/outputDelta",
	agentcore.EventExecApprovalRequest:       "item/commandExecution/requestApproval",
	agentcore.EventExecTerminalInteraction:   "item/commandExecution/terminalInteraction",
	agentcore.EventFileChangeApprovalRequest: "item/fileChange/requestApproval",

	agentcore.EventPatchApply:      "item/fileChange/outputDelta",
	agentcore.EventPatchApplyBegin: "item/started",
	agentcore.EventPatchApplyEnd:   "item/completed",
	agentcore.EventFileRead:        "item/started",
	agentcore.EventFileUpdated:     "item/completed",

	// ── Dynamic Tools ──
	agentcore.EventDynamicToolCall: "item/tool/call",

	// ── 推理事件 ──
	agentcore.EventReasoning:            "item/reasoning/textDelta",
	agentcore.EventReasoningDelta:       "item/reasoning/summaryTextDelta",
	agentcore.EventReasoningSummary:     "item/reasoning/summaryTextDelta",
	agentcore.EventReasoningSummaryPart: "item/reasoning/summaryPartAdded",

	// ── MCP ──
	agentcore.EventMCPToolCallBegin:     "item/started",
	agentcore.EventMCPToolCallEnd:       "item/completed",
	agentcore.EventMCPToolCall:          "item/started",
	agentcore.EventMCPToolProgress:      "item/mcpToolCall/progress",
	agentcore.EventMCPListToolsResponse: "codex/event/mcp_list_tools_response",
	agentcore.EventMCPStartupUpdate:     "codex/event/mcp_startup_update",
	agentcore.EventMCPStartupComplete:   "codex/event/mcp_startup_complete",
	agentcore.EventMCPOAuthCompleted:    "mcpServer/oauthLogin/completed",

	// ── 计划 ──
	agentcore.EventPlanDelta: "item/plan/delta",

	// ── 协作 ──
	agentcore.EventCollabAgentSpawnBegin:       "item/started",
	agentcore.EventCollabAgentSpawnEnd:         "item/completed",
	agentcore.EventCollabAgentInteractionBegin: "item/started",
	agentcore.EventCollabAgentInteractionEnd:   "item/completed",
	agentcore.EventCollabWaitingBegin:          "item/started",
	agentcore.EventCollabWaitingEnd:            "item/completed",
	"collab_agent_launched":                    "item/started",   // apiserver-only, no agentcore constant
	"collab_agent_completed":                   "item/completed", // apiserver-only, no agentcore constant
	agentcore.EventEnteredReviewMode:           "item/started",
	agentcore.EventExitedReviewMode:            "item/completed",

	// ── 账号/配置推送 ──
	"account_updated":     "account/updated",            // apiserver-only, no agentcore constant
	"login_completed":     "account/login/completed",    // apiserver-only, no agentcore constant
	"rate_limits_updated": "account/rateLimits/updated", // apiserver-only, no agentcore constant
	"app_list_updated":    "app/list/updated",           // apiserver-only, no agentcore constant

	// ── 搜索推送 ──
	"fuzzy_search_updated":   "fuzzyFileSearch/sessionUpdated",   // apiserver-only, no agentcore constant
	"fuzzy_search_completed": "fuzzyFileSearch/sessionCompleted", // apiserver-only, no agentcore constant

	// ── 错误/配置/弃用 ──
	agentcore.EventError:            "error",
	agentcore.EventWarning:          "configWarning",
	"deprecation_notice":            "deprecationNotice", // apiserver-only, no agentcore constant
	agentcore.EventShutdownComplete: "codex/event/shutdown_complete",
	agentcore.EventStreamError:      "error",
	agentcore.EventBackgroundEvent:  "codex/event/background_event",

	// ── Skills ──
	agentcore.EventListSkillsResponse: "codex/event/list_skills_response",

	// ── Agent 间消息 ──
	"user_message": "codex/event/user_message",
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

package apiserver

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

var eventMethodMap = map[string]string{
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

	agentcore.EventDynamicToolCall: "item/tool/call",

	agentcore.EventReasoning:            "item/reasoning/textDelta",
	agentcore.EventReasoningDelta:       "item/reasoning/summaryTextDelta",
	agentcore.EventReasoningSummary:     "item/reasoning/summaryTextDelta",
	agentcore.EventReasoningSummaryPart: "item/reasoning/summaryPartAdded",

	agentcore.EventMCPToolCallBegin:     "item/started",
	agentcore.EventMCPToolCallEnd:       "item/completed",
	agentcore.EventMCPToolCall:          "item/started",
	agentcore.EventMCPToolProgress:      "item/mcpToolCall/progress",
	agentcore.EventMCPListToolsResponse: "codex/event/mcp_list_tools_response",
	agentcore.EventMCPStartupUpdate:     "codex/event/mcp_startup_update",
	agentcore.EventMCPStartupComplete:   "codex/event/mcp_startup_complete",
	agentcore.EventMCPOAuthCompleted:    "mcpServer/oauthLogin/completed",

	agentcore.EventPlanDelta: "item/plan/delta",

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

	"account_updated":     "account/updated",            // apiserver-only, no agentcore constant
	"login_completed":     "account/login/completed",    // apiserver-only, no agentcore constant
	"rate_limits_updated": "account/rateLimits/updated", // apiserver-only, no agentcore constant
	"app_list_updated":    "app/list/updated",           // apiserver-only, no agentcore constant

	"fuzzy_search_updated":   "fuzzyFileSearch/sessionUpdated",   // apiserver-only, no agentcore constant
	"fuzzy_search_completed": "fuzzyFileSearch/sessionCompleted", // apiserver-only, no agentcore constant

	agentcore.EventError:            "error",
	agentcore.EventWarning:          "configWarning",
	"deprecation_notice":            "deprecationNotice", // apiserver-only, no agentcore constant
	agentcore.EventShutdownComplete: "codex/event/shutdown_complete",
	agentcore.EventStreamError:      "error",
	agentcore.EventBackgroundEvent:  "codex/event/background_event",

	agentcore.EventListSkillsResponse: "codex/event/list_skills_response",

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

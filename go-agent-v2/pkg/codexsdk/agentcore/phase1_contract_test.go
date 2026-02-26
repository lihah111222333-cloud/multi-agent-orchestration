package agentcore_test

import (
	"encoding/json"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/codex"
)

var (
	_ agentcore.Event = codex.Event{}
	_ codex.Event     = agentcore.Event{}

	_ agentcore.DynamicTool = codex.DynamicTool{}
	_ codex.DynamicTool     = agentcore.DynamicTool{}

	_ agentcore.Client  = (*codex.Client)(nil)
	_ agentcore.Client  = (*codex.AppServerClient)(nil)
	_ codex.CodexClient = (*codex.Client)(nil)
	_ codex.CodexClient = (*codex.AppServerClient)(nil)
)

func TestEventConstantsContract(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"EventSessionConfigured":           agentcore.EventSessionConfigured,
		"EventTurnStarted":                 agentcore.EventTurnStarted,
		"EventTurnComplete":                agentcore.EventTurnComplete,
		"EventIdle":                        agentcore.EventIdle,
		"EventError":                       agentcore.EventError,
		"EventShutdownComplete":            agentcore.EventShutdownComplete,
		"EventAgentMessage":                agentcore.EventAgentMessage,
		"EventAgentMessageDelta":           agentcore.EventAgentMessageDelta,
		"EventAgentMessageContentDelta":    agentcore.EventAgentMessageContentDelta,
		"EventAgentReasoning":              agentcore.EventAgentReasoning,
		"EventAgentReasoningDelta":         agentcore.EventAgentReasoningDelta,
		"EventAgentReasoningRaw":           agentcore.EventAgentReasoningRaw,
		"EventAgentReasoningRawDelta":      agentcore.EventAgentReasoningRawDelta,
		"EventAgentReasoningSectionBreak":  agentcore.EventAgentReasoningSectionBreak,
		"EventExecApprovalRequest":         agentcore.EventExecApprovalRequest,
		"EventExecCommandBegin":            agentcore.EventExecCommandBegin,
		"EventExecCommandOutputDelta":      agentcore.EventExecCommandOutputDelta,
		"EventExecCommandEnd":              agentcore.EventExecCommandEnd,
		"EventPatchApplyBegin":             agentcore.EventPatchApplyBegin,
		"EventPatchApplyEnd":               agentcore.EventPatchApplyEnd,
		"EventTurnDiff":                    agentcore.EventTurnDiff,
		"EventUndoStarted":                 agentcore.EventUndoStarted,
		"EventUndoCompleted":               agentcore.EventUndoCompleted,
		"EventMCPToolCallBegin":            agentcore.EventMCPToolCallBegin,
		"EventMCPToolCallEnd":              agentcore.EventMCPToolCallEnd,
		"EventMCPListToolsResponse":        agentcore.EventMCPListToolsResponse,
		"EventListSkillsResponse":          agentcore.EventListSkillsResponse,
		"EventEnteredReviewMode":           agentcore.EventEnteredReviewMode,
		"EventExitedReviewMode":            agentcore.EventExitedReviewMode,
		"EventCollabAgentSpawnBegin":       agentcore.EventCollabAgentSpawnBegin,
		"EventCollabAgentSpawnEnd":         agentcore.EventCollabAgentSpawnEnd,
		"EventCollabAgentInteractionBegin": agentcore.EventCollabAgentInteractionBegin,
		"EventCollabAgentInteractionEnd":   agentcore.EventCollabAgentInteractionEnd,
		"EventCollabWaitingBegin":          agentcore.EventCollabWaitingBegin,
		"EventCollabWaitingEnd":            agentcore.EventCollabWaitingEnd,
		"EventDynamicToolCall":             agentcore.EventDynamicToolCall,
		"EventMCPStartupComplete":          agentcore.EventMCPStartupComplete,
		"EventAgentMessageCompleted":       agentcore.EventAgentMessageCompleted,
		"EventTokenCount":                  agentcore.EventTokenCount,
		"EventContextCompacted":            agentcore.EventContextCompacted,
		"EventThreadNameUpdated":           agentcore.EventThreadNameUpdated,
		"EventThreadRolledBack":            agentcore.EventThreadRolledBack,
		"EventWarning":                     agentcore.EventWarning,
		"EventStreamError":                 agentcore.EventStreamError,
		"EventBackgroundEvent":             agentcore.EventBackgroundEvent,
		"EventPlanDelta":                   agentcore.EventPlanDelta,
		"EventPlanUpdate":                  agentcore.EventPlanUpdate,
	}

	for name, value := range cases {
		if value == "" {
			t.Fatalf("%s should not be empty", name)
		}
	}
}

func TestDynamicToolCallDataWireTags(t *testing.T) {
	t.Parallel()

	payload := agentcore.DynamicToolCallData{
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		CallID:    "call-1",
		Tool:      "lsp_hover",
		Arguments: json.RawMessage(`{"line":1}`),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal dynamic tool call data: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal dynamic tool call data: %v", err)
	}

	for _, key := range []string{"threadId", "turnId", "callId", "tool", "arguments"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("missing wire key: %s", key)
		}
	}
}

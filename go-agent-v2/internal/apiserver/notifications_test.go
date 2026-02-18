package apiserver

import "testing"

func TestMapEventToMethod_ProtocolAlignedMappings(t *testing.T) {
	tests := []struct {
		eventType string
		want      string
	}{
		{"session_configured", "thread/started"},
		{"turn_started", "turn/started"},
		{"turn_complete", "turn/completed"},
		{"token_count", "thread/tokenUsage/updated"},
		{"agent_message_delta", "item/agentMessage/delta"},
		{"agent_reasoning_delta", "item/reasoning/summaryTextDelta"},
		{"agent_reasoning_raw_delta", "item/reasoning/textDelta"},
		{"agent_reasoning_section_break", "item/reasoning/summaryPartAdded"},
		{"exec_command_begin", "item/started"},
		{"exec_command_output_delta", "item/commandExecution/outputDelta"},
		{"exec_command_end", "item/completed"},
		{"exec_approval_request", "item/commandExecution/requestApproval"},
		{"file_change_approval_request", "item/fileChange/requestApproval"},
		{"patch_apply", "item/fileChange/outputDelta"},
		{"patch_apply_begin", "item/started"},
		{"patch_apply_end", "item/completed"},
		{"dynamic_tool_call", "item/tool/call"},
		{"plan_delta", "item/plan/delta"},
		{"account_updated", "account/updated"},
		{"rate_limits_updated", "account/rateLimits/updated"},
		{"fuzzy_search_updated", "fuzzyFileSearch/sessionUpdated"},
		{"warning", "configWarning"},
		{"deprecation_notice", "deprecationNotice"},
		{"shutdown_complete", "codex/event/shutdown_complete"},
		{"list_skills_response", "codex/event/list_skills_response"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := mapEventToMethod(tt.eventType)
			if got != tt.want {
				t.Fatalf("mapEventToMethod(%q) = %q, want %q", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestMapEventToMethod_PassthroughFamilies(t *testing.T) {
	tests := []string{
		"thread/started",
		"turn/completed",
		"item/agentMessage/delta",
		"item/started",
		"item/completed",
		"account/rateLimits/updated",
		"app/list/updated",
		"mcpServer/oauthLogin/completed",
		"fuzzyFileSearch/sessionUpdated",
		"codex/event/task_complete",
		"agent/event/custom_event",
	}

	for _, eventType := range tests {
		t.Run(eventType, func(t *testing.T) {
			got := mapEventToMethod(eventType)
			if got != eventType {
				t.Fatalf("mapEventToMethod(%q) = %q, want passthrough", eventType, got)
			}
		})
	}
}

func TestMapEventToMethod_NoLegacySyntheticMethods(t *testing.T) {
	badMethods := map[string]struct{}{
		"item/fileChange/started":   {},
		"item/fileChange/completed": {},
		"item/dynamicToolCall":      {},
		"skills/list/response":      {},
		"thread/shutdown":           {},
		"item/agentMessage/started": {},
		"item/userMessage":          {},
		"mcpServer/startupUpdate":   {},
		"mcpServer/startupComplete": {},
	}

	tests := []string{
		"patch_apply_begin",
		"patch_apply_end",
		"dynamic_tool_call",
		"list_skills_response",
		"shutdown_complete",
		"codex/event/agent_message",
		"codex/event/user_message",
		"codex/event/mcp_startup_update",
		"codex/event/mcp_startup_complete",
	}

	for _, eventType := range tests {
		got := mapEventToMethod(eventType)
		if _, exists := badMethods[got]; exists {
			t.Fatalf("mapEventToMethod(%q) returned deprecated synthetic method %q", eventType, got)
		}
	}
}

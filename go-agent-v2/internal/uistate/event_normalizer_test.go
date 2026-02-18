package uistate

import (
	"encoding/json"
	"testing"
)

func TestNormalizeEvent_AssistantDelta(t *testing.T) {
	raw := json.RawMessage(`{"delta":"hello"}`)
	result := NormalizeEvent("agent_message_delta", "item/agentMessage/delta", raw)

	if result.UIType != UITypeAssistantDelta {
		t.Errorf("want UIType=%q, got %q", UITypeAssistantDelta, result.UIType)
	}
	if result.UIStatus != UIStatusThinking {
		t.Errorf("want UIStatus=%q, got %q", UIStatusThinking, result.UIStatus)
	}
	if result.Text != "hello" {
		t.Errorf("want Text=%q, got %q", "hello", result.Text)
	}
}

func TestNormalizeEvent_TurnComplete(t *testing.T) {
	raw := json.RawMessage(`{}`)
	result := NormalizeEvent("turn_complete", "turn/completed", raw)

	if result.UIType != UITypeTurnComplete {
		t.Errorf("want UIType=%q, got %q", UITypeTurnComplete, result.UIType)
	}
	if result.UIStatus != UIStatusIdle {
		t.Errorf("want UIStatus=%q, got %q", UIStatusIdle, result.UIStatus)
	}
}

func TestNormalizeEvent_TurnStarted(t *testing.T) {
	raw := json.RawMessage(`{}`)
	result := NormalizeEvent("turn_started", "turn/started", raw)

	if result.UIType != UITypeTurnStarted {
		t.Errorf("want UIType=%q, got %q", UITypeTurnStarted, result.UIType)
	}
	if result.UIStatus != UIStatusThinking {
		t.Errorf("want UIStatus=%q, got %q", UIStatusThinking, result.UIStatus)
	}
}

func TestNormalizeEvent_CommandStart(t *testing.T) {
	raw := json.RawMessage(`{"command":"ls -la","name":"shell"}`)
	result := NormalizeEvent("exec_command_begin", "item/started", raw)

	if result.UIType != UITypeCommandStart {
		t.Errorf("want UIType=%q, got %q", UITypeCommandStart, result.UIType)
	}
	if result.Command != "ls -la" {
		t.Errorf("want Command=%q, got %q", "ls -la", result.Command)
	}
}

func TestNormalizeEvent_FileEditStart(t *testing.T) {
	raw := json.RawMessage(`{"file":"main.go"}`)
	result := NormalizeEvent("patch_apply_begin", "item/fileChange/started", raw)

	if result.UIType != UITypeFileEditStart {
		t.Errorf("want UIType=%q, got %q", UITypeFileEditStart, result.UIType)
	}
	if len(result.Files) == 0 || result.Files[0] != "main.go" {
		t.Errorf("want Files=%v, got %v", []string{"main.go"}, result.Files)
	}
}

func TestNormalizeEvent_ApprovalRequest(t *testing.T) {
	raw := json.RawMessage(`{"command":"rm -rf /"}`)
	result := NormalizeEvent("exec_approval_request", "item/commandExecution/requestApproval", raw)

	if result.UIType != UITypeApprovalRequest {
		t.Errorf("want UIType=%q, got %q", UITypeApprovalRequest, result.UIType)
	}
}

func TestNormalizeEvent_ShutdownComplete(t *testing.T) {
	result := NormalizeEvent("shutdown_complete", "", json.RawMessage(`{}`))
	if result.UIType != UITypeSystem {
		t.Errorf("want UIType=%q, got %q", UITypeSystem, result.UIType)
	}
	if result.UIStatus != UIStatusIdle {
		t.Errorf("want UIStatus=%q, got %q", UIStatusIdle, result.UIStatus)
	}
}

func TestNormalizeEvent_ExitCodeExtracted(t *testing.T) {
	raw := json.RawMessage(`{"exit_code":1}`)
	result := NormalizeEvent("exec_command_end", "item/completed", raw)

	if result.UIType != UITypeCommandDone {
		t.Errorf("want UIType=%q, got %q", UITypeCommandDone, result.UIType)
	}
	if result.ExitCode == nil || *result.ExitCode != 1 {
		t.Errorf("want ExitCode=1, got %v", result.ExitCode)
	}
}

func TestNormalizeEvent_NilData(t *testing.T) {
	// nil data should not panic
	result := NormalizeEvent("turn_complete", "", nil)
	if result.UIType != UITypeTurnComplete {
		t.Errorf("want UIType=%q, got %q", UITypeTurnComplete, result.UIType)
	}
}

func TestNormalizeEvent_TableDriven(t *testing.T) {
	tests := []struct {
		codexType string
		method    string
		wantUI    UIType
		wantSt    UIStatus
	}{
		{"agent_message_delta", "", UITypeAssistantDelta, UIStatusThinking},
		{"agent_message_content_delta", "", UITypeAssistantDelta, UIStatusThinking},
		{"agent_message_completed", "", UITypeAssistantDone, UIStatusThinking},
		{"agent_message", "", UITypeAssistantDone, UIStatusThinking},
		{"agent_reasoning_delta", "", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning", "", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning_raw", "", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning_raw_delta", "", UITypeReasoningDelta, UIStatusThinking},
		{"exec_command_begin", "", UITypeCommandStart, UIStatusRunning},
		{"exec_output_delta", "", UITypeCommandOutput, UIStatusRunning},
		{"exec_command_output_delta", "", UITypeCommandOutput, UIStatusRunning},
		{"exec_command_end", "", UITypeCommandDone, UIStatusRunning},
		{"turn_started", "", UITypeTurnStarted, UIStatusThinking},
		{"turn_complete", "", UITypeTurnComplete, UIStatusIdle},
		{"idle", "", UITypeTurnComplete, UIStatusIdle},
		{"patch_apply_begin", "", UITypeFileEditStart, UIStatusRunning},
		{"patch_apply_end", "", UITypeFileEditDone, UIStatusRunning},
		{"mcp_tool_call_begin", "", UITypeToolCall, UIStatusRunning},
		{"mcp_tool_call_end", "", UITypeCommandDone, UIStatusRunning},
		{"exec_approval_request", "", UITypeApprovalRequest, UIStatusRunning},
		{"plan_delta", "", UITypePlanDelta, UIStatusThinking},
		{"turn_diff", "", UITypeDiffUpdate, UIStatusIdle},
		{"error", "", UITypeError, UIStatusError},
		{"stream_error", "", UITypeError, UIStatusError},
		{"shutdown_complete", "", UITypeSystem, UIStatusIdle},
		{"dynamic_tool_call", "", UITypeToolCall, UIStatusRunning},
		{"session_configured", "", UITypeSystem, ""},
		{"warning", "", UITypeSystem, ""},
		{"some_unknown_thing", "", UITypeSystem, ""},
	}
	for _, tt := range tests {
		t.Run(tt.codexType, func(t *testing.T) {
			result := NormalizeEvent(tt.codexType, tt.method, json.RawMessage(`{}`))
			if result.UIType != tt.wantUI {
				t.Errorf("UIType: want %q, got %q", tt.wantUI, result.UIType)
			}
			if result.UIStatus != tt.wantSt {
				t.Errorf("UIStatus: want %q, got %q", tt.wantSt, result.UIStatus)
			}
		})
	}
}

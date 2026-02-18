package uistate

import (
	"encoding/json"
	"testing"
)

// ─── classifyEvent (map-lookup refactor) ───

func TestClassifyEvent_AllKnownTypes(t *testing.T) {
	cases := []struct {
		codexType string
		wantType  UIType
		wantStat  UIStatus
	}{
		// Assistant
		{"agent_message_delta", UITypeAssistantDelta, UIStatusThinking},
		{"agent_message_content_delta", UITypeAssistantDelta, UIStatusThinking},
		{"agent_message_completed", UITypeAssistantDone, UIStatusThinking},
		{"agent_message", UITypeAssistantDone, UIStatusThinking},
		// Reasoning
		{"agent_reasoning", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning_delta", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning_raw", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning_raw_delta", UITypeReasoningDelta, UIStatusThinking},
		{"agent_reasoning_section_break", UITypeReasoningDelta, UIStatusThinking},
		// Command Execution
		{"exec_command_begin", UITypeCommandStart, UIStatusRunning},
		{"exec_output_delta", UITypeCommandOutput, UIStatusRunning},
		{"exec_command_output_delta", UITypeCommandOutput, UIStatusRunning},
		{"exec_command_end", UITypeCommandDone, UIStatusRunning},
		// File Editing
		{"patch_apply_begin", UITypeFileEditStart, UIStatusRunning},
		{"file_read", UITypeFileEditStart, UIStatusRunning},
		{"patch_apply", UITypeCommandOutput, UIStatusRunning},
		{"patch_apply_delta", UITypeCommandOutput, UIStatusRunning},
		{"patch_apply_end", UITypeFileEditDone, UIStatusRunning},
		{"file_updated", UITypeFileEditDone, UIStatusRunning},
		// Tool Calls
		{"mcp_tool_call_begin", UITypeToolCall, UIStatusRunning},
		{"mcp_tool_call", UITypeToolCall, UIStatusRunning},
		{"dynamic_tool_call", UITypeToolCall, UIStatusRunning},
		{"mcp_tool_call_end", UITypeCommandDone, UIStatusRunning},
		// Approval
		{"exec_approval_request", UITypeApprovalRequest, UIStatusRunning},
		{"file_change_approval_request", UITypeApprovalRequest, UIStatusRunning},
		// Turn Lifecycle
		{"turn_started", UITypeTurnStarted, UIStatusThinking},
		{"turn_complete", UITypeTurnComplete, UIStatusIdle},
		{"idle", UITypeTurnComplete, UIStatusIdle},
		// Plan / Diff
		{"plan_delta", UITypePlanDelta, UIStatusThinking},
		{"plan_update", UITypePlanDelta, UIStatusThinking},
		{"turn_diff", UITypeDiffUpdate, UIStatusIdle},
		// User Message
		{"user_message", UITypeUserMessage, UIStatusThinking},
		// Errors
		{"error", UITypeError, UIStatusError},
		{"stream_error", UITypeError, UIStatusError},
		// Warnings
		{"warning", UITypeSystem, ""},
		// System / Lifecycle
		{"shutdown_complete", UITypeSystem, UIStatusIdle},
		{"session_configured", UITypeSystem, ""},
		{"mcp_startup_complete", UITypeSystem, ""},
		{"mcp_list_tools_response", UITypeSystem, ""},
		{"list_skills_response", UITypeSystem, ""},
		{"token_count", UITypeSystem, ""},
		{"context_compacted", UITypeSystem, ""},
		{"thread_name_updated", UITypeSystem, ""},
		{"thread_rolled_back", UITypeSystem, ""},
		{"undo_started", UITypeSystem, ""},
		{"undo_completed", UITypeSystem, ""},
		{"entered_review_mode", UITypeSystem, ""},
		{"exited_review_mode", UITypeSystem, ""},
		{"background_event", UITypeSystem, ""},
		// Collab Agents
		{"collab_agent_spawn_begin", UITypeSystem, UIStatusRunning},
		{"collab_agent_interaction_begin", UITypeSystem, UIStatusRunning},
		{"collab_waiting_begin", UITypeSystem, UIStatusRunning},
		{"collab_agent_spawn_end", UITypeSystem, UIStatusRunning},
		{"collab_agent_interaction_end", UITypeSystem, UIStatusRunning},
		{"collab_waiting_end", UITypeSystem, UIStatusRunning},
	}

	for _, tc := range cases {
		gotType, gotStat := classifyEvent(tc.codexType)
		if gotType != tc.wantType || gotStat != tc.wantStat {
			t.Errorf("classifyEvent(%q) = (%q, %q), want (%q, %q)",
				tc.codexType, gotType, gotStat, tc.wantType, tc.wantStat)
		}
	}
}

func TestClassifyEvent_UnknownFallback(t *testing.T) {
	gotType, gotStat := classifyEvent("some_unknown_event_xyz")
	if gotType != UITypeSystem || gotStat != "" {
		t.Errorf("classifyEvent(unknown) = (%q, %q), want (%q, %q)",
			gotType, gotStat, UITypeSystem, "")
	}
}

// ─── extractText (helper for NormalizeEvent) ───

func TestExtractText(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"delta wins", map[string]any{"delta": "d", "text": "t"}, "d"},
		{"text fallback", map[string]any{"text": "t", "content": "c"}, "t"},
		{"content fallback", map[string]any{"content": "c", "output": "o"}, "c"},
		{"output fallback", map[string]any{"output": "o", "message": "m"}, "o"},
		{"message fallback", map[string]any{"message": "m"}, "m"},
		{"empty", map[string]any{}, ""},
		{"non-string ignored", map[string]any{"delta": 123}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractText(tc.payload)
			if got != tc.want {
				t.Errorf("extractText() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ─── extractNormalizedFiles (helper for NormalizeEvent) ───

func TestExtractNormalizedFiles(t *testing.T) {
	cases := []struct {
		name      string
		codexType string
		payload   map[string]any
		wantFile  string
		wantFiles []string
	}{
		{
			"patch_apply_begin with file",
			"patch_apply_begin",
			map[string]any{"file": "a.go"},
			"a.go", []string{"a.go"},
		},
		{
			"generic file field",
			"other_event",
			map[string]any{"file": "b.go"},
			"b.go", []string{"b.go"},
		},
		{
			"generic files array",
			"other_event",
			map[string]any{"files": []any{"c.go", "d.go"}},
			"c.go", []string{"c.go", "d.go"},
		},
		{
			"empty payload",
			"other_event",
			map[string]any{},
			"", nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotFile, gotFiles := extractNormalizedFiles(tc.codexType, tc.payload)
			if gotFile != tc.wantFile {
				t.Errorf("file = %q, want %q", gotFile, tc.wantFile)
			}
			if len(gotFiles) != len(tc.wantFiles) {
				t.Errorf("files = %v, want %v", gotFiles, tc.wantFiles)
			}
		})
	}
}

// ─── extractExitCodeFromPayload (helper for NormalizeEvent) ───

func TestExtractExitCodeFromPayload(t *testing.T) {
	cases := []struct {
		name      string
		codexType string
		payload   map[string]any
		wantNil   bool
		wantCode  int
	}{
		{"exec_command_end with exit_code", "exec_command_end", map[string]any{"exit_code": float64(0)}, false, 0},
		{"exec_command_end nonzero", "exec_command_end", map[string]any{"exit_code": float64(1)}, false, 1},
		{"non exec_command_end ignored", "other", map[string]any{"exit_code": float64(0)}, true, 0},
		{"exec_command_end missing exit_code", "exec_command_end", map[string]any{}, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractExitCodeFromPayload(tc.codexType, tc.payload)
			if tc.wantNil && got != nil {
				t.Errorf("want nil, got %d", *got)
			}
			if !tc.wantNil {
				if got == nil {
					t.Fatal("want non-nil, got nil")
				}
				if *got != tc.wantCode {
					t.Errorf("exit code = %d, want %d", *got, tc.wantCode)
				}
			}
		})
	}
}

// ─── NormalizeEvent integration (ensures refactored version matches original) ───

func TestNormalizeEvent_Integration(t *testing.T) {
	// Test the full NormalizeEvent with a real JSON payload
	data, _ := json.Marshal(map[string]any{
		"delta":   "hello world",
		"command": "ls -la",
		"file":    "main.go",
	})

	ev := NormalizeEvent("exec_command_begin", "", data)
	if ev.UIType != UITypeCommandStart {
		t.Errorf("UIType = %q, want %q", ev.UIType, UITypeCommandStart)
	}
	if ev.UIStatus != UIStatusRunning {
		t.Errorf("UIStatus = %q, want %q", ev.UIStatus, UIStatusRunning)
	}
	if ev.Text != "hello world" {
		t.Errorf("Text = %q, want %q", ev.Text, "hello world")
	}
	if ev.Command != "ls -la" {
		t.Errorf("Command = %q, want %q", ev.Command, "ls -la")
	}
	if ev.File != "main.go" {
		t.Errorf("File = %q, want %q", ev.File, "main.go")
	}
}

func TestNormalizeEvent_ExitCode(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"exit_code": float64(42)})
	ev := NormalizeEvent("exec_command_end", "", data)
	if ev.ExitCode == nil {
		t.Fatal("ExitCode is nil")
	}
	if *ev.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", *ev.ExitCode)
	}
}

func TestNormalizeEvent_EmptyData(t *testing.T) {
	ev := NormalizeEvent("turn_started", "", nil)
	if ev.UIType != UITypeTurnStarted {
		t.Errorf("UIType = %q, want %q", ev.UIType, UITypeTurnStarted)
	}
}

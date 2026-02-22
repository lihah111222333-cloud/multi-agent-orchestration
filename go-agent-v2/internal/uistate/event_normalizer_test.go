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
	}{
		// Assistant
		{"agent_message_delta", UITypeAssistantDelta},
		{"agent_message_content_delta", UITypeAssistantDelta},
		{"agent_message_completed", UITypeAssistantDone},
		{"agent_message", UITypeAssistantDone},
		// Reasoning
		{"agent_reasoning", UITypeReasoningDelta},
		{"agent_reasoning_delta", UITypeReasoningDelta},
		{"agent_reasoning_raw", UITypeReasoningDelta},
		{"agent_reasoning_raw_delta", UITypeReasoningDelta},
		{"agent_reasoning_section_break", UITypeReasoningDelta},
		// Command Execution
		{"exec_command_begin", UITypeCommandStart},
		{"exec_output_delta", UITypeCommandOutput},
		{"exec_command_output_delta", UITypeCommandOutput},
		{"exec_command_end", UITypeCommandDone},
		{"exec_terminal_interaction", UITypeSystem},
		// File Editing
		{"patch_apply_begin", UITypeFileEditStart},
		{"file_read", UITypeFileEditStart},
		{"patch_apply", UITypeCommandOutput},
		{"patch_apply_delta", UITypeCommandOutput},
		{"patch_apply_end", UITypeFileEditDone},
		{"file_updated", UITypeFileEditDone},
		// Tool Calls
		{"mcp_tool_call_begin", UITypeToolCall},
		{"mcp_tool_call", UITypeToolCall},
		{"dynamic_tool_call", UITypeSystem},
		{"mcp_tool_call_end", UITypeToolCall},
		// Approval
		{"exec_approval_request", UITypeApprovalRequest},
		{"file_change_approval_request", UITypeApprovalRequest},
		// Turn Lifecycle
		{"turn_started", UITypeTurnStarted},
		{"task_started", UITypeTurnStarted},
		{"codex/event/task_started", UITypeTurnStarted},
		{"agent/event/task_started", UITypeTurnStarted},
		{"turn_complete", UITypeTurnComplete},
		{"task_complete", UITypeTurnComplete},
		{"codex/event/task_complete", UITypeTurnComplete},
		{"agent/event/task_complete", UITypeTurnComplete},
		{"turn/completed", UITypeTurnComplete},
		{"idle", UITypeTurnComplete},
		// Plan / Diff
		{"plan_delta", UITypePlanDelta},
		{"plan_update", UITypePlanDelta},
		{"turn_plan", UITypePlanDelta},
		{"item/plan/delta", UITypePlanDelta},
		{"codex/event/plan_delta", UITypePlanDelta},
		{"turn_diff", UITypeDiffUpdate},
		// User Message
		{"user_message", UITypeUserMessage},
		// Errors
		{"error", UITypeError},
		{"stream_error", UITypeError},
		// Warnings
		{"warning", UITypeSystem},
		// System / Lifecycle
		{"shutdown_complete", UITypeSystem},
		{"session_configured", UITypeSystem},
		{"mcp_startup_update", UITypeSystem},
		{"mcp_startup_complete", UITypeSystem},
		{"mcp_list_tools_response", UITypeSystem},
		{"list_skills_response", UITypeSystem},
		{"token_count", UITypeSystem},
		{"context_compacted", UITypeSystem},
		{"thread_name_updated", UITypeSystem},
		{"thread_rolled_back", UITypeSystem},
		{"undo_started", UITypeSystem},
		{"undo_completed", UITypeSystem},
		{"entered_review_mode", UITypeSystem},
		{"exited_review_mode", UITypeSystem},
		{"background_event", UITypeSystem},
		// Collab Agents
		{"collab_agent_spawn_begin", UITypeSystem},
		{"collab_agent_interaction_begin", UITypeSystem},
		{"collab_waiting_begin", UITypeSystem},
		{"collab_agent_spawn_end", UITypeSystem},
		{"collab_agent_interaction_end", UITypeSystem},
		{"collab_waiting_end", UITypeSystem},
	}

	for _, tc := range cases {
		gotType := classifyEvent(tc.codexType)
		if gotType != tc.wantType {
			t.Errorf("classifyEvent(%q) = %q, want %q",
				tc.codexType, gotType, tc.wantType)
		}
	}
}

func TestClassifyEventWithMethod_Fallback(t *testing.T) {
	gotType := classifyEventWithMethod("unmapped_event", "turn/started")
	if gotType != UITypeTurnStarted {
		t.Fatalf("classifyEventWithMethod turn started = %q, want %q", gotType, UITypeTurnStarted)
	}

	gotType = classifyEventWithMethod("unmapped_event", "turn/completed")
	if gotType != UITypeTurnComplete {
		t.Fatalf("classifyEventWithMethod turn completed = %q, want %q", gotType, UITypeTurnComplete)
	}

	gotType = classifyEventWithMethod("unmapped_event", "codex/event/task_complete")
	if gotType != UITypeTurnComplete {
		t.Fatalf("classifyEventWithMethod task complete = %q, want %q", gotType, UITypeTurnComplete)
	}

	gotType = classifyEventWithMethod("unmapped_event", "item/commandExecution/terminalInteraction")
	if gotType != UITypeSystem {
		t.Fatalf("classifyEventWithMethod terminal interaction = %q, want %q", gotType, UITypeSystem)
	}

	gotType = classifyEventWithMethod("unmapped_event", "codex/event/mcp_startup_update")
	if gotType != UITypeSystem {
		t.Fatalf("classifyEventWithMethod mcp startup = %q, want %q", gotType, UITypeSystem)
	}

	gotType = classifyEventWithMethod("unmapped_event", "codex/event/background_event")
	if gotType != UITypeSystem {
		t.Fatalf("classifyEventWithMethod background event = %q, want %q", gotType, UITypeSystem)
	}

	gotType = classifyEventWithMethod("unmapped_event", "turn/plan/updated")
	if gotType != UITypePlanDelta {
		t.Fatalf("classifyEventWithMethod turn plan updated = %q, want %q", gotType, UITypePlanDelta)
	}

	gotType = classifyEventWithMethod("unmapped_event", "item/plan/delta")
	if gotType != UITypePlanDelta {
		t.Fatalf("classifyEventWithMethod item plan delta = %q, want %q", gotType, UITypePlanDelta)
	}
}

func TestNormalizeEventFromPayload_ItemLifecycleCommand(t *testing.T) {
	start := NormalizeEventFromPayload("item/started", "item/started", map[string]any{
		"type":    "commandExecution",
		"command": "go test ./...",
	})
	if start.UIType != UITypeCommandStart {
		t.Fatalf("item/started command uiType = %q, want %q", start.UIType, UITypeCommandStart)
	}

	end := NormalizeEventFromPayload("item/completed", "item/completed", map[string]any{
		"type":      "commandExecution",
		"exit_code": float64(9),
	})
	if end.UIType != UITypeCommandDone {
		t.Fatalf("item/completed command uiType = %q, want %q", end.UIType, UITypeCommandDone)
	}
	if end.ExitCode == nil || *end.ExitCode != 9 {
		t.Fatalf("item/completed command exitCode = %v, want 9", end.ExitCode)
	}
}

func TestNormalizeEventFromPayload_ItemLifecycleCommandFromNestedItem(t *testing.T) {
	start := NormalizeEventFromPayload("item/started", "item/started", map[string]any{
		"item": map[string]any{
			"type":            "commandExecution",
			"command_display": "go test ./...",
		},
	})
	if start.UIType != UITypeCommandStart {
		t.Fatalf("item/started nested item uiType = %q, want %q", start.UIType, UITypeCommandStart)
	}
	if start.Command != "go test ./..." {
		t.Fatalf("item/started nested item command = %q, want go test ./...", start.Command)
	}
}

func TestNormalizeEventFromPayload_ItemLifecycleFile(t *testing.T) {
	start := NormalizeEventFromPayload("item/started", "item/started", map[string]any{
		"item": map[string]any{
			"type": "fileChange",
		},
		"file": "README.md",
	})
	if start.UIType != UITypeFileEditStart {
		t.Fatalf("item/started file uiType = %q, want %q", start.UIType, UITypeFileEditStart)
	}

	end := NormalizeEventFromPayload("item/completed", "item/completed", map[string]any{
		"msg": `{"type":"file_change"}`,
	})
	if end.UIType != UITypeFileEditDone {
		t.Fatalf("item/completed file uiType = %q, want %q", end.UIType, UITypeFileEditDone)
	}
}

func TestClassifyEvent_UnknownFallback(t *testing.T) {
	gotType := classifyEvent("some_unknown_event_xyz")
	if gotType != UITypeSystem {
		t.Errorf("classifyEvent(unknown) = %q, want %q",
			gotType, UITypeSystem)
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
		{"item/completed with exit_code", "item/completed", map[string]any{"exit_code": float64(2)}, false, 2},
		{"codex/event/item_completed with exit_code", "codex/event/item_completed", map[string]any{"exit_code": float64(3)}, false, 3},
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

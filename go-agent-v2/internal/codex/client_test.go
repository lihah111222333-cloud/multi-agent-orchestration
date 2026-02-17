package codex

import (
	"encoding/json"
	"testing"
)

func TestEventConstants(t *testing.T) {
	// 确保核心事件常量不为空
	coreEvents := []string{
		EventSessionConfigured,
		EventTurnStarted,
		EventTurnComplete,
		EventIdle,
		EventError,
		EventShutdownComplete,
	}
	for _, e := range coreEvents {
		if e == "" {
			t.Errorf("core event constant is empty")
		}
	}
}

func TestSubmitMessageJSON(t *testing.T) {
	msg := SubmitMessage{
		Type:   "submit",
		Prompt: "分析 main.rs",
		Images: []string{"/tmp/screenshot.png"},
		Files:  []string{"/path/to/file.rs"},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal SubmitMessage: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded["type"] != "submit" {
		t.Errorf("type = %v, want submit", decoded["type"])
	}
	if decoded["prompt"] != "分析 main.rs" {
		t.Errorf("prompt = %v, want 分析 main.rs", decoded["prompt"])
	}
	images, ok := decoded["images"].([]any)
	if !ok || len(images) != 1 {
		t.Errorf("images = %v, want 1 item", decoded["images"])
	}
	files, ok := decoded["files"].([]any)
	if !ok || len(files) != 1 {
		t.Errorf("files = %v, want 1 item", decoded["files"])
	}
}

func TestCommandMessageJSON(t *testing.T) {
	msg := CommandMessage{
		Type:    "command",
		Command: CmdCompact,
		Args:    "",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal CommandMessage: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded["type"] != "command" {
		t.Errorf("type = %v, want command", decoded["type"])
	}
	if decoded["command"] != "/compact" {
		t.Errorf("command = %v, want /compact", decoded["command"])
	}
}

func TestEventParsing(t *testing.T) {
	raw := `{"type":"agent_message_delta","data":{"delta":"hello"}}`
	var event Event
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if event.Type != EventAgentMessageDelta {
		t.Errorf("type = %q, want %q", event.Type, EventAgentMessageDelta)
	}

	var delta TextData
	if err := json.Unmarshal(event.Data, &delta); err != nil {
		t.Fatalf("unmarshal delta data: %v", err)
	}
	if delta.Delta != "hello" {
		t.Errorf("delta = %q, want hello", delta.Delta)
	}
}

func TestAllCommandsDefined(t *testing.T) {
	if len(AllCommands) != 15 {
		t.Errorf("AllCommands has %d entries, want 15", len(AllCommands))
	}
	for _, cmd := range AllCommands {
		if cmd.Cmd == "" || cmd.Label == "" {
			t.Errorf("command with empty Cmd or Label: %+v", cmd)
		}
	}
}

func TestCreateThreadRequestJSON(t *testing.T) {
	req := CreateThreadRequest{
		Prompt:         "hello",
		Model:          "gpt-5.2-codex",
		Cwd:            "/tmp",
		ApprovalPolicy: "never",
		Images:         []string{"/tmp/img.png"},
		Files:          []string{"/tmp/file.txt"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded CreateThreadRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Model != "gpt-5.2-codex" {
		t.Errorf("model = %q, want gpt-5.2-codex", decoded.Model)
	}
	if len(decoded.Images) != 1 {
		t.Errorf("images len = %d, want 1", len(decoded.Images))
	}
	if len(decoded.Files) != 1 {
		t.Errorf("files len = %d, want 1", len(decoded.Files))
	}
}

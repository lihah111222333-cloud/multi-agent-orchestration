package codex

import (
	"encoding/json"
	"testing"
)

func TestHandleRPCResponse_Result(t *testing.T) {
	client := NewAppServerClient(0)
	id := int64(7)
	call := &pendingCall{done: make(chan struct{})}
	client.pending.Store(id, call)

	msg := jsonRPCMessage{
		ID:     &id,
		Result: json.RawMessage(`{"ok":true}`),
	}
	if !client.handleRPCResponse(msg) {
		t.Fatal("expected RPC response to be handled")
	}
	<-call.done
	if len(call.result) == 0 {
		t.Fatal("expected result payload")
	}
}

func TestHandleRPCResponse_Error(t *testing.T) {
	client := NewAppServerClient(0)
	id := int64(9)
	call := &pendingCall{done: make(chan struct{})}
	client.pending.Store(id, call)

	msg := jsonRPCMessage{
		ID:    &id,
		Error: &jsonRPCError{Code: -32000, Message: "boom"},
	}
	if !client.handleRPCResponse(msg) {
		t.Fatal("expected RPC error response to be handled")
	}
	<-call.done
	if call.err == nil {
		t.Fatal("expected error on pending call")
	}
}

func TestBuildTurnStartInputs_WithImagesAndFiles(t *testing.T) {
	inputs := buildTurnStartInputs(
		"看下这个截图",
		[]string{
			"/tmp/screen.png",
			"https://example.com/a.png",
			"data:image/png;base64,AAA",
		},
		[]string{
			"/tmp/spec.md",
			"docs/readme.txt",
		},
	)

	if len(inputs) != 6 {
		t.Fatalf("len(inputs) = %d, want 6", len(inputs))
	}
	if inputs[0].Type != "text" || inputs[0].Text != "看下这个截图" {
		t.Fatalf("inputs[0] = %+v", inputs[0])
	}
	if inputs[1].Type != "localImage" || inputs[1].Path != "/tmp/screen.png" {
		t.Fatalf("inputs[1] = %+v", inputs[1])
	}
	if inputs[2].Type != "image" || inputs[2].URL != "https://example.com/a.png" {
		t.Fatalf("inputs[2] = %+v", inputs[2])
	}
	if inputs[3].Type != "image" || inputs[3].URL != "data:image/png;base64,AAA" {
		t.Fatalf("inputs[3] = %+v", inputs[3])
	}
	if inputs[4].Type != "mention" || inputs[4].Name != "spec.md" || inputs[4].Path != "/tmp/spec.md" {
		t.Fatalf("inputs[4] = %+v", inputs[4])
	}
	if inputs[5].Type != "mention" || inputs[5].Name != "readme.txt" || inputs[5].Path != "docs/readme.txt" {
		t.Fatalf("inputs[5] = %+v", inputs[5])
	}
}

func TestBuildTurnStartInputs_AttachmentsOnly_NoEmptyTextItem(t *testing.T) {
	inputs := buildTurnStartInputs("", []string{"/tmp/screen.png"}, nil)
	if len(inputs) != 1 {
		t.Fatalf("len(inputs) = %d, want 1", len(inputs))
	}
	if inputs[0].Type != "localImage" || inputs[0].Path != "/tmp/screen.png" {
		t.Fatalf("inputs[0] = %+v", inputs[0])
	}
}

func TestBuildTurnStartInputs_EmptyFallbackText(t *testing.T) {
	inputs := buildTurnStartInputs("", nil, nil)
	if len(inputs) != 1 {
		t.Fatalf("len(inputs) = %d, want 1", len(inputs))
	}
	if inputs[0].Type != "text" {
		t.Fatalf("inputs[0] = %+v", inputs[0])
	}
}

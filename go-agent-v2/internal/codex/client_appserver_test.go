package codex

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func TestHandleRPCResponse_Result(t *testing.T) {
	client := NewAppServerClient(0, "")
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
	client := NewAppServerClient(0, "")
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

func TestExtractTurnIDFromEventData(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "top-level turnId",
			raw:  `{"threadId":"thr-1","turnId":"turn-100"}`,
			want: "turn-100",
		},
		{
			name: "nested turn id",
			raw:  `{"threadId":"thr-2","turn":{"id":"turn-200","status":"inProgress"}}`,
			want: "turn-200",
		},
		{
			name: "legacy envelope",
			raw:  `{"msg":{"data":{"turn":{"id":"turn-300"}}}}`,
			want: "turn-300",
		},
		{
			name: "missing turn id",
			raw:  `{"threadId":"thr-4"}`,
			want: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := extractTurnIDFromEventData(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Fatalf("extractTurnIDFromEventData() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrackTurnLifecycle_WithTurnStartedAndCompleted(t *testing.T) {
	client := NewAppServerClient(0, "")
	client.trackTurnLifecycle(Event{
		Type: EventTurnStarted,
		Data: json.RawMessage(`{"threadId":"thr-1","turn":{"id":"turn-1","status":"inProgress"}}`),
	}, "turn/started")
	if got := client.getActiveTurnID(); got != "turn-1" {
		t.Fatalf("active turn id = %q, want %q", got, "turn-1")
	}

	client.trackTurnLifecycle(Event{
		Type: EventTurnComplete,
		Data: json.RawMessage(`{"threadId":"thr-1","turn":{"id":"turn-1","status":"interrupted"}}`),
	}, "turn/completed")
	if got := client.getActiveTurnID(); got != "" {
		t.Fatalf("active turn id = %q, want empty", got)
	}
}

func TestTrackTurnLifecycle_WithTurnAbortedClearsActiveTurn(t *testing.T) {
	client := NewAppServerClient(0, "")
	client.trackTurnLifecycle(Event{
		Type: EventTurnStarted,
		Data: json.RawMessage(`{"threadId":"thr-2","turn":{"id":"turn-2","status":"inProgress"}}`),
	}, "turn/started")
	if got := client.getActiveTurnID(); got != "turn-2" {
		t.Fatalf("active turn id = %q, want %q", got, "turn-2")
	}

	client.trackTurnLifecycle(Event{
		Type: "turn_aborted",
		Data: json.RawMessage(`{"threadId":"thr-2","turn":{"id":"turn-2","status":"aborted"}}`),
	}, "turn/aborted")
	if got := client.getActiveTurnID(); got != "" {
		t.Fatalf("active turn id = %q, want empty", got)
	}
}

func TestMapMethodToEventType_TurnAborted(t *testing.T) {
	eventType, ok := mapMethodToEventType("agent/event/turn_aborted")
	if !ok {
		t.Fatal("expected agent/event/turn_aborted to be mapped")
	}
	if eventType != "turn_aborted" {
		t.Fatalf("eventType = %q, want turn_aborted", eventType)
	}
}

func TestIsInvalidParamsRPCError(t *testing.T) {
	if !isInvalidParamsRPCError(assertErr("rpc error: invalid params (code -32602)")) {
		t.Fatalf("expected invalid params error to match")
	}
	if isInvalidParamsRPCError(assertErr("rpc error: method not found (code -32601)")) {
		t.Fatalf("did not expect method-not-found to match invalid params")
	}
}

func TestIsInterruptTurnIDMismatchError(t *testing.T) {
	if !isInterruptTurnIDMismatchError(assertErr("rpc error: turn not found")) {
		t.Fatalf("expected turn-not-found to match turn-id mismatch")
	}
	if !isInterruptTurnIDMismatchError(assertErr("rpc error: turn_id mismatch")) {
		t.Fatalf("expected turn_id mismatch to match turn-id mismatch")
	}
	if isInterruptTurnIDMismatchError(assertErr("rpc error: permission denied")) {
		t.Fatalf("did not expect unrelated error to match turn-id mismatch")
	}
}

func assertErr(msg string) error { return &testErr{msg: msg} }

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestFailPendingCalls_ResolvesWaitingCalls(t *testing.T) {
	client := NewAppServerClient(0, "")
	id := int64(11)
	call := &pendingCall{done: make(chan struct{})}
	client.pending.Store(id, call)

	marker := errors.New("transport broken")
	client.failPendingCalls(marker)

	<-call.done
	if !errors.Is(call.err, marker) {
		t.Fatalf("call.err = %v, want marker error", call.err)
	}
}

func TestAsWriteJSON_NoConnectionFailsPending(t *testing.T) {
	client := NewAppServerClient(0, "")
	id := int64(17)
	call := &pendingCall{done: make(chan struct{})}
	client.pending.Store(id, call)

	err := client.asWriteJSON(map[string]any{"hello": "world"})
	if err == nil {
		t.Fatal("expected write error when ws is disconnected")
	}
	if !strings.Contains(err.Error(), "ws not connected") {
		t.Fatalf("err = %v, want contains ws not connected", err)
	}

	<-call.done
	if call.err == nil {
		t.Fatal("expected pending call resolved with error")
	}
	if !strings.Contains(call.err.Error(), "ws not connected") {
		t.Fatalf("call.err = %v, want contains ws not connected", call.err)
	}
}

func TestIsIdleTimeoutError(t *testing.T) {
	if !isIdleTimeoutError(timeoutErr{}) {
		t.Fatal("expected timeoutErr to be recognized as idle timeout")
	}
	if isIdleTimeoutError(errors.New("permission denied")) {
		t.Fatal("unexpected idle timeout match")
	}
}

func TestEmitStreamErrorPayload(t *testing.T) {
	client := NewAppServerClient(9988, "agent-a")
	var got Event
	client.SetEventHandler(func(event Event) {
		got = event
	})

	client.emitStreamError(errors.New("socket idle timeout"), "read", true)
	if got.Type != EventStreamError {
		t.Fatalf("event type = %q, want %q", got.Type, EventStreamError)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.Data, &payload); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}
	if payload["phase"] != "read" {
		t.Fatalf("phase = %v, want read", payload["phase"])
	}
	if payload["reason"] != "idle_timeout" {
		t.Fatalf("reason = %v, want idle_timeout", payload["reason"])
	}
	if payload["agentId"] != "agent-a" {
		t.Fatalf("agentId = %v, want agent-a", payload["agentId"])
	}
}

func TestRespondError_NoConnection(t *testing.T) {
	client := NewAppServerClient(0, "test-agent")
	err := client.RespondError(42, -32603, "test error message")
	if err == nil {
		t.Fatal("expected error when ws is nil")
	}
	if !strings.Contains(err.Error(), "ws not connected") {
		t.Fatalf("err = %v, want contains ws not connected", err)
	}
}

func TestAppServerReconnectDelay(t *testing.T) {
	if got := appServerReconnectDelay(1); got != 0 {
		t.Fatalf("delay(attempt=1) = %v, want 0", got)
	}
	if got := appServerReconnectDelay(2); got != appServerReconnectBaseDelay {
		t.Fatalf("delay(attempt=2) = %v, want %v", got, appServerReconnectBaseDelay)
	}
	if got := appServerReconnectDelay(3); got != appServerReconnectBaseDelay*2 {
		t.Fatalf("delay(attempt=3) = %v, want %v", got, appServerReconnectBaseDelay*2)
	}
	if got := appServerReconnectDelay(16); got != appServerReconnectMaxDelay {
		t.Fatalf("delay(attempt=16) = %v, want capped %v", got, appServerReconnectMaxDelay)
	}
}

func TestEmitBackgroundEventPayload(t *testing.T) {
	client := NewAppServerClient(9988, "agent-b")
	var got Event
	client.SetEventHandler(func(event Event) {
		got = event
	})

	client.emitBackgroundEvent("Reconnecting...", "reconnecting", true, false, map[string]any{
		"phase":   "reconnect",
		"attempt": 1,
	})
	if got.Type != EventBackgroundEvent {
		t.Fatalf("event type = %q, want %q", got.Type, EventBackgroundEvent)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.Data, &payload); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}
	if payload["message"] != "Reconnecting..." {
		t.Fatalf("message = %v, want Reconnecting...", payload["message"])
	}
	if payload["status"] != "reconnecting" {
		t.Fatalf("status = %v, want reconnecting", payload["status"])
	}
	if payload["active"] != true {
		t.Fatalf("active = %v, want true", payload["active"])
	}
}

func TestReconnectWS_StoppedShortCircuit(t *testing.T) {
	client := NewAppServerClient(0, "agent-c")
	client.stopped.Store(true)
	if ok := client.reconnectWS("read_error", errors.New("boom")); ok {
		t.Fatal("reconnectWS should return false when client already stopped")
	}
}

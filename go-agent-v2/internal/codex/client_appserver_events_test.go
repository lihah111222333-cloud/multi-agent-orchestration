package codex

import (
	"encoding/json"
	"testing"
)

func TestThreadStatusChangedTerminalState(t *testing.T) {
	cases := []struct {
		name   string
		raw    map[string]any
		wantOK bool
		want   string
	}{
		{name: "idle_string", raw: map[string]any{"status": "idle"}, wantOK: true, want: "idle"},
		{name: "system_error_string", raw: map[string]any{"status": "system_error"}, wantOK: true, want: "system_error"},
		{name: "not_loaded_string", raw: map[string]any{"status": "not_loaded"}, wantOK: true, want: "not_loaded"},
		{name: "idle_object", raw: map[string]any{"status": map[string]any{"type": "idle"}}, wantOK: true, want: "idle"},
		{name: "active_string", raw: map[string]any{"status": "active"}, wantOK: false, want: "active"},
		{name: "missing", raw: map[string]any{"foo": "bar"}, wantOK: false, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.raw)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			got, ok := threadStatusChangedTerminalState(data)
			if ok != tc.wantOK {
				t.Fatalf("terminal mismatch: got %v want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("status mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestTrackTurnLifecycleThreadStatusChanged(t *testing.T) {
	t.Run("terminal_clears_active_turn", func(t *testing.T) {
		c := &AppServerClient{AgentID: "agent-1"}
		c.setActiveTurnID("turn-1")
		data, _ := json.Marshal(map[string]any{"status": "idle"})

		c.trackTurnLifecycle(Event{Type: "thread/status/changed", Data: data}, "thread/status/changed")

		if got := c.getActiveTurnID(); got != "" {
			t.Fatalf("expected active turn cleared, got %q", got)
		}
	})

	t.Run("non_terminal_keeps_active_turn", func(t *testing.T) {
		c := &AppServerClient{AgentID: "agent-1"}
		c.setActiveTurnID("turn-1")
		data, _ := json.Marshal(map[string]any{"status": "active"})

		c.trackTurnLifecycle(Event{Type: "thread/status/changed", Data: data}, "thread/status/changed")

		if got := c.getActiveTurnID(); got != "turn-1" {
			t.Fatalf("expected active turn retained, got %q", got)
		}
	})
}

func TestExtractConversationIDFromEventParams(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{name: "conversation_id", payload: map[string]any{"conversationId": "thread-1"}, want: "thread-1"},
		{name: "thread_id_camel", payload: map[string]any{"threadId": "thread-2"}, want: "thread-2"},
		{name: "thread_id_snake", payload: map[string]any{"thread_id": "thread-3"}, want: "thread-3"},
		{name: "nested_thread", payload: map[string]any{"thread": map[string]any{"id": "thread-4"}}, want: "thread-4"},
		{name: "missing", payload: map[string]any{"status": "idle"}, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			if got := extractConversationIDFromEventParams(raw); got != tc.want {
				t.Fatalf("extractConversationIDFromEventParams()=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestHandleRPCEventDropsMismatchedThreadScopedEvent(t *testing.T) {
	c := &AppServerClient{AgentID: "agent-1", ThreadID: "thread-a"}
	c.setActiveTurnID("turn-a")
	handled := 0
	c.SetEventHandler(func(Event) { handled++ })

	data, err := json.Marshal(map[string]any{"threadId": "thread-b", "status": "active"})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	shutdown := c.handleRPCEvent(jsonRPCMessage{Method: "thread/status/changed", Params: data})
	if shutdown {
		t.Fatalf("unexpected shutdown=true")
	}
	if got := c.getActiveTurnID(); got != "turn-a" {
		t.Fatalf("expected active turn unchanged, got %q", got)
	}
	if handled != 0 {
		t.Fatalf("expected handler not called on mismatch, got %d", handled)
	}
}

func TestHandleRPCEventMismatchedTerminalRecoversTurnLifecycle(t *testing.T) {
	c := &AppServerClient{AgentID: "agent-1", ThreadID: "thread-a"}
	c.setActiveTurnID("turn-a")
	handled := 0
	c.SetEventHandler(func(Event) { handled++ })

	data, err := json.Marshal(map[string]any{"threadId": "thread-b", "status": "idle"})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	shutdown := c.handleRPCEvent(jsonRPCMessage{Method: "thread/status/changed", Params: data})
	if shutdown {
		t.Fatalf("unexpected shutdown=true")
	}
	if got := c.getActiveTurnID(); got != "" {
		t.Fatalf("expected active turn cleared by mismatched terminal event, got %q", got)
	}
	if handled != 0 {
		t.Fatalf("expected handler not called on mismatch, got %d", handled)
	}
}

func TestHandleRPCEventAllowsMatchedThreadScopedEvent(t *testing.T) {
	c := &AppServerClient{AgentID: "agent-1", ThreadID: "thread-a"}
	c.setActiveTurnID("turn-a")
	handled := 0
	c.SetEventHandler(func(Event) { handled++ })

	data, err := json.Marshal(map[string]any{"threadId": "thread-a", "status": "idle"})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	shutdown := c.handleRPCEvent(jsonRPCMessage{Method: "thread/status/changed", Params: data})
	if shutdown {
		t.Fatalf("unexpected shutdown=true")
	}
	if got := c.getActiveTurnID(); got != "" {
		t.Fatalf("expected active turn cleared, got %q", got)
	}
	if handled != 1 {
		t.Fatalf("expected handler called once, got %d", handled)
	}
}

func TestHandleRPCEventStringRequestIDSetsCallbacks(t *testing.T) {
	c := &AppServerClient{AgentID: "agent-1"}
	var captured Event
	handled := 0
	c.SetEventHandler(func(e Event) {
		handled++
		captured = e
	})

	id := jsonRPCID{}
	if err := id.UnmarshalJSON([]byte(`"req-1"`)); err != nil {
		t.Fatalf("unmarshal id failed: %v", err)
	}

	c.handleRPCEvent(jsonRPCMessage{ID: &id, Method: "item/tool/call", Params: json.RawMessage(`{}`)})

	if handled != 1 {
		t.Fatalf("expected one handled event, got %d", handled)
	}
	if captured.RequestID != nil {
		t.Fatalf("expected numeric RequestID to be nil for string id")
	}
	if len(captured.RequestIDRaw) == 0 {
		t.Fatalf("expected RequestIDRaw to be set")
	}
	if captured.RespondFunc == nil {
		t.Fatalf("expected RespondFunc to be set")
	}
	if captured.RespondResultFunc == nil {
		t.Fatalf("expected RespondResultFunc to be set")
	}
}

func TestHandleRPCResponseStringIDMatchesPending(t *testing.T) {
	c := &AppServerClient{AgentID: "agent-1"}
	id := jsonRPCID{}
	if err := id.UnmarshalJSON([]byte("\"req-2\"")); err != nil {
		t.Fatalf("unmarshal id failed: %v", err)
	}
	pc := &pendingCall{done: make(chan struct{})}
	c.pending.Store(id.pendingKey(), pc)
	defer c.pending.Delete(id.pendingKey())

	handled := c.handleRPCResponse(jsonRPCMessage{ID: &id, Result: json.RawMessage("{\"ok\":true}")})
	if !handled {
		t.Fatalf("expected response to be handled")
	}
	select {
	case <-pc.done:
		if pc.err != nil {
			t.Fatalf("expected nil error, got %v", pc.err)
		}
		if string(pc.result) != "{\"ok\":true}" {
			t.Fatalf("unexpected result: %s", string(pc.result))
		}
	default:
		t.Fatalf("pending call not resolved")
	}
}

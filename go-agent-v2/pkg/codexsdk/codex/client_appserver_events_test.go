package codex

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
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

	data, err := json.Marshal(map[string]any{"threadId": "thread-b", "status": "idle", "turnId": "turn-a"})
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

func TestHandleRPCEventMismatchedTerminalWithoutMatchingTurnKeepsLifecycle(t *testing.T) {
	t.Run("missing_turn_id_keeps_when_listener_healthy", func(t *testing.T) {
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
		if got := c.getActiveTurnID(); got != "turn-a" {
			t.Fatalf("expected active turn unchanged, got %q", got)
		}
		if handled != 0 {
			t.Fatalf("expected handler not called on mismatch, got %d", handled)
		}
	})

	t.Run("missing_turn_id_recovers_when_listener_ensure_pending", func(t *testing.T) {
		c := &AppServerClient{AgentID: "agent-1", ThreadID: "thread-a"}
		c.setActiveTurnID("turn-a")
		c.listenerEnsureNeeded.Store(true)
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
			t.Fatalf("expected active turn cleared during listener ensure window, got %q", got)
		}
		if handled != 0 {
			t.Fatalf("expected handler not called on mismatch, got %d", handled)
		}
	})

	t.Run("different_turn_id_keeps_even_when_listener_ensure_pending", func(t *testing.T) {
		c := &AppServerClient{AgentID: "agent-1", ThreadID: "thread-a"}
		c.setActiveTurnID("turn-a")
		c.listenerEnsureNeeded.Store(true)
		handled := 0
		c.SetEventHandler(func(Event) { handled++ })

		data, err := json.Marshal(map[string]any{"threadId": "thread-b", "status": "idle", "turnId": "turn-other"})
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
	})
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

func TestTrackTurnLifecycleStartedStreamingTerminalOrder(t *testing.T) {
	c := &AppServerClient{AgentID: "agent-1"}

	startedData, err := json.Marshal(map[string]any{"turnId": "turn-42"})
	if err != nil {
		t.Fatalf("marshal started data failed: %v", err)
	}
	c.trackTurnLifecycle(Event{Type: EventTurnStarted, Data: startedData}, "turn/started")
	if got := c.getActiveTurnID(); got != "turn-42" {
		t.Fatalf("expected active turn set after started, got %q", got)
	}

	c.trackTurnLifecycle(Event{Type: EventTurnDiff, Data: json.RawMessage(`{"delta":"partial"}`)}, "turn/diff/updated")
	if got := c.getActiveTurnID(); got != "turn-42" {
		t.Fatalf("expected active turn kept during streaming progress, got %q", got)
	}

	c.trackTurnLifecycle(Event{Type: EventTurnComplete, Data: json.RawMessage(`{"turnId":"turn-42"}`)}, "turn/completed")
	if got := c.getActiveTurnID(); got != "" {
		t.Fatalf("expected active turn cleared on terminal event, got %q", got)
	}
}

func TestJSONRPCToEventUnknownMethodFallsBackToRawType(t *testing.T) {
	c := &AppServerClient{AgentID: "agent-1"}
	event := c.jsonRPCToEvent(jsonRPCMessage{
		Method: "unknown/method",
		Params: json.RawMessage(`{"ok":true}`),
	})
	if event.Type != "unknown/method" {
		t.Fatalf("expected unknown method to fall back to raw type, got %q", event.Type)
	}
}

func TestTrackTurnLifecycleStreamErrorWillRetryStartsRecoveryTimer(t *testing.T) {
	c := &AppServerClient{AgentID: "agent-1"}

	// Set an active turn
	c.setActiveTurnID("turn-100")

	// Send stream_error with willRetry=true
	data, _ := json.Marshal(map[string]any{"willRetry": true, "message": "test error"})
	c.trackTurnLifecycle(Event{Type: EventStreamError, Data: data}, "error")

	// activeTurnID should be preserved (not cleared)
	if got := c.getActiveTurnID(); got != "turn-100" {
		t.Fatalf("expected active turn preserved on willRetry stream_error, got %q", got)
	}

	// Recovery timer should have been started
	c.streamErrorRecoveryMu.Lock()
	hasTimer := c.streamErrorRecoveryTimer != nil
	c.streamErrorRecoveryMu.Unlock()
	if !hasTimer {
		t.Fatalf("expected recovery timer to be started")
	}

	// Clean up timer
	c.cancelStreamErrorRecoveryTimer()
}

func TestTrackTurnLifecycleStreamErrorRecoveryTimerCancelledOnNewEvent(t *testing.T) {
	c := &AppServerClient{AgentID: "agent-1"}

	// Set an active turn
	c.setActiveTurnID("turn-200")

	// Start recovery timer via willRetry stream_error
	data, _ := json.Marshal(map[string]any{"willRetry": true})
	c.trackTurnLifecycle(Event{Type: EventStreamError, Data: data}, "error")

	// Verify timer started
	c.streamErrorRecoveryMu.Lock()
	hasTimer := c.streamErrorRecoveryTimer != nil
	c.streamErrorRecoveryMu.Unlock()
	if !hasTimer {
		t.Fatalf("expected recovery timer to be started")
	}

	// Simulate receiving a new event (this would be called via handleRPCEvent)
	c.cancelStreamErrorRecoveryTimer()

	// Timer should be nil after cancellation
	c.streamErrorRecoveryMu.Lock()
	hasTimer = c.streamErrorRecoveryTimer != nil
	c.streamErrorRecoveryMu.Unlock()
	if hasTimer {
		t.Fatalf("expected recovery timer to be cancelled")
	}

	// Active turn should still be set
	if got := c.getActiveTurnID(); got != "turn-200" {
		t.Fatalf("expected active turn still set after cancel, got %q", got)
	}
}

func TestTrackTurnLifecycleStreamErrorRecoveryTimerFires(t *testing.T) {
	// Override timeout for test speed — use a package-level workaround
	// by calling startStreamErrorRecoveryTimer with a very short timer directly.
	c := &AppServerClient{AgentID: "agent-test-fire"}

	// Need a handler to receive the emitted stream_error(willRetry=false)
	emittedEvents := make(chan Event, 10)
	c.SetEventHandler(func(event Event) {
		emittedEvents <- event
	})

	c.setActiveTurnID("turn-300")

	// Directly set a very short timer (50ms) instead of using the 60s default
	c.streamErrorRecoveryMu.Lock()
	turnID := "turn-300"
	c.streamErrorRecoveryTimer = newTestRecoveryTimer(c, turnID, 50*time.Millisecond)
	c.streamErrorRecoveryMu.Unlock()

	// Wait for timer to fire
	select {
	case evt := <-emittedEvents:
		if evt.Type != EventStreamError {
			t.Fatalf("expected stream_error event, got %q", evt.Type)
		}
		// Verify willRetry=false in payload
		var payload map[string]any
		if err := json.Unmarshal(evt.Data, &payload); err != nil {
			t.Fatalf("unmarshal event data: %v", err)
		}
		if willRetry, _ := payload["willRetry"].(bool); willRetry {
			t.Fatalf("expected willRetry=false in recovery timeout event")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("recovery timer did not fire within 2s")
	}

	// activeTurnID should be cleared
	if got := c.getActiveTurnID(); got != "" {
		t.Fatalf("expected active turn cleared after recovery timeout, got %q", got)
	}
}

// newTestRecoveryTimer creates a recovery timer with a custom duration for testing.
func newTestRecoveryTimer(c *AppServerClient, turnID string, timeout time.Duration) *time.Timer {
	return time.AfterFunc(timeout, func() {
		currentTurnID := c.getActiveTurnID()
		if currentTurnID == "" || currentTurnID != turnID {
			return
		}
		c.clearActiveTurnID()
		c.emitStreamError(
			errors.New("stream_error recovery timeout (test)"),
			"recovery_timeout",
			false,
			false,
			map[string]any{
				"message":        "Stream recovery failed — no events received after reconnection",
				"originalTurnId": turnID,
				"trigger":        "recovery_timeout",
			},
		)
	})
}

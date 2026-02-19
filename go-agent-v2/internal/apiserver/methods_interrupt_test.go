package apiserver

import (
	"errors"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestNormalizeInterruptState(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "idle"},
		{name: "done", in: "done", want: "idle"},
		{name: "failed", in: "failed", want: "error"},
		{name: "running", in: "running", want: "running"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeInterruptState(tc.in); got != tc.want {
				t.Fatalf("normalizeInterruptState(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsInterruptNoActiveTurnError(t *testing.T) {
	if !isInterruptNoActiveTurnError(errors.New("no active turn")) {
		t.Fatalf("expected no active turn error to be detected")
	}
	if isInterruptNoActiveTurnError(errors.New("permission denied")) {
		t.Fatalf("unexpected match for unrelated error")
	}
}

func TestWaitInterruptSettled(t *testing.T) {
	t.Run("event-driven interrupted", func(t *testing.T) {
		srv := &Server{
			activeTurns:         make(map[string]*trackedTurn),
			turnWatchdogTimeout: time.Second,
		}
		threadID := "thread-event"
		_ = srv.beginTrackedTurn(threadID, "turn-event")
		if ok := srv.markTrackedTurnInterruptRequested(threadID); !ok {
			t.Fatalf("expected interrupt mark success")
		}
		go func() {
			time.Sleep(20 * time.Millisecond)
			_, _ = srv.completeTrackedTurn(threadID, "completed", "turn_complete")
		}()
		confirmed, state, _ := srv.waitInterruptSettled(threadID, 200*time.Millisecond)
		if !confirmed {
			t.Fatalf("confirmed = false, want true")
		}
		if state != "interrupted" {
			t.Fatalf("state = %q, want interrupted", state)
		}
	})

	t.Run("immediate idle", func(t *testing.T) {
		srv := &Server{uiRuntime: uistate.NewRuntimeManager()}
		threadID := "thread-1"
		srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
			{ID: threadID, Name: threadID, State: "idle"},
		})
		confirmed, state, _ := srv.waitInterruptSettled(threadID, 50*time.Millisecond)
		if !confirmed {
			t.Fatalf("confirmed = false, want true")
		}
		if state != "idle" {
			t.Fatalf("state = %q, want idle", state)
		}
	})

	t.Run("timeout while running", func(t *testing.T) {
		srv := &Server{uiRuntime: uistate.NewRuntimeManager()}
		threadID := "thread-2"
		srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
			{ID: threadID, Name: threadID, State: "running"},
		})
		confirmed, state, _ := srv.waitInterruptSettled(threadID, 10*time.Millisecond)
		if confirmed {
			t.Fatalf("confirmed = true, want false")
		}
		if state != "running" {
			t.Fatalf("state = %q, want running", state)
		}
	})
}

func TestWaitInterruptOutcome(t *testing.T) {
	t.Run("no active turn remains no active", func(t *testing.T) {
		srv := &Server{uiRuntime: uistate.NewRuntimeManager()}
		threadID := "thread-no-active"
		srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
			{ID: threadID, Name: threadID, State: "idle"},
		})
		confirmed, state, _, observedActive := srv.waitInterruptOutcome(threadID, 50*time.Millisecond, false)
		if confirmed {
			t.Fatalf("confirmed = true, want false")
		}
		if observedActive {
			t.Fatalf("observedActive = true, want false")
		}
		if state != "idle" {
			t.Fatalf("state = %q, want idle", state)
		}
	})

	t.Run("active hint confirms settle", func(t *testing.T) {
		srv := &Server{uiRuntime: uistate.NewRuntimeManager()}
		threadID := "thread-active-hint"
		srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
			{ID: threadID, Name: threadID, State: "idle"},
		})
		confirmed, state, _, observedActive := srv.waitInterruptOutcome(threadID, 50*time.Millisecond, true)
		if !confirmed {
			t.Fatalf("confirmed = false, want true")
		}
		if !observedActive {
			t.Fatalf("observedActive = false, want true")
		}
		if state != "idle" {
			t.Fatalf("state = %q, want idle", state)
		}
	})
}

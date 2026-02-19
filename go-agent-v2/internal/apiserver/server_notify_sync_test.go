package apiserver

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestNotifyReplaySyntheticTurnCompletedSyncsUIRuntime(t *testing.T) {
	srv := &Server{
		uiRuntime: uistate.NewRuntimeManager(),
	}
	threadID := "thread-notify-sync"
	srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
		{ID: threadID, Name: threadID, State: "idle"},
	})

	started := uistate.NormalizeEventFromPayload("turn_started", "turn/started", map[string]any{
		"threadId": threadID,
	})
	srv.uiRuntime.ApplyAgentEvent(threadID, started, map[string]any{"threadId": threadID})
	if got := srv.uiRuntime.Snapshot().Statuses[threadID]; got != "thinking" {
		t.Fatalf("status before notify = %q, want thinking", got)
	}

	srv.Notify("turn/completed", map[string]any{
		"threadId": threadID,
		"status":   "completed",
		"reason":   "synthetic_completion",
	})

	if got := srv.uiRuntime.Snapshot().Statuses[threadID]; got != "idle" {
		t.Fatalf("status after synthetic turn/completed = %q, want idle", got)
	}
}

func TestNotifySkipsReplayWhenUITypeAlreadyPresent(t *testing.T) {
	srv := &Server{
		uiRuntime: uistate.NewRuntimeManager(),
	}
	threadID := "thread-notify-skip"
	srv.uiRuntime.ReplaceThreads([]uistate.ThreadSnapshot{
		{ID: threadID, Name: threadID, State: "idle"},
	})

	started := uistate.NormalizeEventFromPayload("turn_started", "turn/started", map[string]any{
		"threadId": threadID,
	})
	srv.uiRuntime.ApplyAgentEvent(threadID, started, map[string]any{"threadId": threadID})
	if got := srv.uiRuntime.Snapshot().Statuses[threadID]; got != "thinking" {
		t.Fatalf("status before notify = %q, want thinking", got)
	}

	srv.Notify("turn/completed", map[string]any{
		"threadId": threadID,
		"uiType":   "turn_complete",
	})

	if got := srv.uiRuntime.Snapshot().Statuses[threadID]; got != "thinking" {
		t.Fatalf("status after notify with uiType = %q, want thinking", got)
	}
}

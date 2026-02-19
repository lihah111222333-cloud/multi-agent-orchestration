package apiserver

import (
	"testing"
	"time"
)

func TestTrackedTurnLifecycle(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}

	turnID := srv.beginTrackedTurn("thread-1", "turn-1")
	if turnID != "turn-1" {
		t.Fatalf("turnID = %q, want turn-1", turnID)
	}
	if !srv.hasActiveTrackedTurn("thread-1") {
		t.Fatalf("expected active tracked turn")
	}

	completion, ok := srv.completeTrackedTurn("thread-1", "completed", "turn_complete")
	if !ok {
		t.Fatalf("expected turn completion")
	}
	if completion["status"] != "completed" {
		t.Fatalf("status = %v, want completed", completion["status"])
	}
	if srv.hasActiveTrackedTurn("thread-1") {
		t.Fatalf("expected no active tracked turn after completion")
	}
}

func TestTrackedTurnInterruptMapsToInterrupted(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}

	_ = srv.beginTrackedTurn("thread-2", "turn-2")
	if ok := srv.markTrackedTurnInterruptRequested("thread-2"); !ok {
		t.Fatalf("expected interrupt mark success")
	}

	completion, ok := srv.completeTrackedTurn("thread-2", "completed", "turn_complete")
	if !ok {
		t.Fatalf("expected turn completion")
	}
	if completion["status"] != "interrupted" {
		t.Fatalf("status = %v, want interrupted", completion["status"])
	}
}

func TestCompleteTrackedTurnByIDRejectsMismatchedTurn(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}

	_ = srv.beginTrackedTurn("thread-3", "turn-3")
	if _, ok := srv.completeTrackedTurnByID("thread-3", "turn-x", "completed", "turn_complete"); ok {
		t.Fatalf("expected completion rejected for mismatched turn id")
	}
	if !srv.hasActiveTrackedTurn("thread-3") {
		t.Fatalf("turn should still be active after mismatched completion")
	}
}

func TestMaybeFinalizeTrackedTurnFromStreamError(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}
	_ = srv.beginTrackedTurn("thread-4", "turn-4")

	gotMethod := ""
	gotStatus := ""
	srv.SetNotifyHook(func(method string, params any) {
		if method != "turn/completed" {
			return
		}
		gotMethod = method
		if payload, ok := params.(map[string]any); ok {
			if status, ok := payload["status"].(string); ok {
				gotStatus = status
			}
		}
	})

	srv.maybeFinalizeTrackedTurn("thread-4", "stream_error", "codex/event/stream_error", map[string]any{
		"reason": "idle_timeout",
	})

	if gotMethod != "turn/completed" {
		t.Fatalf("notify method = %q, want turn/completed", gotMethod)
	}
	if gotStatus != "failed" {
		t.Fatalf("notify status = %q, want failed", gotStatus)
	}
	if srv.hasActiveTrackedTurn("thread-4") {
		t.Fatalf("expected active turn cleared after stream error")
	}
}

func TestMaybeFinalizeTrackedTurnFromTurnAborted(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}
	_ = srv.beginTrackedTurn("thread-6", "turn-6")

	payload := map[string]any{
		"reason": "turn_aborted",
	}
	srv.maybeFinalizeTrackedTurn("thread-6", "turn_aborted", "turn/completed", payload)

	if payload["status"] != "interrupted" {
		t.Fatalf("status = %v, want interrupted", payload["status"])
	}
	if srv.hasActiveTrackedTurn("thread-6") {
		t.Fatalf("expected active turn cleared after turn_aborted")
	}
}

func TestTrackedTurnWatchdogTimeout(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: 20 * time.Millisecond,
	}

	done := make(chan map[string]any, 1)
	srv.SetNotifyHook(func(method string, params any) {
		if method != "turn/completed" {
			return
		}
		if payload, ok := params.(map[string]any); ok {
			select {
			case done <- payload:
			default:
			}
		}
	})

	_ = srv.beginTrackedTurn("thread-5", "turn-5")

	select {
	case payload := <-done:
		if payload["status"] != "failed" {
			t.Fatalf("status = %v, want failed", payload["status"])
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected watchdog completion notification")
	}
}

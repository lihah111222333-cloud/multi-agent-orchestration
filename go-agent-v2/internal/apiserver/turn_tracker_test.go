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

func TestCompleteTrackedTurnByIDMismatchedIDStillCompletes(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}

	_ = srv.beginTrackedTurn("thread-3", "turn-3")
	completion, ok := srv.completeTrackedTurnByID("thread-3", "turn-x", "completed", "turn_complete")
	if !ok {
		t.Fatalf("expected completion to succeed even with mismatched turn id")
	}
	if completion["status"] != "completed" {
		t.Fatalf("status = %v, want completed", completion["status"])
	}
	if srv.hasActiveTrackedTurn("thread-3") {
		t.Fatalf("expected active turn cleared after mismatched completion")
	}
}

func TestMaybeFinalizeTrackedTurnMismatchedIDStillCompletes(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}
	_ = srv.beginTrackedTurn("thread-7", "turn-7a")

	payload := map[string]any{
		"turnId": "turn-7b",
	}
	srv.maybeFinalizeTrackedTurn("thread-7", "turn_complete", "turn/completed", payload)

	if srv.hasActiveTrackedTurn("thread-7") {
		t.Fatalf("expected active turn cleared even with mismatched turnId")
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

func TestMaybeFinalizeTrackedTurnSkipsRetryableStreamError(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}
	_ = srv.beginTrackedTurn("thread-retry", "turn-retry")

	notified := false
	srv.SetNotifyHook(func(method string, params any) {
		if method == "turn/completed" {
			notified = true
		}
	})

	srv.maybeFinalizeTrackedTurn("thread-retry", "stream_error", "error", map[string]any{
		"message":   "Reconnecting... 1/5",
		"willRetry": true,
	})

	if notified {
		t.Fatalf("retryable stream error should not emit synthetic turn/completed")
	}
	if !srv.hasActiveTrackedTurn("thread-retry") {
		t.Fatalf("retryable stream error should keep active tracked turn")
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

func TestMaybeFinalizeTrackedTurnFromThreadStatusIdle(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}
	_ = srv.beginTrackedTurn("thread-8", "turn-8")

	gotMethod := ""
	gotStatus := ""
	gotReason := ""
	srv.SetNotifyHook(func(method string, params any) {
		if method != "turn/completed" {
			return
		}
		gotMethod = method
		payload, _ := params.(map[string]any)
		gotStatus, _ = payload["status"].(string)
		gotReason, _ = payload["reason"].(string)
	})

	srv.maybeFinalizeTrackedTurn("thread-8", "thread/status/changed", "thread/status/changed", map[string]any{
		"status": map[string]any{
			"type": "idle",
		},
	})

	if gotMethod != "turn/completed" {
		t.Fatalf("notify method = %q, want turn/completed", gotMethod)
	}
	if gotStatus != "completed" {
		t.Fatalf("notify status = %q, want completed", gotStatus)
	}
	if gotReason != "thread_status_idle" {
		t.Fatalf("notify reason = %q, want thread_status_idle", gotReason)
	}
	if srv.hasActiveTrackedTurn("thread-8") {
		t.Fatalf("expected active turn cleared after thread/status/changed idle")
	}
}

func TestMaybeFinalizeTrackedTurnPreservesLastAgentMessage(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}
	_ = srv.beginTrackedTurn("thread-9", "turn-9")

	payload := map[string]any{
		"turn": map[string]any{
			"id":               "turn-9",
			"status":           "completed",
			"lastAgentMessage": "已完成：修复了 JSON-RPC 回调，并补充了测试。",
		},
	}
	srv.maybeFinalizeTrackedTurn("thread-9", "turn_complete", "turn/completed", payload)

	turn, ok := payload["turn"].(map[string]any)
	if !ok {
		t.Fatalf("payload turn missing")
	}
	gotSummary, _ := turn["lastAgentMessage"].(string)
	if gotSummary == "" {
		t.Fatalf("turn.lastAgentMessage should be preserved")
	}
}

func TestMaybeFinalizeTrackedTurnUsesCachedSummaryForSyntheticCompletion(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}
	_ = srv.beginTrackedTurn("thread-10", "turn-10")
	srv.rememberTrackedTurnSummary("thread-10", "turn-10", "cached_summary")

	gotSummary := ""
	srv.SetNotifyHook(func(method string, params any) {
		if method != "turn/completed" {
			return
		}
		payload, _ := params.(map[string]any)
		turn, _ := payload["turn"].(map[string]any)
		gotSummary, _ = turn["lastAgentMessage"].(string)
	})

	srv.maybeFinalizeTrackedTurn("thread-10", "thread/status/changed", "thread/status/changed", map[string]any{
		"turnId": "turn-10",
		"status": map[string]any{
			"type": "idle",
		},
	})

	if gotSummary != "cached_summary" {
		t.Fatalf("turn.lastAgentMessage = %q, want cached_summary", gotSummary)
	}
}

func TestCaptureAndInjectTurnSummaryBindsMissingTurnIDToActiveTurn(t *testing.T) {
	srv := &Server{
		activeTurns:         make(map[string]*trackedTurn),
		turnWatchdogTimeout: time.Second,
	}
	_ = srv.beginTrackedTurn("thread-11", "turn-11")

	payload := map[string]any{
		"msg": map[string]any{
			"last_agent_message": "bound_to_active_turn",
		},
	}
	srv.captureAndInjectTurnSummary("thread-11", "codex/event/task_complete", "codex/event/task_complete", payload)

	if got := srv.lookupTrackedTurnSummary("thread-11", "turn-11"); got != "bound_to_active_turn" {
		t.Fatalf("summary for active turn = %q, want bound_to_active_turn", got)
	}
	if got := srv.lookupTrackedTurnSummary("thread-11", "turn-other"); got != "" {
		t.Fatalf("summary should not leak to other turn, got %q", got)
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

package tracker

import (
	"sync"
	"testing"
	"time"
)

func TestEnsureTurnTrackerStateLocked_Defaults(t *testing.T) {
	var (
		mu             sync.Mutex
		activeTurns    map[string]*trackedTurn
		watchdog       time.Duration
		summaryCache   map[string]TrackedTurnSummaryCacheEntry
		summaryTTL     time.Duration
		stallThreshold time.Duration
		stallHeartbeat time.Duration
	)
	state := TurnTrackerState{
		Mu:                  &mu,
		ActiveTurns:         &activeTurns,
		TurnWatchdogTimeout: &watchdog,
		TurnSummaryCache:    &summaryCache,
		TurnSummaryTTL:      &summaryTTL,
		StallThreshold:      &stallThreshold,
		StallHeartbeat:      &stallHeartbeat,
	}

	EnsureTurnTrackerStateLocked(state)

	if activeTurns == nil {
		t.Fatal("activeTurns should be initialized")
	}
	if watchdog != DefaultTurnWatchdogTimeout {
		t.Fatalf("watchdog = %v, want %v", watchdog, DefaultTurnWatchdogTimeout)
	}
	if summaryCache == nil {
		t.Fatal("summaryCache should be initialized")
	}
	if summaryTTL != DefaultTrackedTurnSummaryTTL {
		t.Fatalf("summaryTTL = %v, want %v", summaryTTL, DefaultTrackedTurnSummaryTTL)
	}
	if stallThreshold != DefaultStallThreshold {
		t.Fatalf("stallThreshold = %v, want %v", stallThreshold, DefaultStallThreshold)
	}
	if stallHeartbeat != DefaultStallHeartbeat {
		t.Fatalf("stallHeartbeat = %v, want %v", stallHeartbeat, DefaultStallHeartbeat)
	}
}

func TestExportedPayloadHelpers(t *testing.T) {
	payload := map[string]any{
		"turn": map[string]any{
			"id":     "turn-1",
			"status": "completed",
			"reason": "done",
		},
	}

	if got := ExtractTrackedTurnID(payload); got != "turn-1" {
		t.Fatalf("ExtractTrackedTurnID = %q, want %q", got, "turn-1")
	}
	if got := ExtractTrackedTurnStatus(payload); got != "completed" {
		t.Fatalf("ExtractTrackedTurnStatus = %q, want %q", got, "completed")
	}
	if got := ExtractTrackedTurnReason(payload); got != "done" {
		t.Fatalf("ExtractTrackedTurnReason = %q, want %q", got, "done")
	}
	if got := NormalizeTrackedTurnStatus("Complete"); got != "completed" {
		t.Fatalf("NormalizeTrackedTurnStatus = %q, want %q", got, "completed")
	}
	if got := TrackedTurnSummaryCacheKey(" th ", " id "); got != "th\x00id" {
		t.Fatalf("TrackedTurnSummaryCacheKey = %q, want %q", got, "th\x00id")
	}
}

func TestExportedPayloadMutationHelpers(t *testing.T) {
	payload := map[string]any{"turn": map[string]any{"id": "turn-1"}}
	completion := map[string]any{
		"status": "completed",
		"reason": "done",
		"turn": map[string]any{
			"status": "completed",
			"reason": "done",
		},
	}

	MergeTrackedTurnCompletionPayload(payload, completion)
	if payload["status"] != "completed" {
		t.Fatalf("payload status = %v, want completed", payload["status"])
	}
	turnObj, ok := payload["turn"].(map[string]any)
	if !ok {
		t.Fatal("payload.turn should be map")
	}
	if turnObj["id"] != "turn-1" {
		t.Fatalf("payload.turn.id = %v, want turn-1", turnObj["id"])
	}
	if turnObj["reason"] != "done" {
		t.Fatalf("payload.turn.reason = %v, want done", turnObj["reason"])
	}

	msgPayload := map[string]any{}
	InjectTrackedTurnSummary(msgPayload, "hello")
	if msgPayload["lastAgentMessage"] != "hello" {
		t.Fatalf("lastAgentMessage = %v, want hello", msgPayload["lastAgentMessage"])
	}
}

func TestThreadStatusTerminalFromPayload_Exported(t *testing.T) {
	status, reason, terminal := ThreadStatusTerminalFromPayload(map[string]any{"status": "idle"})
	if !terminal || status != "completed" || reason != "thread_status_idle" {
		t.Fatalf("got (%q, %q, %v), want (%q, %q, %v)", status, reason, terminal, "completed", "thread_status_idle", true)
	}
}

package codex

import (
	"testing"
	"time"
)

func TestReadFailureOpensCircuitBreaker(t *testing.T) {
	c := &AppServerClient{}
	base := time.Now()

	var opened bool
	for i := 0; i < appServerCircuitBreakerThreshold; i++ {
		_, _, opened = c.noteReadFailure(base.Add(time.Duration(i) * time.Second))
	}
	if !opened {
		t.Fatal("expected circuit breaker to open on threshold")
	}

	remaining, snapshot := c.circuitRemaining(base.Add(3 * time.Second))
	if remaining <= 0 {
		t.Fatalf("expected positive circuit remaining, got %v", remaining)
	}
	if !snapshot.CircuitOpen {
		t.Fatal("snapshot should report circuit open")
	}
}

func TestReadFailureBurstPrefersRespawn(t *testing.T) {
	c := &AppServerClient{}
	base := time.Now()

	for i := 0; i < appServerRespawnEscalationThreshold; i++ {
		c.noteReadFailure(base.Add(time.Duration(i) * time.Second))
	}
	if !c.shouldPreferRespawn(base.Add(2 * time.Second)) {
		t.Fatal("shouldPreferRespawn should be true after burst failures")
	}

	c.noteReconnectSuccess(base.Add(3 * time.Second))
	if c.shouldPreferRespawn(base.Add(4 * time.Second)) {
		t.Fatal("shouldPreferRespawn should reset after reconnect success")
	}
}

func TestNotInitializedEscalates(t *testing.T) {
	c := &AppServerClient{}
	base := time.Now()

	_, shouldRecover := c.noteNotInitializedRPC(base)
	if shouldRecover {
		t.Fatal("first not-initialized should not trigger recovery")
	}
	snapshot, shouldRecover := c.noteNotInitializedRPC(base.Add(1 * time.Second))
	if !shouldRecover {
		t.Fatal("second not-initialized in window should trigger recovery")
	}
	if snapshot.NotInitializedStreak < 2 {
		t.Fatalf("not_initialized_streak = %d, want >=2", snapshot.NotInitializedStreak)
	}
}

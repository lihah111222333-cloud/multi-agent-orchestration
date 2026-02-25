package codexadapter

import (
	"strings"
	"time"
)

// WaitInterruptOutcome waits until interrupt settles based on tracker/runtime state.
func WaitInterruptOutcome(
	threadID string,
	timeout time.Duration,
	activeHint bool,
	waitTrackedTurnTerminal func(string, time.Duration) (string, bool),
	readThreadRuntimeState func(string) string,
) (bool, string, int64, bool) {
	start := time.Now()
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false, "idle", 0, false
	}
	observedActive := activeHint
	if waitTrackedTurnTerminal != nil {
		if terminalStatus, ok := waitTrackedTurnTerminal(id, timeout); ok {
			afterState := NormalizeInterruptState(terminalStatus)
			confirmed := strings.EqualFold(terminalStatus, "interrupted")
			return confirmed, afterState, time.Since(start).Milliseconds(), true
		}
	}
	if readThreadRuntimeState == nil {
		return false, "", time.Since(start).Milliseconds(), observedActive
	}
	deadline := start.Add(timeout)
	lastState := readThreadRuntimeState(id)
	if IsInterruptActiveState(lastState) {
		observedActive = true
	}
	for {
		if !IsInterruptActiveState(lastState) {
			if !observedActive {
				return false, lastState, time.Since(start).Milliseconds(), false
			}
			return true, lastState, time.Since(start).Milliseconds(), true
		}
		observedActive = true
		if time.Now().After(deadline) {
			return false, lastState, time.Since(start).Milliseconds(), true
		}
		time.Sleep(120 * time.Millisecond)
		lastState = readThreadRuntimeState(id)
	}
}

// WaitInterruptOutcome waits for terminal state using adapter-owned tracker/runtime state.
func (a *Adapter) WaitInterruptOutcome(threadID string, timeout time.Duration, activeHint bool) (bool, string, int64, bool) {
	return WaitInterruptOutcome(threadID, timeout, activeHint, a.WaitTrackedTurnTerminal, a.ReadThreadRuntimeState)
}

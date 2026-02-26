package codexadapter

import (
	"strings"
	"time"
)

// peekTrackedTurnMeta returns current tracked turn metadata for one thread.
// peekTrackedTurnMeta returns current tracked turn metadata for one thread.
func (a *Adapter) peekTrackedTurnMeta(threadID string) (string, time.Time, bool, bool) {
	return peekTrackedTurnMetaCore(a.trackerHelperState(), threadID)
}

// markTrackedTurnStallHint marks one-shot stall hint flag.
// markTrackedTurnStallHint marks one-shot stall hint flag.
func (a *Adapter) markTrackedTurnStallHint(threadID, turnID string) bool {
	return markTrackedTurnStallHintCore(a.trackerHelperState(), threadID, turnID)
}

// touchTrackedTurnLastEvent updates turn heartbeat using adapter-owned tracker state.
// touchTrackedTurnLastEvent updates turn heartbeat using adapter-owned tracker state.
func (a *Adapter) touchTrackedTurnLastEvent(threadID string) {
	touchTrackedTurnLastEventCore(a.trackerHelperState(), threadID)
}

// StartApprovalStallHeartbeat starts approval heartbeat with adapter-owned tracker state.
func (a *Adapter) StartApprovalStallHeartbeat(threadID string) func() {
	_, _, _, stallThreshold := a.trackerState()
	return startApprovalStallHeartbeat(threadID, stallThreshold, defaultStallThreshold, a.touchTrackedTurnLastEvent)
}

// StartDynamicToolStallHeartbeat starts heartbeat while dynamic tools execute.
func (a *Adapter) StartDynamicToolStallHeartbeat(threadID string) func() {
	return startApprovalStallHeartbeat(threadID, a.stallThreshold(), defaultStallThreshold, a.touchTrackedTurnLastEvent)
}

// stallThreshold returns current tracker stall threshold.
func (a *Adapter) stallThreshold() time.Duration {
	return a.trackerDuration(func(state turnTrackerState) *time.Duration {
		return state.StallThreshold
	}, defaultStallThreshold)
}

// SetStallThreshold updates tracker stall threshold.
func (a *Adapter) SetStallThreshold(threshold time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration {
		return state.StallThreshold
	}, threshold)
}

// stallHeartbeat returns current configured stall heartbeat interval.
func (a *Adapter) stallHeartbeat() time.Duration {
	return a.trackerDuration(func(state turnTrackerState) *time.Duration {
		return state.StallHeartbeat
	}, defaultStallHeartbeat)
}

// SetStallHeartbeat updates stall heartbeat interval.
func (a *Adapter) SetStallHeartbeat(interval time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration {
		return state.StallHeartbeat
	}, interval)
}

func (a *Adapter) nextTrackedTurnStallDecision(threadID, turnID string, stallThreshold time.Duration) trackedTurnStallDecision {
	return nextTrackedTurnStallDecisionCore(a.trackerHelperState(), threadID, turnID, stallThreshold, a.checkTurnStall)
}

// checkTurnStall advances stall detection state machine.
// checkTurnStall advances stall detection state machine.
func (a *Adapter) checkTurnStall(threadID string, turnID string) {
	checkTurnStallCore(a.trackerHelperState(), threadID, turnID, a.handleStallGracePeriod, a.executeStallAutoInterrupt, a.checkTurnStall)
}

func (a *Adapter) handleStallGracePeriod(threadID, turnID string, silent, threshold time.Duration) {
	handleStallGracePeriodCore(a.trackerHelperState(), threadID, turnID, silent, threshold, trackerRuntimePushAlert(a.uiRuntime()), a.checkTurnStall)
}

// executeStallAutoInterrupt performs /interrupt and fallback completion when stalled.
// executeStallAutoInterrupt performs /interrupt and fallback completion when stalled.
func (a *Adapter) executeStallAutoInterrupt(
	threadID string,
	turnID string,
	silent time.Duration,
	threshold time.Duration,
) {
	executeStallAutoInterruptCore(threadID, turnID, silent, threshold, trackerRuntimePushAlert(a.uiRuntime()), a.markTrackedTurnInterruptRequested, a.cancelCodeRuns, trackerInterruptSender(a.manager(), a.SendCommand), a.completeTrackedTurnByID, a.trackerNotify())
}

func approvalstallHeartbeatInterval(stallThreshold, fallback time.Duration) time.Duration {
	base := stallThreshold
	if base <= 0 {
		base = fallback
	}
	if base <= 0 {
		base = defaultStallThreshold
	}
	interval := base / 3
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	return interval
}

func startApprovalStallHeartbeat(threadID string, stallThreshold, fallback time.Duration, touch func(string)) func() {
	id := strings.TrimSpace(threadID)
	interval := approvalstallHeartbeatInterval(stallThreshold, fallback)
	ticker := time.NewTicker(interval)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if touch != nil {
					touch(id)
				}
			case <-stop:
				ticker.Stop()
				return
			}
		}
	}()
	return func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
}

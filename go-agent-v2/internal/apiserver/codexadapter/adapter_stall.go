package codexadapter

import (
	"strings"
	"time"

	trackersvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/tracker"
)

func (a *Adapter) trackerStateAndNotify() (turnTrackerState, func(string, any)) {
	if a == nil {
		return turnTrackerState{}, nil
	}
	if a.ctx != nil {
		return a.tracker, a.ctx.Notify
	}
	return a.tracker, nil
}

func (a *Adapter) trackerState() turnTrackerState {
	state, _ := a.trackerStateAndNotify()
	return state
}

func (a *Adapter) applyTrackedTurnTransition(threadID string, req trackedTurnTransitionRequest) trackedTurnTransitionResult {
	return trackersvc.ApplyTrackedTurnTransitionCore(a.trackerState(), threadID, req)
}

func (a *Adapter) activeTrackedTurnID(threadID string) (string, bool) {
	state := a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{})
	if !state.Found || strings.TrimSpace(state.TurnID) == "" {
		return "", false
	}
	return state.TurnID, true
}

func (a *Adapter) hasActiveTrackedTurn(threadID string) bool {
	return a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{}).Found
}

func (a *Adapter) markTrackedTurnInterruptRequested(threadID string) bool {
	state := a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{MarkInterruptRequested: true})
	return state.Found && state.InterruptRequested
}

func (a *Adapter) waitTrackedTurnTerminal(threadID string, timeout time.Duration) (string, bool) {
	return trackersvc.WaitTrackedTurnTerminalCore(a.trackerState(), threadID, timeout)
}

func (a *Adapter) completeTrackedTurnByID(threadID, turnID, status, reason string) (map[string]any, bool) {
	return trackersvc.CompleteTrackedTurnByIDCore(a.trackerState(), threadID, turnID, status, reason)
}

func (a *Adapter) beginTrackedTurn(threadID, turnID string) string {
	state, notify := a.trackerStateAndNotify()
	return trackersvc.BeginTrackedTurnCore(state, threadID, turnID, a.completeTrackedTurnByID, notify, a.checkTurnStall, a.recoverProcess)
}

func (a *Adapter) trackerDuration(getter func(turnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	return trackersvc.TrackerDurationCore(a.trackerState(), getter, fallback)
}

func (a *Adapter) setTrackerDuration(getter func(turnTrackerState) *time.Duration, value time.Duration) {
	trackersvc.SetTrackerDurationCore(a.trackerState(), getter, value)
}

func (a *Adapter) SetStallThreshold(threshold time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration { return state.StallThreshold }, threshold)
}

func (a *Adapter) SetStallHeartbeat(interval time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration { return state.StallHeartbeat }, interval)
}

func (a *Adapter) touchTrackedTurnLastEvent(threadID string) {
	trackersvc.TouchTrackedTurnLastEventCore(a.trackerState(), threadID)
}

func (a *Adapter) StartApprovalStallHeartbeat(threadID string) func() {
	return a.StartDynamicToolStallHeartbeat(threadID)
}

func (a *Adapter) StartDynamicToolStallHeartbeat(threadID string) func() {
	return trackersvc.StartStallHeartbeat(threadID, a.trackerDuration(func(state turnTrackerState) *time.Duration { return state.StallThreshold }, defaultStallThreshold), defaultStallThreshold, defaultStallThreshold, a.touchTrackedTurnLastEvent)
}

func (a *Adapter) checkTurnStall(threadID string, turnID string) {
	trackersvc.CheckTurnStallCore(a.trackerState(), threadID, turnID, a.handleStallGracePeriod, a.executeStallAutoInterrupt, a.checkTurnStall)
}

func (a *Adapter) handleStallGracePeriod(threadID, turnID string, silent, threshold time.Duration) {
	trackersvc.HandleStallGracePeriodCore(a.trackerState(), threadID, turnID, silent, threshold, trackersvc.TrackerRuntimePushAlert(a.uiRuntime()), a.checkTurnStall)
}

func (a *Adapter) executeStallAutoInterrupt(threadID string, turnID string, silent time.Duration, threshold time.Duration) {
	_, notify := a.trackerStateAndNotify()
	trackersvc.ExecuteStallAutoInterruptCore(
		threadID,
		turnID,
		silent,
		threshold,
		trackersvc.TrackerRuntimePushAlert(a.uiRuntime()),
		a.markTrackedTurnInterruptRequested,
		a.cancelCodeRuns,
		trackersvc.TrackerInterruptSender(a.managerProcess, a.sendCommandFromAny),
		a.completeTrackedTurnByID,
		notify,
		a.recoverProcess,
	)
}

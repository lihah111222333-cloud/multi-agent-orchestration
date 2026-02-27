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
	var notify func(string, any)
	if deps := a.Context(); deps != nil {
		notify = deps.Notify
	}
	return a.tracker, notify
}

func (a *Adapter) applyTrackedTurnTransition(threadID string, req trackedTurnTransitionRequest) trackedTurnTransitionResult {
	state, _ := a.trackerStateAndNotify()
	return trackersvc.ApplyTrackedTurnTransitionCore(state, threadID, req)
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
	state, _ := a.trackerStateAndNotify()
	return trackersvc.WaitTrackedTurnTerminalCore(state, threadID, timeout)
}

func (a *Adapter) completeTrackedTurnByID(threadID, turnID, status, reason string) (map[string]any, bool) {
	state, _ := a.trackerStateAndNotify()
	return trackersvc.CompleteTrackedTurnByIDCore(state, threadID, turnID, status, reason)
}

func (a *Adapter) beginTrackedTurn(threadID, turnID string) string {
	state, notify := a.trackerStateAndNotify()
	return trackersvc.BeginTrackedTurnCore(state, threadID, turnID, a.completeTrackedTurnByID, notify, a.checkTurnStall, a.recoverProcess)
}

func (a *Adapter) trackerDuration(getter func(turnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	state, _ := a.trackerStateAndNotify()
	return trackersvc.TrackerDurationCore(state, getter, fallback)
}

func (a *Adapter) setTrackerDuration(getter func(turnTrackerState) *time.Duration, value time.Duration) {
	state, _ := a.trackerStateAndNotify()
	trackersvc.SetTrackerDurationCore(state, getter, value)
}

func (a *Adapter) SetStallThreshold(threshold time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration { return state.StallThreshold }, threshold)
}

func (a *Adapter) SetStallHeartbeat(interval time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration { return state.StallHeartbeat }, interval)
}

func (a *Adapter) touchTrackedTurnLastEvent(threadID string) {
	state, _ := a.trackerStateAndNotify()
	trackersvc.TouchTrackedTurnLastEventCore(state, threadID)
}

func (a *Adapter) StartApprovalStallHeartbeat(threadID string) func() {
	stallThreshold := a.trackerDuration(func(state turnTrackerState) *time.Duration { return state.StallThreshold }, defaultStallThreshold)
	return trackersvc.StartStallHeartbeat(threadID, stallThreshold, defaultStallThreshold, defaultStallThreshold, a.touchTrackedTurnLastEvent)
}

func (a *Adapter) StartDynamicToolStallHeartbeat(threadID string) func() {
	stallThreshold := a.trackerDuration(func(state turnTrackerState) *time.Duration { return state.StallThreshold }, defaultStallThreshold)
	return trackersvc.StartStallHeartbeat(threadID, stallThreshold, defaultStallThreshold, defaultStallThreshold, a.touchTrackedTurnLastEvent)
}

func (a *Adapter) checkTurnStall(threadID string, turnID string) {
	state, _ := a.trackerStateAndNotify()
	trackersvc.CheckTurnStallCore(state, threadID, turnID, a.handleStallGracePeriod, a.executeStallAutoInterrupt, a.checkTurnStall)
}

func (a *Adapter) handleStallGracePeriod(threadID, turnID string, silent, threshold time.Duration) {
	state, _ := a.trackerStateAndNotify()
	trackersvc.HandleStallGracePeriodCore(state, threadID, turnID, silent, threshold, trackersvc.TrackerRuntimePushAlert(a.uiRuntime()), a.checkTurnStall)
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

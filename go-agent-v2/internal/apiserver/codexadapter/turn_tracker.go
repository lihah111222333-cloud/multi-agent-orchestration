package codexadapter

import (
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	trackersvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/tracker"
)

const (
	DefaultTurnWatchdogTimeout        = trackersvc.DefaultTurnWatchdogTimeout
	DefaultTrackedTurnSummaryTTL      = trackersvc.DefaultTrackedTurnSummaryTTL
	TrackedTurnSummaryCacheMaxEntries = trackersvc.TrackedTurnSummaryCacheMaxEntries
	defaultStallThreshold             = trackersvc.DefaultStallThreshold
	defaultStallHeartbeat             = trackersvc.DefaultStallHeartbeat
)

// Type aliases keep adapter state shape stable while forwarding logic to service/tracker.
type (
	trackedTurn                  = trackersvc.TrackedTurn
	trackedTurnFinalizeRequest   = trackersvc.TrackedTurnFinalizeRequest
	trackedTurnTransitionRequest = trackersvc.TrackedTurnTransitionRequest
	trackedTurnTransitionResult  = trackersvc.TrackedTurnTransitionResult
	trackedTurnSummaryCacheEntry = trackersvc.TrackedTurnSummaryCacheEntry
	turnTrackerState             = trackersvc.TurnTrackerState
	trackedTurnStallAction       = trackersvc.TrackedTurnStallAction
	trackedTurnStallDecision     = trackersvc.TrackedTurnStallDecision
	trackerAlertRuntime          = trackersvc.TrackerAlertRuntime
)

const (
	trackedTurnStallNoop          = trackersvc.TrackedTurnStallNoop
	trackedTurnStallRescheduled   = trackersvc.TrackedTurnStallRescheduled
	trackedTurnStallEnterGrace    = trackersvc.TrackedTurnStallEnterGrace
	trackedTurnStallAutoInterrupt = trackersvc.TrackedTurnStallAutoInterrupt
)

func ensureTurnTrackerStateLocked(state turnTrackerState) {
	trackersvc.EnsureTurnTrackerStateLocked(state)
}

// ensureTurnTrackerStateLocked initializes tracker defaults using adapter-owned state.
func (a *Adapter) ensureTurnTrackerStateLocked() {
	if a == nil {
		return
	}
	ensureTurnTrackerStateLocked(a.trackerHelperState())
}

func (a *Adapter) trackerHelperState() turnTrackerState {
	if a == nil {
		return turnTrackerState{}
	}
	return a.tracker
}

func (a *Adapter) trackerNotify() func(string, any) {
	return a.notifier()
}

func trackerDurationOrDefault(value *time.Duration, fallback time.Duration) time.Duration {
	return trackersvc.TrackerDurationOrDefault(value, fallback)
}

func (a *Adapter) withTrackerStateLock(fn func(turnTrackerState)) {
	withTrackerStateLockCore(a.trackerHelperState(), fn)
}

func (a *Adapter) trackerDuration(getter func(turnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	return trackerDurationCore(a.trackerHelperState(), getter, fallback)
}

func (a *Adapter) setTrackerDuration(getter func(turnTrackerState) *time.Duration, value time.Duration) {
	setTrackerDurationCore(a.trackerHelperState(), getter, value)
}

func (a *Adapter) trackerState() (map[string]*trackedTurn, *sync.Mutex, time.Duration, time.Duration) {
	return trackerStateCore(a.trackerHelperState())
}

func (a *Adapter) applyTrackedTurnTransition(threadID string, req trackedTurnTransitionRequest) trackedTurnTransitionResult {
	return applyTrackedTurnTransitionCore(a.trackerHelperState(), threadID, req)
}

func (a *Adapter) withActiveTurn(threadID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	return withActiveTurnCore(a.trackerHelperState(), threadID, fn)
}

func (a *Adapter) withActiveTurnByID(threadID, turnID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	return withActiveTurnByIDCore(a.trackerHelperState(), threadID, turnID, fn)
}

var (
	supersedeActiveTurn               = trackersvc.SupersedeActiveTurn
	normalizeTrackedTurnStatus        = trackersvc.NormalizeTrackedTurnStatus
	extractTrackedString              = trackersvc.ExtractTrackedString
	mergeTrackedTurnCompletionPayload = trackersvc.MergeTrackedTurnCompletionPayload
	captureAndInjectTurnSummaryCore   = trackersvc.CaptureAndInjectTurnSummaryCore
)

// beginTrackedTurn establishes tracked turn state and supersedes old one when needed.
func (a *Adapter) beginTrackedTurn(threadID, turnID string) string {
	return beginTrackedTurnCore(
		a.trackerHelperState(),
		threadID,
		turnID,
		a.completeTrackedTurnByID,
		a.trackerNotify(),
		a.checkTurnStall,
	)
}

// hasActiveTrackedTurn checks whether a thread has an active tracked turn.
func (a *Adapter) hasActiveTrackedTurn(threadID string) bool {
	return a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{}).Found
}

// activeTrackedTurnID returns current tracked turn id for a thread.
func (a *Adapter) activeTrackedTurnID(threadID string) (string, bool) {
	state := a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{})
	if !state.Found || strings.TrimSpace(state.TurnID) == "" {
		return "", false
	}
	return state.TurnID, true
}

// markTrackedTurnInterruptRequested marks interrupt intent on current tracked turn.
func (a *Adapter) markTrackedTurnInterruptRequested(threadID string) bool {
	state := a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{MarkInterruptRequested: true})
	return state.Found && state.InterruptRequested
}

// waitTrackedTurnTerminal waits until tracked turn reaches terminal status or timeout.
func (a *Adapter) waitTrackedTurnTerminal(threadID string, timeout time.Duration) (string, bool) {
	return waitTrackedTurnTerminalCore(a.trackerHelperState(), threadID, timeout)
}

// completeTrackedTurnByID closes a tracked turn and returns completion payload.
func (a *Adapter) completeTrackedTurnByID(threadID, turnID, status, reason string) (map[string]any, bool) {
	return completeTrackedTurnByIDCore(a.trackerHelperState(), threadID, turnID, status, reason)
}

func threadStatusTerminalFromPayload(payload map[string]any) (status string, reason string, terminal bool) {
	return trackersvc.ThreadStatusTerminalFromPayload(payload)
}

func extractTrackedTurnID(payload map[string]any) string {
	return trackersvc.ExtractTrackedTurnID(payload)
}

func extractTrackedTurnStatus(payload map[string]any) string {
	return trackersvc.ExtractTrackedTurnStatus(payload)
}

func extractTrackedTurnReason(payload map[string]any) string {
	return trackersvc.ExtractTrackedTurnReason(payload)
}

func trackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	return trackersvc.TrackedTurnTerminalFromEvent(eventType, method, payload)
}

func trackedTurnSummaryFromPayload(payload map[string]any) string {
	return trackersvc.TrackedTurnSummaryFromPayload(payload)
}

func trackedTurnSummaryCacheKey(threadID, turnID string) string {
	return trackersvc.TrackedTurnSummaryCacheKey(threadID, turnID)
}

func injectTrackedTurnSummary(payload map[string]any, summary string) {
	trackersvc.InjectTrackedTurnSummary(payload, summary)
}

func isTerminalEventType(eventType, method string) bool {
	return trackersvc.IsTerminalEventType(eventType, method)
}

func rememberTrackedTurnSummary(state turnTrackerState, turnMu *sync.Mutex, threadID, turnID, summary string) {
	trackersvc.RememberTrackedTurnSummary(state, turnMu, threadID, turnID, summary)
}

func lookupTrackedTurnSummary(state turnTrackerState, turnMu *sync.Mutex, threadID, turnID string) string {
	return trackersvc.LookupTrackedTurnSummary(state, turnMu, threadID, turnID)
}

func withTrackerStateLockCore(state turnTrackerState, fn func(turnTrackerState)) {
	trackersvc.WithTrackerStateLockCore(state, fn)
}

func trackerDurationCore(state turnTrackerState, getter func(turnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	return trackersvc.TrackerDurationCore(state, getter, fallback)
}

func setTrackerDurationCore(state turnTrackerState, getter func(turnTrackerState) *time.Duration, value time.Duration) {
	trackersvc.SetTrackerDurationCore(state, getter, value)
}

func trackerStateCore(state turnTrackerState) (map[string]*trackedTurn, *sync.Mutex, time.Duration, time.Duration) {
	return trackersvc.TrackerStateCore(state)
}

func applyTrackedTurnTransitionCore(state turnTrackerState, threadID string, req trackedTurnTransitionRequest) trackedTurnTransitionResult {
	return trackersvc.ApplyTrackedTurnTransitionCore(state, threadID, req)
}

func withActiveTurnCore(state turnTrackerState, threadID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	return trackersvc.WithActiveTurnCore(state, threadID, fn)
}

func withActiveTurnByIDCore(state turnTrackerState, threadID, turnID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	return trackersvc.WithActiveTurnByIDCore(state, threadID, turnID, fn)
}

func beginTrackedTurnCore(
	state turnTrackerState,
	threadID string,
	turnID string,
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
	checkTurnStall func(string, string),
) string {
	return trackersvc.BeginTrackedTurnCore(state, threadID, turnID, completeTrackedTurnByID, notify, checkTurnStall)
}

func waitTrackedTurnTerminalCore(state turnTrackerState, threadID string, timeout time.Duration) (string, bool) {
	return trackersvc.WaitTrackedTurnTerminalCore(state, threadID, timeout)
}

func completeTrackedTurnByIDCore(state turnTrackerState, threadID, turnID, status, reason string) (map[string]any, bool) {
	return trackersvc.CompleteTrackedTurnByIDCore(state, threadID, turnID, status, reason)
}

func peekTrackedTurnMetaCore(state turnTrackerState, threadID string) (string, time.Time, bool, bool) {
	return trackersvc.PeekTrackedTurnMetaCore(state, threadID)
}

func markTrackedTurnStallHintCore(state turnTrackerState, threadID, turnID string) bool {
	return trackersvc.MarkTrackedTurnStallHintCore(state, threadID, turnID)
}

func touchTrackedTurnLastEventCore(state turnTrackerState, threadID string) {
	trackersvc.TouchTrackedTurnLastEventCore(state, threadID)
}

func nextTrackedTurnStallDecisionCore(state turnTrackerState, threadID, turnID string, stallThreshold time.Duration, checkTurnStall func(string, string)) trackedTurnStallDecision {
	return trackersvc.NextTrackedTurnStallDecisionCore(state, threadID, turnID, stallThreshold, checkTurnStall)
}

func checkTurnStallCore(
	state turnTrackerState,
	threadID string,
	turnID string,
	handleStallGracePeriod func(threadID, turnID string, silent, threshold time.Duration),
	executeStallAutoInterrupt func(threadID, turnID string, silent, threshold time.Duration),
	checkTurnStall func(string, string),
) {
	trackersvc.CheckTurnStallCore(state, threadID, turnID, handleStallGracePeriod, executeStallAutoInterrupt, checkTurnStall)
}

func handleStallGracePeriodCore(
	state turnTrackerState,
	threadID string,
	turnID string,
	silent time.Duration,
	threshold time.Duration,
	pushAlert func(threadID, category, message string),
	checkTurnStall func(string, string),
) {
	trackersvc.HandleStallGracePeriodCore(state, threadID, turnID, silent, threshold, pushAlert, checkTurnStall)
}

func trackerRuntimePushAlert(runtime trackerAlertRuntime) func(threadID, category, message string) {
	return trackersvc.TrackerRuntimePushAlert(runtime)
}

func trackerInterruptSender(manager *runner.AgentManager, sendCommand func(*runner.AgentProcess, string, string) error) func(string) (bool, error) {
	if manager == nil || sendCommand == nil {
		return nil
	}
	return func(threadID string) (bool, error) {
		proc := manager.Get(threadID)
		if proc == nil {
			return false, nil
		}
		if err := sendCommand(proc, "/interrupt", ""); err != nil {
			return true, err
		}
		return true, nil
	}
}

func executeStallAutoInterruptCore(
	threadID string,
	turnID string,
	silent time.Duration,
	threshold time.Duration,
	pushAlert func(threadID, category, message string),
	markTrackedTurnInterruptRequested func(string) bool,
	cancelCodeRuns func(string) int,
	sendInterrupt func(string) (bool, error),
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
) {
	trackersvc.ExecuteStallAutoInterruptCore(threadID, turnID, silent, threshold, pushAlert, markTrackedTurnInterruptRequested, cancelCodeRuns, sendInterrupt, completeTrackedTurnByID, notify)
}

func maybeFinalizeTrackedTurnCore(state turnTrackerState, threadID, eventType, method string, payload map[string]any, notify func(string, any)) {
	trackersvc.MaybeFinalizeTrackedTurnCore(state, threadID, eventType, method, payload, notify)
}

func finalizeTrackedTurnEventCore(state turnTrackerState, threadID, eventType, method string, payload map[string]any, notify func(string, any)) {
	trackersvc.FinalizeTrackedTurnEventCore(state, threadID, eventType, method, payload, notify)
}

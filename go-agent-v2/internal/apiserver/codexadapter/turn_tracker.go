package codexadapter

import (
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	trackerconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/tracker"
)

const (
	DefaultTurnWatchdogTimeout        = trackerconsumer.DefaultTurnWatchdogTimeout
	DefaultTrackedTurnSummaryTTL      = trackerconsumer.DefaultTrackedTurnSummaryTTL
	TrackedTurnSummaryCacheMaxEntries = trackerconsumer.TrackedTurnSummaryCacheMaxEntries
	defaultStallThreshold             = trackerconsumer.DefaultStallThreshold
	defaultStallHeartbeat             = trackerconsumer.DefaultStallHeartbeat
)

// Type aliases keep adapter state shape stable while forwarding logic to service/tracker.
type (
	trackedTurn                  = trackerconsumer.TrackedTurn
	trackedTurnFinalizeRequest   = trackerconsumer.TrackedTurnFinalizeRequest
	trackedTurnTransitionRequest = trackerconsumer.TrackedTurnTransitionRequest
	trackedTurnTransitionResult  = trackerconsumer.TrackedTurnTransitionResult
	trackedTurnSummaryCacheEntry = trackerconsumer.TrackedTurnSummaryCacheEntry
	turnTrackerState             = trackerconsumer.TurnTrackerState
	trackedTurnStallAction       = trackerconsumer.TrackedTurnStallAction
	trackedTurnStallDecision     = trackerconsumer.TrackedTurnStallDecision
	trackerAlertRuntime          = trackerconsumer.TrackerAlertRuntime
)

const (
	trackedTurnStallNoop          = trackerconsumer.TrackedTurnStallNoop
	trackedTurnStallRescheduled   = trackerconsumer.TrackedTurnStallRescheduled
	trackedTurnStallEnterGrace    = trackerconsumer.TrackedTurnStallEnterGrace
	trackedTurnStallAutoInterrupt = trackerconsumer.TrackedTurnStallAutoInterrupt
)

func ensureTurnTrackerStateLocked(state turnTrackerState) {
	trackerconsumer.EnsureTurnTrackerStateLocked(state)
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
	return trackerconsumer.TrackerDurationOrDefault(value, fallback)
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
	supersedeActiveTurn               = trackerconsumer.SupersedeActiveTurn
	normalizeTrackedTurnStatus        = trackerconsumer.NormalizeTrackedTurnStatus
	extractTrackedString              = trackerconsumer.ExtractTrackedString
	mergeTrackedTurnCompletionPayload = trackerconsumer.MergeTrackedTurnCompletionPayload
	captureAndInjectTurnSummaryCore   = trackerconsumer.CaptureAndInjectTurnSummaryCore
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
	return trackerconsumer.ThreadStatusTerminalFromPayload(payload)
}

func extractTrackedTurnID(payload map[string]any) string {
	return trackerconsumer.ExtractTrackedTurnID(payload)
}

func extractTrackedTurnStatus(payload map[string]any) string {
	return trackerconsumer.ExtractTrackedTurnStatus(payload)
}

func extractTrackedTurnReason(payload map[string]any) string {
	return trackerconsumer.ExtractTrackedTurnReason(payload)
}

func trackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	return trackerconsumer.TrackedTurnTerminalFromEvent(eventType, method, payload)
}

func trackedTurnSummaryFromPayload(payload map[string]any) string {
	return trackerconsumer.TrackedTurnSummaryFromPayload(payload)
}

func trackedTurnSummaryCacheKey(threadID, turnID string) string {
	return trackerconsumer.TrackedTurnSummaryCacheKey(threadID, turnID)
}

func injectTrackedTurnSummary(payload map[string]any, summary string) {
	trackerconsumer.InjectTrackedTurnSummary(payload, summary)
}

func isTerminalEventType(eventType, method string) bool {
	return trackerconsumer.IsTerminalEventType(eventType, method)
}

func rememberTrackedTurnSummary(state turnTrackerState, turnMu *sync.Mutex, threadID, turnID, summary string) {
	trackerconsumer.RememberTrackedTurnSummary(state, turnMu, threadID, turnID, summary)
}

func lookupTrackedTurnSummary(state turnTrackerState, turnMu *sync.Mutex, threadID, turnID string) string {
	return trackerconsumer.LookupTrackedTurnSummary(state, turnMu, threadID, turnID)
}

func withTrackerStateLockCore(state turnTrackerState, fn func(turnTrackerState)) {
	trackerconsumer.WithTrackerStateLockCore(state, fn)
}

func trackerDurationCore(state turnTrackerState, getter func(turnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	return trackerconsumer.TrackerDurationCore(state, getter, fallback)
}

func setTrackerDurationCore(state turnTrackerState, getter func(turnTrackerState) *time.Duration, value time.Duration) {
	trackerconsumer.SetTrackerDurationCore(state, getter, value)
}

func trackerStateCore(state turnTrackerState) (map[string]*trackedTurn, *sync.Mutex, time.Duration, time.Duration) {
	return trackerconsumer.TrackerStateCore(state)
}

func applyTrackedTurnTransitionCore(state turnTrackerState, threadID string, req trackedTurnTransitionRequest) trackedTurnTransitionResult {
	return trackerconsumer.ApplyTrackedTurnTransitionCore(state, threadID, req)
}

func withActiveTurnCore(state turnTrackerState, threadID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	return trackerconsumer.WithActiveTurnCore(state, threadID, fn)
}

func withActiveTurnByIDCore(state turnTrackerState, threadID, turnID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	return trackerconsumer.WithActiveTurnByIDCore(state, threadID, turnID, fn)
}

func beginTrackedTurnCore(
	state turnTrackerState,
	threadID string,
	turnID string,
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
	checkTurnStall func(string, string),
) string {
	return trackerconsumer.BeginTrackedTurnCore(state, threadID, turnID, completeTrackedTurnByID, notify, checkTurnStall)
}

func waitTrackedTurnTerminalCore(state turnTrackerState, threadID string, timeout time.Duration) (string, bool) {
	return trackerconsumer.WaitTrackedTurnTerminalCore(state, threadID, timeout)
}

func completeTrackedTurnByIDCore(state turnTrackerState, threadID, turnID, status, reason string) (map[string]any, bool) {
	return trackerconsumer.CompleteTrackedTurnByIDCore(state, threadID, turnID, status, reason)
}

func peekTrackedTurnMetaCore(state turnTrackerState, threadID string) (string, time.Time, bool, bool) {
	return trackerconsumer.PeekTrackedTurnMetaCore(state, threadID)
}

func markTrackedTurnStallHintCore(state turnTrackerState, threadID, turnID string) bool {
	return trackerconsumer.MarkTrackedTurnStallHintCore(state, threadID, turnID)
}

func touchTrackedTurnLastEventCore(state turnTrackerState, threadID string) {
	trackerconsumer.TouchTrackedTurnLastEventCore(state, threadID)
}

func nextTrackedTurnStallDecisionCore(state turnTrackerState, threadID, turnID string, stallThreshold time.Duration, checkTurnStall func(string, string)) trackedTurnStallDecision {
	return trackerconsumer.NextTrackedTurnStallDecisionCore(state, threadID, turnID, stallThreshold, checkTurnStall)
}

func checkTurnStallCore(
	state turnTrackerState,
	threadID string,
	turnID string,
	handleStallGracePeriod func(threadID, turnID string, silent, threshold time.Duration),
	executeStallAutoInterrupt func(threadID, turnID string, silent, threshold time.Duration),
	checkTurnStall func(string, string),
) {
	trackerconsumer.CheckTurnStallCore(state, threadID, turnID, handleStallGracePeriod, executeStallAutoInterrupt, checkTurnStall)
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
	trackerconsumer.HandleStallGracePeriodCore(state, threadID, turnID, silent, threshold, pushAlert, checkTurnStall)
}

func trackerRuntimePushAlert(runtime trackerAlertRuntime) func(threadID, category, message string) {
	return trackerconsumer.TrackerRuntimePushAlert(runtime)
}

func trackerInterruptSender(manager *codexsdk.AgentManager, sendCommand func(*codexsdk.AgentProcess, string, string) error) func(string) (bool, error) {
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
	trackerconsumer.ExecuteStallAutoInterruptCore(threadID, turnID, silent, threshold, pushAlert, markTrackedTurnInterruptRequested, cancelCodeRuns, sendInterrupt, completeTrackedTurnByID, notify)
}

func maybeFinalizeTrackedTurnCore(state turnTrackerState, threadID, eventType, method string, payload map[string]any, notify func(string, any)) {
	trackerconsumer.MaybeFinalizeTrackedTurnCore(state, threadID, eventType, method, payload, notify)
}

func finalizeTrackedTurnEventCore(state turnTrackerState, threadID, eventType, method string, payload map[string]any, notify func(string, any)) {
	trackerconsumer.FinalizeTrackedTurnEventCore(state, threadID, eventType, method, payload, notify)
}

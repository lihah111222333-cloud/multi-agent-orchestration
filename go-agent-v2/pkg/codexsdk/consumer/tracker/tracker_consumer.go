package tracker

import (
	"sync"
	"time"

	trackersvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/tracker"
)

const (
	DefaultTurnWatchdogTimeout        = trackersvc.DefaultTurnWatchdogTimeout
	DefaultTrackedTurnSummaryTTL      = trackersvc.DefaultTrackedTurnSummaryTTL
	TrackedTurnSummaryCacheMaxEntries = trackersvc.TrackedTurnSummaryCacheMaxEntries
	DefaultStallThreshold             = trackersvc.DefaultStallThreshold
	DefaultStallHeartbeat             = trackersvc.DefaultStallHeartbeat
)

type TrackedTurn = trackersvc.TrackedTurn
type TrackedTurnFinalizeRequest = trackersvc.TrackedTurnFinalizeRequest
type TrackedTurnTransitionRequest = trackersvc.TrackedTurnTransitionRequest
type TrackedTurnTransitionResult = trackersvc.TrackedTurnTransitionResult
type TrackedTurnSummaryCacheEntry = trackersvc.TrackedTurnSummaryCacheEntry
type TurnTrackerState = trackersvc.TurnTrackerState
type TrackedTurnStallAction = trackersvc.TrackedTurnStallAction
type TrackedTurnStallDecision = trackersvc.TrackedTurnStallDecision
type TrackerAlertRuntime = trackersvc.TrackerAlertRuntime

const (
	TrackedTurnStallNoop          = trackersvc.TrackedTurnStallNoop
	TrackedTurnStallRescheduled   = trackersvc.TrackedTurnStallRescheduled
	TrackedTurnStallEnterGrace    = trackersvc.TrackedTurnStallEnterGrace
	TrackedTurnStallAutoInterrupt = trackersvc.TrackedTurnStallAutoInterrupt
)

func EnsureTurnTrackerStateLocked(state TurnTrackerState) {
	trackersvc.EnsureTurnTrackerStateLocked(state)
}

func TrackerDurationOrDefault(value *time.Duration, fallback time.Duration) time.Duration {
	return trackersvc.TrackerDurationOrDefault(value, fallback)
}

var (
	SupersedeActiveTurn               = trackersvc.SupersedeActiveTurn
	NormalizeTrackedTurnStatus        = trackersvc.NormalizeTrackedTurnStatus
	ExtractTrackedString              = trackersvc.ExtractTrackedString
	MergeTrackedTurnCompletionPayload = trackersvc.MergeTrackedTurnCompletionPayload
	CaptureAndInjectTurnSummaryCore   = trackersvc.CaptureAndInjectTurnSummaryCore
)

func ThreadStatusTerminalFromPayload(payload map[string]any) (status string, reason string, terminal bool) {
	return trackersvc.ThreadStatusTerminalFromPayload(payload)
}

func ExtractTrackedTurnID(payload map[string]any) string {
	return trackersvc.ExtractTrackedTurnID(payload)
}

func ExtractTrackedTurnStatus(payload map[string]any) string {
	return trackersvc.ExtractTrackedTurnStatus(payload)
}

func ExtractTrackedTurnReason(payload map[string]any) string {
	return trackersvc.ExtractTrackedTurnReason(payload)
}

func TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	return trackersvc.TrackedTurnTerminalFromEvent(eventType, method, payload)
}

func TrackedTurnSummaryFromPayload(payload map[string]any) string {
	return trackersvc.TrackedTurnSummaryFromPayload(payload)
}

func TrackedTurnSummaryCacheKey(threadID, turnID string) string {
	return trackersvc.TrackedTurnSummaryCacheKey(threadID, turnID)
}

func InjectTrackedTurnSummary(payload map[string]any, summary string) {
	trackersvc.InjectTrackedTurnSummary(payload, summary)
}

func IsTerminalEventType(eventType, method string) bool {
	return trackersvc.IsTerminalEventType(eventType, method)
}

func RememberTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID, summary string) {
	trackersvc.RememberTrackedTurnSummary(state, turnMu, threadID, turnID, summary)
}

func LookupTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID string) string {
	return trackersvc.LookupTrackedTurnSummary(state, turnMu, threadID, turnID)
}

func WithTrackerStateLockCore(state TurnTrackerState, fn func(TurnTrackerState)) {
	trackersvc.WithTrackerStateLockCore(state, fn)
}

func TrackerDurationCore(state TurnTrackerState, getter func(TurnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	return trackersvc.TrackerDurationCore(state, getter, fallback)
}

func SetTrackerDurationCore(state TurnTrackerState, getter func(TurnTrackerState) *time.Duration, value time.Duration) {
	trackersvc.SetTrackerDurationCore(state, getter, value)
}

func TrackerStateCore(state TurnTrackerState) (map[string]*TrackedTurn, *sync.Mutex, time.Duration, time.Duration) {
	return trackersvc.TrackerStateCore(state)
}

func ApplyTrackedTurnTransitionCore(state TurnTrackerState, threadID string, req TrackedTurnTransitionRequest) TrackedTurnTransitionResult {
	return trackersvc.ApplyTrackedTurnTransitionCore(state, threadID, req)
}

func WithActiveTurnCore(state TurnTrackerState, threadID string, fn func(threadID string, turn *TrackedTurn, activeTurns map[string]*TrackedTurn) bool) bool {
	return trackersvc.WithActiveTurnCore(state, threadID, fn)
}

func WithActiveTurnByIDCore(state TurnTrackerState, threadID, turnID string, fn func(threadID string, turn *TrackedTurn, activeTurns map[string]*TrackedTurn) bool) bool {
	return trackersvc.WithActiveTurnByIDCore(state, threadID, turnID, fn)
}

func BeginTrackedTurnCore(
	state TurnTrackerState,
	threadID string,
	turnID string,
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
	checkTurnStall func(string, string),
) string {
	return trackersvc.BeginTrackedTurnCore(state, threadID, turnID, completeTrackedTurnByID, notify, checkTurnStall)
}

func WaitTrackedTurnTerminalCore(state TurnTrackerState, threadID string, timeout time.Duration) (string, bool) {
	return trackersvc.WaitTrackedTurnTerminalCore(state, threadID, timeout)
}

func CompleteTrackedTurnByIDCore(state TurnTrackerState, threadID, turnID, status, reason string) (map[string]any, bool) {
	return trackersvc.CompleteTrackedTurnByIDCore(state, threadID, turnID, status, reason)
}

func PeekTrackedTurnMetaCore(state TurnTrackerState, threadID string) (string, time.Time, bool, bool) {
	return trackersvc.PeekTrackedTurnMetaCore(state, threadID)
}

func MarkTrackedTurnStallHintCore(state TurnTrackerState, threadID, turnID string) bool {
	return trackersvc.MarkTrackedTurnStallHintCore(state, threadID, turnID)
}

func TouchTrackedTurnLastEventCore(state TurnTrackerState, threadID string) {
	trackersvc.TouchTrackedTurnLastEventCore(state, threadID)
}

func NextTrackedTurnStallDecisionCore(
	state TurnTrackerState,
	threadID, turnID string,
	stallThreshold time.Duration,
	checkTurnStall func(string, string),
) TrackedTurnStallDecision {
	return trackersvc.NextTrackedTurnStallDecisionCore(state, threadID, turnID, stallThreshold, checkTurnStall)
}

func CheckTurnStallCore(
	state TurnTrackerState,
	threadID string,
	turnID string,
	handleStallGracePeriod func(threadID, turnID string, silent, threshold time.Duration),
	executeStallAutoInterrupt func(threadID, turnID string, silent, threshold time.Duration),
	checkTurnStall func(string, string),
) {
	trackersvc.CheckTurnStallCore(
		state,
		threadID,
		turnID,
		handleStallGracePeriod,
		executeStallAutoInterrupt,
		checkTurnStall,
	)
}

func HandleStallGracePeriodCore(
	state TurnTrackerState,
	threadID string,
	turnID string,
	silent time.Duration,
	threshold time.Duration,
	pushAlert func(threadID, category, message string),
	checkTurnStall func(string, string),
) {
	trackersvc.HandleStallGracePeriodCore(
		state,
		threadID,
		turnID,
		silent,
		threshold,
		pushAlert,
		checkTurnStall,
	)
}

func TrackerRuntimePushAlert(runtime TrackerAlertRuntime) func(threadID, category, message string) {
	return trackersvc.TrackerRuntimePushAlert(runtime)
}

func ExecuteStallAutoInterruptCore(
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
	trackersvc.ExecuteStallAutoInterruptCore(
		threadID,
		turnID,
		silent,
		threshold,
		pushAlert,
		markTrackedTurnInterruptRequested,
		cancelCodeRuns,
		sendInterrupt,
		completeTrackedTurnByID,
		notify,
	)
}

func MaybeFinalizeTrackedTurnCore(state TurnTrackerState, threadID, eventType, method string, payload map[string]any, notify func(string, any)) {
	trackersvc.MaybeFinalizeTrackedTurnCore(state, threadID, eventType, method, payload, notify)
}

func FinalizeTrackedTurnEventCore(state TurnTrackerState, threadID, eventType, method string, payload map[string]any, notify func(string, any)) {
	trackersvc.FinalizeTrackedTurnEventCore(state, threadID, eventType, method, payload, notify)
}

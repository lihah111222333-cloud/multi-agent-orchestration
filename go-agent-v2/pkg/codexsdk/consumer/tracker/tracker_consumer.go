package tracker

import (
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
type TrackedTurnTransitionRequest = trackersvc.TrackedTurnTransitionRequest
type TrackedTurnTransitionResult = trackersvc.TrackedTurnTransitionResult
type TrackedTurnSummaryCacheEntry = trackersvc.TrackedTurnSummaryCacheEntry
type TurnTrackerState = trackersvc.TurnTrackerState

var (
	NormalizeTrackedTurnStatus        = trackersvc.NormalizeTrackedTurnStatus
	ThreadStatusTerminalFromPayload   = trackersvc.ThreadStatusTerminalFromPayload
	ExtractTrackedString              = trackersvc.ExtractTrackedString
	ExtractTrackedTurnID              = trackersvc.ExtractTrackedTurnID
	ExtractTrackedTurnStatus          = trackersvc.ExtractTrackedTurnStatus
	ExtractTrackedTurnReason          = trackersvc.ExtractTrackedTurnReason
	TrackedTurnTerminalFromEvent      = trackersvc.TrackedTurnTerminalFromEvent
	TrackedTurnSummaryFromPayload     = trackersvc.TrackedTurnSummaryFromPayload
	TrackedTurnSummaryCacheKey        = trackersvc.TrackedTurnSummaryCacheKey
	InjectTrackedTurnSummary          = trackersvc.InjectTrackedTurnSummary
	MergeTrackedTurnCompletionPayload = trackersvc.MergeTrackedTurnCompletionPayload
	TrackerDurationCore               = trackersvc.TrackerDurationCore
	SetTrackerDurationCore            = trackersvc.SetTrackerDurationCore
	TrackerStateCore                  = trackersvc.TrackerStateCore
	ApplyTrackedTurnTransitionCore    = trackersvc.ApplyTrackedTurnTransitionCore
	BeginTrackedTurnCore              = trackersvc.BeginTrackedTurnCore
	WaitTrackedTurnTerminalCore       = trackersvc.WaitTrackedTurnTerminalCore
	CompleteTrackedTurnByIDCore       = trackersvc.CompleteTrackedTurnByIDCore
	TouchTrackedTurnLastEventCore     = trackersvc.TouchTrackedTurnLastEventCore
	CheckTurnStallCore                = trackersvc.CheckTurnStallCore
	HandleStallGracePeriodCore        = trackersvc.HandleStallGracePeriodCore
	TrackerRuntimePushAlert           = trackersvc.TrackerRuntimePushAlert
	ExecuteStallAutoInterruptCore     = trackersvc.ExecuteStallAutoInterruptCore
	CaptureAndInjectTurnSummaryCore   = trackersvc.CaptureAndInjectTurnSummaryCore
	FinalizeTrackedTurnEventCore      = trackersvc.FinalizeTrackedTurnEventCore
	ApprovalStallHeartbeatInterval    = trackersvc.ApprovalStallHeartbeatInterval
	StartStallHeartbeat               = trackersvc.StartStallHeartbeat
	TrackerInterruptSender            = trackersvc.TrackerInterruptSender
)

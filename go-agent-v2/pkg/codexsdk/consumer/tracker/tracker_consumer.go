package tracker

import (
	"strings"
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

var (
	EnsureTurnTrackerStateLocked      = trackersvc.EnsureTurnTrackerStateLocked
	TrackerDurationOrDefault          = trackersvc.TrackerDurationOrDefault
	SupersedeActiveTurn               = trackersvc.SupersedeActiveTurn
	NormalizeTrackedTurnStatus        = trackersvc.NormalizeTrackedTurnStatus
	ExtractTrackedString              = trackersvc.ExtractTrackedString
	MergeTrackedTurnCompletionPayload = trackersvc.MergeTrackedTurnCompletionPayload
	CaptureAndInjectTurnSummaryCore   = trackersvc.CaptureAndInjectTurnSummaryCore
	ThreadStatusTerminalFromPayload   = trackersvc.ThreadStatusTerminalFromPayload
	ExtractTrackedTurnID              = trackersvc.ExtractTrackedTurnID
	ExtractTrackedTurnStatus          = trackersvc.ExtractTrackedTurnStatus
	ExtractTrackedTurnReason          = trackersvc.ExtractTrackedTurnReason
	TrackedTurnTerminalFromEvent      = trackersvc.TrackedTurnTerminalFromEvent
	TrackedTurnSummaryFromPayload     = trackersvc.TrackedTurnSummaryFromPayload
	TrackedTurnSummaryCacheKey        = trackersvc.TrackedTurnSummaryCacheKey
	InjectTrackedTurnSummary          = trackersvc.InjectTrackedTurnSummary
	IsTerminalEventType               = trackersvc.IsTerminalEventType
	RememberTrackedTurnSummary        = trackersvc.RememberTrackedTurnSummary
	LookupTrackedTurnSummary          = trackersvc.LookupTrackedTurnSummary
	WithTrackerStateLockCore          = trackersvc.WithTrackerStateLockCore
	TrackerDurationCore               = trackersvc.TrackerDurationCore
	SetTrackerDurationCore            = trackersvc.SetTrackerDurationCore
	TrackerStateCore                  = trackersvc.TrackerStateCore
	ApplyTrackedTurnTransitionCore    = trackersvc.ApplyTrackedTurnTransitionCore
	WithActiveTurnCore                = trackersvc.WithActiveTurnCore
	WithActiveTurnByIDCore            = trackersvc.WithActiveTurnByIDCore
	BeginTrackedTurnCore              = trackersvc.BeginTrackedTurnCore
	WaitTrackedTurnTerminalCore       = trackersvc.WaitTrackedTurnTerminalCore
	CompleteTrackedTurnByIDCore       = trackersvc.CompleteTrackedTurnByIDCore
	PeekTrackedTurnMetaCore           = trackersvc.PeekTrackedTurnMetaCore
	MarkTrackedTurnStallHintCore      = trackersvc.MarkTrackedTurnStallHintCore
	TouchTrackedTurnLastEventCore     = trackersvc.TouchTrackedTurnLastEventCore
	NextTrackedTurnStallDecisionCore  = trackersvc.NextTrackedTurnStallDecisionCore
	CheckTurnStallCore                = trackersvc.CheckTurnStallCore
	HandleStallGracePeriodCore        = trackersvc.HandleStallGracePeriodCore
	TrackerRuntimePushAlert           = trackersvc.TrackerRuntimePushAlert
	ExecuteStallAutoInterruptCore     = trackersvc.ExecuteStallAutoInterruptCore
	MaybeFinalizeTrackedTurnCore      = trackersvc.MaybeFinalizeTrackedTurnCore
	FinalizeTrackedTurnEventCore      = trackersvc.FinalizeTrackedTurnEventCore
)

func ApprovalStallHeartbeatInterval(stallThreshold, fallback, defaultThreshold time.Duration) time.Duration {
	base := defaultThreshold
	if fallback > 0 {
		base = fallback
	}
	if stallThreshold > 0 {
		base = stallThreshold
	}
	interval := base / 3
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	return interval
}

func StartStallHeartbeat(threadID string, stallThreshold, fallback, defaultThreshold time.Duration, touch func(string)) func() {
	id := strings.TrimSpace(threadID)
	interval := ApprovalStallHeartbeatInterval(stallThreshold, fallback, defaultThreshold)
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

func TrackerInterruptSender(getProcess func(string) any, sendCommand func(any, string, string) error) func(string) (bool, error) {
	if getProcess == nil || sendCommand == nil {
		return nil
	}
	return func(threadID string) (bool, error) {
		proc := getProcess(threadID)
		if proc == nil {
			return false, nil
		}
		return true, sendCommand(proc, "/interrupt", "")
	}
}

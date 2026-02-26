package codexadapter

import (
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)


const (
	DefaultTurnWatchdogTimeout        = 10 * time.Minute
	DefaultTrackedTurnSummaryTTL      = 30 * time.Minute
	TrackedTurnSummaryCacheMaxEntries = 512
	defaultStallThreshold             = 480 * time.Second
	defaultStallHeartbeat             = 300 * time.Second
)

// trackedTurn is the turn lifecycle tracking state.
type trackedTurn struct {
	ID                   string
	ThreadID             string
	StartedAt            time.Time
	LastEventAt          time.Time
	InterruptRequested   bool
	InterruptRequestedAt time.Time
	StallHintLogged      bool
	StallGraceStarted    bool
	StallAutoInterrupted bool
	Done                 chan string
	Timer                *time.Timer
	StallTimer           *time.Timer
}

type trackedTurnFinalizeRequest struct {
	TurnID string
	Status string
	Reason string
}

type trackedTurnTransitionRequest struct {
	TouchHeartbeat         bool
	MarkInterruptRequested bool
	MarkStallHint          bool
	MarkStallHintForTurnID string
	Finalize               *trackedTurnFinalizeRequest
}

type trackedTurnTransitionResult struct {
	Found              bool
	ThreadID           string
	TurnID             string
	StartedAt          time.Time
	LastEventAt        time.Time
	InterruptRequested bool
	StallHintLogged    bool
	StallHintApplied   bool

	Finalized      bool
	FinalStatus    string
	FinalReason    string
	Completion     map[string]any
	ExpectedTurnID string
	TurnIDMismatch bool
}

// ensureTurnTrackerStateLocked initializes turn-tracker state and defaults.
func ensureTurnTrackerStateLocked(state turnTrackerState) {
	if state.ActiveTurns != nil && *state.ActiveTurns == nil {
		*state.ActiveTurns = make(map[string]*trackedTurn)
	}
	if state.TurnWatchdogTimeout != nil && *state.TurnWatchdogTimeout <= 0 {
		*state.TurnWatchdogTimeout = DefaultTurnWatchdogTimeout
	}
	if state.TurnSummaryCache != nil && *state.TurnSummaryCache == nil {
		*state.TurnSummaryCache = make(map[string]trackedTurnSummaryCacheEntry)
	}
	if state.TurnSummaryTTL != nil && *state.TurnSummaryTTL <= 0 {
		*state.TurnSummaryTTL = DefaultTrackedTurnSummaryTTL
	}
	if state.stallThreshold != nil && *state.stallThreshold <= 0 {
		*state.stallThreshold = defaultStallThreshold
	}
	if state.stallHeartbeat != nil && *state.stallHeartbeat <= 0 {
		*state.stallHeartbeat = defaultStallHeartbeat
	}
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
	if value != nil && *value > 0 {
		return *value
	}
	return fallback
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


func supersedeActiveTurn(activeTurns map[string]*trackedTurn, threadID, nextTurnID string) (map[string]any, string, bool) {
	if activeTurns == nil {
		return nil, "", false
	}
	prev, ok := activeTurns[threadID]
	if !ok || prev == nil {
		return nil, "", false
	}
	delete(activeTurns, threadID)
	if prev.Timer != nil {
		prev.Timer.Stop()
	}
	if prev.StallTimer != nil {
		prev.StallTimer.Stop()
	}
	doneSent := false
	if prev.Done != nil {
		select {
		case prev.Done <- "failed":
			doneSent = true
		default:
		}
	}

	prevAge := time.Since(prev.StartedAt)
	prevLastEventAge := time.Since(prev.LastEventAt)
	logFn := logger.Warn
	if prevAge < 5*time.Second && !prev.InterruptRequested {
		logFn = logger.Info
	}
	fields := append(threadLogFields(threadID),
		"prev_turn_id", prev.ID,
		"next_turn_id", nextTurnID,
		"prev_age_ms", prevAge.Milliseconds(),
		"prev_last_event_age_ms", prevLastEventAge.Milliseconds(),
		"prev_interrupt_requested", prev.InterruptRequested,
		"prev_done_sent", doneSent,
		"prev_stall_hint_logged", prev.StallHintLogged,
		"prev_stall_grace_started", prev.StallGraceStarted,
		"prev_stall_auto_interrupted", prev.StallAutoInterrupted,
	)
	logFn("turn tracker: superseding active turn", fields...)

	payload := map[string]any{
		"threadId": threadID,
		"turn": map[string]any{
			"id":     prev.ID,
			"status": "failed",
		},
		"status": "failed",
		"reason": "superseded_by_new_turn",
	}
	return payload, prev.ID, true
}

// beginTrackedTurn establishes tracked turn state and supersedes old one when needed.
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
// waitTrackedTurnTerminal waits until tracked turn reaches terminal status or timeout.
func (a *Adapter) waitTrackedTurnTerminal(threadID string, timeout time.Duration) (string, bool) {
	return waitTrackedTurnTerminalCore(a.trackerHelperState(), threadID, timeout)
}


// completeTrackedTurnByID closes a tracked turn and returns completion payload.
// completeTrackedTurnByID closes a tracked turn and returns completion payload.
func (a *Adapter) completeTrackedTurnByID(threadID, turnID, status, reason string) (map[string]any, bool) {
	return completeTrackedTurnByIDCore(a.trackerHelperState(), threadID, turnID, status, reason)
}


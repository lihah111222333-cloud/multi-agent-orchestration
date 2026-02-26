package codexadapter

import (
	"fmt"
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

// trackedTurnSummaryCacheEntry caches the latest summary for one tracked turn.
type trackedTurnSummaryCacheEntry struct {
	TurnID    string
	Summary   string
	UpdatedAt time.Time
}

// turnTrackerState carries mutable turn-tracker state owned by the caller.
type turnTrackerState struct {
	Mu                  *sync.Mutex
	ActiveTurns         *map[string]*trackedTurn
	TurnWatchdogTimeout *time.Duration
	TurnSummaryCache    *map[string]trackedTurnSummaryCacheEntry
	TurnSummaryTTL      *time.Duration
	stallThreshold      *time.Duration
	stallHeartbeat      *time.Duration
}

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
	TouchHeartbeat        bool
	MarkInterruptRequested bool
	MarkStallHint         bool
	MarkStallHintForTurnID string
	Finalize              *trackedTurnFinalizeRequest
}

type trackedTurnTransitionResult struct {
	Found             bool
	ThreadID          string
	TurnID            string
	StartedAt         time.Time
	LastEventAt       time.Time
	InterruptRequested bool
	StallHintLogged   bool
	StallHintApplied  bool

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
	if a == nil || fn == nil {
		return
	}
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	ensureTurnTrackerStateLocked(state)
	fn(state)
}

func (a *Adapter) trackerDuration(getter func(turnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	if getter == nil {
		return fallback
	}
	value := fallback
	a.withTrackerStateLock(func(state turnTrackerState) {
		value = trackerDurationOrDefault(getter(state), fallback)
	})
	return value
}

func (a *Adapter) setTrackerDuration(getter func(turnTrackerState) *time.Duration, value time.Duration) {
	if value <= 0 || getter == nil {
		return
	}
	a.withTrackerStateLock(func(state turnTrackerState) {
		target := getter(state)
		if target != nil {
			*target = value
		}
	})
}

func (a *Adapter) trackerState() (map[string]*trackedTurn, *sync.Mutex, time.Duration, time.Duration) {
	if a == nil {
		return nil, nil, 0, 0
	}
	state := a.trackerHelperState()
	var activeTurns map[string]*trackedTurn
	if state.ActiveTurns != nil {
		activeTurns = *state.ActiveTurns
	}
	turnMu := state.Mu
	watchdogTimeout := trackerDurationOrDefault(state.TurnWatchdogTimeout, DefaultTurnWatchdogTimeout)
	stallThreshold := trackerDurationOrDefault(state.stallThreshold, defaultStallThreshold)
	return activeTurns, turnMu, watchdogTimeout, stallThreshold
}

func (a *Adapter) applyTrackedTurnTransition(threadID string, req trackedTurnTransitionRequest) trackedTurnTransitionResult {
	result := trackedTurnTransitionResult{}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return result
	}

	activeTurns, turnMu, _, _ := a.trackerState()
	if turnMu == nil || activeTurns == nil {
		return result
	}

	turnMu.Lock()
	defer turnMu.Unlock()
	a.ensureTurnTrackerStateLocked()

	turn, ok := activeTurns[id]
	if !ok || turn == nil {
		return result
	}

	result.Found = true
	result.ThreadID = id
	result.TurnID = strings.TrimSpace(turn.ID)
	result.StartedAt = turn.StartedAt
	result.LastEventAt = turn.LastEventAt
	result.InterruptRequested = turn.InterruptRequested
	result.StallHintLogged = turn.StallHintLogged

	if req.TouchHeartbeat {
		turn.LastEventAt = time.Now()
		result.LastEventAt = turn.LastEventAt
	}
	if req.MarkInterruptRequested {
		turn.InterruptRequested = true
		turn.InterruptRequestedAt = time.Now()
		result.InterruptRequested = true
	}
	if req.MarkStallHint {
		wantTurnID := strings.TrimSpace(req.MarkStallHintForTurnID)
		if wantTurnID != "" && !strings.EqualFold(result.TurnID, wantTurnID) {
			return result
		}
		if !turn.StallHintLogged {
			turn.StallHintLogged = true
			result.StallHintLogged = true
			result.StallHintApplied = true
		}
	}

	if req.Finalize != nil {
		wantTurnID := strings.TrimSpace(req.Finalize.TurnID)
		result.ExpectedTurnID = wantTurnID
		if wantTurnID != "" && !strings.EqualFold(result.TurnID, wantTurnID) {
			result.TurnIDMismatch = true
		}

		delete(activeTurns, id)
		if turn.Timer != nil {
			turn.Timer.Stop()
		}
		if turn.StallTimer != nil {
			turn.StallTimer.Stop()
		}

		finalStatus := normalizeTrackedTurnStatus(req.Finalize.Status)
		if turn.InterruptRequested && finalStatus == "completed" {
			finalStatus = "interrupted"
		}
		if turn.Done != nil {
			select {
			case turn.Done <- finalStatus:
			default:
			}
		}
		reasonText := strings.TrimSpace(req.Finalize.Reason)
		result.Finalized = true
		result.FinalStatus = finalStatus
		result.FinalReason = reasonText
		result.Completion = map[string]any{
			"threadId": id,
			"turn": map[string]any{
				"id":     result.TurnID,
				"status": finalStatus,
			},
			"status": finalStatus,
			"reason": reasonText,
		}
	}

	return result
}

func (a *Adapter) withActiveTurn(threadID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	activeTurns, turnMu, _, _ := a.trackerState()
	id := strings.TrimSpace(threadID)
	if id == "" || turnMu == nil || fn == nil {
		return false
	}
	turnMu.Lock()
	defer turnMu.Unlock()
	if activeTurns == nil {
		return false
	}
	turn, ok := activeTurns[id]
	if !ok || turn == nil {
		return false
	}
	return fn(id, turn, activeTurns)
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
func (a *Adapter) beginTrackedTurn(threadID, turnID string) string {
	activeTurns, turnMu, watchdogTimeout, stallThreshold := a.trackerState()
	completeTrackedTurnByID := a.completeTrackedTurnByID
	notify := a.trackerNotify()
	checkTurnStall := a.checkTurnStall

	id := strings.TrimSpace(threadID)
	if id == "" {
		return ""
	}
	tid := strings.TrimSpace(turnID)
	if tid == "" {
		tid = fmt.Sprintf("turn-%d", time.Now().UnixMilli())
	}
	if turnMu == nil || activeTurns == nil {
		return tid
	}

	var superseded map[string]any
	var prevTurnID string
	var hadPrevTurn bool

	turnMu.Lock()
	a.ensureTurnTrackerStateLocked()
	superseded, prevTurnID, hadPrevTurn = supersedeActiveTurn(activeTurns, id, tid)

	now := time.Now()
	turn := &trackedTurn{
		ID:          tid,
		ThreadID:    id,
		StartedAt:   now,
		LastEventAt: now,
		Done:        make(chan string, 1),
	}

	watchdogTurnID := tid
	watchdogThreadID := id
	watchdogStartedAt := turn.StartedAt
	turn.Timer = time.AfterFunc(watchdogTimeout, func() {
		logger.Warn("turn tracker: watchdog timeout reached", append(threadLogFields(watchdogThreadID),
			logger.FieldTurnID, watchdogTurnID,
			"watchdog_timeout_ms", watchdogTimeout.Milliseconds(),
			"turn_age_ms", time.Since(watchdogStartedAt).Milliseconds(),
		)...)
		if notify == nil {
			return
		}
		if completion, ok := completeTrackedTurnByID(watchdogThreadID, watchdogTurnID, "failed", "watchdog_timeout"); ok {
			notify("turn/completed", completion)
		}
	})

	activeTurns[id] = turn
	stallInterval := max(stallThreshold/3, 10*time.Second)
	turn.StallTimer = time.AfterFunc(stallInterval, func() {
		checkTurnStall(id, tid)
	})
	turnMu.Unlock()

	logger.Info("turn tracker: begin turn tracking", append(threadLogFields(id),
		logger.FieldTurnID, tid,
		"source_turn_id", strings.TrimSpace(turnID),
		"watchdog_timeout_ms", watchdogTimeout.Milliseconds(),
		"had_prev_turn", hadPrevTurn,
		"prev_turn_id", prevTurnID,
	)...)

	if superseded != nil && notify != nil {
		notify("turn/completed", superseded)
	}
	return tid
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
	if timeout <= 0 {
		return "", false
	}
	var done chan string
	ok := a.withActiveTurn(threadID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		if turn.Done == nil {
			return false
		}
		done = turn.Done
		return true
	})
	if !ok || done == nil {
		return "", false
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case status := <-done:
		return normalizeTrackedTurnStatus(status), true
	case <-timer.C:
		return "", false
	}
}

// completeTrackedTurnByID closes a tracked turn and returns completion payload.
func (a *Adapter) completeTrackedTurnByID(threadID, turnID, status, reason string) (map[string]any, bool) {
	transition := a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{
		Finalize: &trackedTurnFinalizeRequest{
			TurnID: turnID,
			Status: status,
			Reason: reason,
		},
	})
	if !transition.Finalized || transition.Completion == nil {
		return nil, false
	}

	if transition.TurnIDMismatch {
		logger.Info("turn tracker: turn id mismatch, completing anyway to avoid stuck state", append(threadLogFields(transition.ThreadID),
			"active_turn_id", transition.TurnID,
			"event_turn_id", transition.ExpectedTurnID,
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
		)...)
	}

	logger.Info("turn tracker: turn completed", append(threadLogFields(transition.ThreadID),
		logger.FieldTurnID, transition.TurnID,
		logger.FieldStatus, transition.FinalStatus,
		"reason", transition.FinalReason,
		"duration_ms", time.Since(transition.StartedAt).Milliseconds(),
		"interrupt_requested", transition.InterruptRequested,
	)...)
	return transition.Completion, true
}

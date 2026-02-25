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
	watchdogTimeout := DefaultTurnWatchdogTimeout
	if state.TurnWatchdogTimeout != nil && *state.TurnWatchdogTimeout > 0 {
		watchdogTimeout = *state.TurnWatchdogTimeout
	}
	stallThreshold := defaultStallThreshold
	if state.stallThreshold != nil && *state.stallThreshold > 0 {
		stallThreshold = *state.stallThreshold
	}
	return activeTurns, turnMu, watchdogTimeout, stallThreshold
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
	return a.withActiveTurn(threadID, func(string, *trackedTurn, map[string]*trackedTurn) bool {
		return true
	})
}

// markTrackedTurnInterruptRequested marks interrupt intent on current tracked turn.
func (a *Adapter) markTrackedTurnInterruptRequested(threadID string) bool {
	return a.withActiveTurn(threadID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		turn.InterruptRequested = true
		turn.InterruptRequestedAt = time.Now()
		return true
	})
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
	wantTurnID := strings.TrimSpace(turnID)
	var completedTurn *trackedTurn
	var completedThreadID string
	var finalStatus string

	ok := a.withActiveTurn(threadID, func(id string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool {
		if wantTurnID != "" && !strings.EqualFold(strings.TrimSpace(turn.ID), wantTurnID) {
			logger.Info("turn tracker: turn id mismatch, completing anyway to avoid stuck state", append(threadLogFields(id),
				"active_turn_id", strings.TrimSpace(turn.ID),
				"event_turn_id", wantTurnID,
				logger.FieldStatus, strings.TrimSpace(status),
				"reason", strings.TrimSpace(reason),
			)...)
		}
		delete(activeTurns, id)
		if turn.Timer != nil {
			turn.Timer.Stop()
		}
		if turn.StallTimer != nil {
			turn.StallTimer.Stop()
		}
		finalStatus = normalizeTrackedTurnStatus(status)
		if turn.InterruptRequested && finalStatus == "completed" {
			finalStatus = "interrupted"
		}
		if turn.Done != nil {
			select {
			case turn.Done <- finalStatus:
			default:
			}
		}
		completedTurn = turn
		completedThreadID = id
		return true
	})
	if !ok || completedTurn == nil {
		return nil, false
	}

	reasonText := strings.TrimSpace(reason)
	payload := map[string]any{
		"threadId": completedThreadID,
		"turn": map[string]any{
			"id":     completedTurn.ID,
			"status": finalStatus,
		},
		"status": finalStatus,
		"reason": reasonText,
	}
	logger.Info("turn tracker: turn completed", append(threadLogFields(completedThreadID),
		logger.FieldTurnID, completedTurn.ID,
		logger.FieldStatus, finalStatus,
		"reason", reasonText,
		"duration_ms", time.Since(completedTurn.StartedAt).Milliseconds(),
		"interrupt_requested", completedTurn.InterruptRequested,
	)...)
	return payload, true
}

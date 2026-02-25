package codexadapter

import (
	"sync"
	"time"
)

const (
	DefaultTurnWatchdogTimeout        = 10 * time.Minute
	DefaultTrackedTurnSummaryTTL      = 30 * time.Minute
	TrackedTurnSummaryCacheMaxEntries = 512
	DefaultStallThreshold             = 480 * time.Second
	DefaultStallHeartbeat             = 300 * time.Second
)

// TrackedTurnSummaryCacheEntry caches the latest summary for one tracked turn.
type TrackedTurnSummaryCacheEntry struct {
	TurnID    string
	Summary   string
	UpdatedAt time.Time
}

// TurnTrackerState carries mutable turn-tracker state owned by the caller.
type TurnTrackerState struct {
	Mu                  *sync.Mutex
	ActiveTurns         *map[string]*TrackedTurn
	TurnWatchdogTimeout *time.Duration
	TurnSummaryCache    *map[string]TrackedTurnSummaryCacheEntry
	TurnSummaryTTL      *time.Duration
	StallThreshold      *time.Duration
	StallHeartbeat      *time.Duration
}

// EnsureTurnTrackerStateLocked initializes turn-tracker state and defaults.
func EnsureTurnTrackerStateLocked(state TurnTrackerState) {
	if state.ActiveTurns != nil && *state.ActiveTurns == nil {
		*state.ActiveTurns = make(map[string]*TrackedTurn)
	}
	if state.TurnWatchdogTimeout != nil && *state.TurnWatchdogTimeout <= 0 {
		*state.TurnWatchdogTimeout = DefaultTurnWatchdogTimeout
	}
	if state.TurnSummaryCache != nil && *state.TurnSummaryCache == nil {
		*state.TurnSummaryCache = make(map[string]TrackedTurnSummaryCacheEntry)
	}
	if state.TurnSummaryTTL != nil && *state.TurnSummaryTTL <= 0 {
		*state.TurnSummaryTTL = DefaultTrackedTurnSummaryTTL
	}
	if state.StallThreshold != nil && *state.StallThreshold <= 0 {
		*state.StallThreshold = DefaultStallThreshold
	}
	if state.StallHeartbeat != nil && *state.StallHeartbeat <= 0 {
		*state.StallHeartbeat = DefaultStallHeartbeat
	}
}

func (a *Adapter) trackerHelperState() TurnTrackerState {
	if a == nil {
		return TurnTrackerState{}
	}
	return a.tracker
}

// EnsureTurnTrackerStateLocked initializes tracker defaults using adapter-owned state.
func (a *Adapter) EnsureTurnTrackerStateLocked() {
	if a == nil {
		return
	}
	EnsureTurnTrackerStateLocked(a.trackerHelperState())
}

// LookupTrackedTurnSummary reads tracked-turn summary from adapter-owned cache state.
func (a *Adapter) LookupTrackedTurnSummary(threadID, turnID string) string {
	if a == nil {
		return ""
	}
	state := a.trackerHelperState()
	return LookupTrackedTurnSummary(state, state.Mu, threadID, turnID)
}

// TouchTrackedTurnLastEvent updates turn heartbeat using adapter-owned tracker state.
func (a *Adapter) TouchTrackedTurnLastEvent(threadID string) {
	activeTurns, turnMu, _, _ := a.trackerState()
	TouchTrackedTurnLastEvent(activeTurns, turnMu, threadID)
}

// StartApprovalStallHeartbeat starts approval heartbeat with adapter-owned tracker state.
func (a *Adapter) StartApprovalStallHeartbeat(threadID string) func() {
	_, _, _, stallThreshold := a.trackerState()
	return StartApprovalStallHeartbeat(threadID, stallThreshold, DefaultStallThreshold, a.TouchTrackedTurnLastEvent)
}

// StartDynamicToolStallHeartbeat starts heartbeat while dynamic tools execute.
func (a *Adapter) StartDynamicToolStallHeartbeat(threadID string) func() {
	return StartApprovalStallHeartbeat(threadID, a.StallThreshold(), DefaultStallThreshold, a.TouchTrackedTurnLastEvent)
}

// StallThreshold returns current tracker stall threshold.
func (a *Adapter) StallThreshold() time.Duration {
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	if state.StallThreshold != nil && *state.StallThreshold > 0 {
		return *state.StallThreshold
	}
	return DefaultStallThreshold
}

// SetStallThreshold updates tracker stall threshold.
func (a *Adapter) SetStallThreshold(threshold time.Duration) {
	if threshold <= 0 {
		return
	}
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	EnsureTurnTrackerStateLocked(state)
	if state.StallThreshold != nil {
		*state.StallThreshold = threshold
	}
}

// StallHeartbeat returns current configured stall heartbeat interval.
func (a *Adapter) StallHeartbeat() time.Duration {
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	if state.StallHeartbeat != nil && *state.StallHeartbeat > 0 {
		return *state.StallHeartbeat
	}
	return DefaultStallHeartbeat
}

// SetStallHeartbeat updates stall heartbeat interval.
func (a *Adapter) SetStallHeartbeat(interval time.Duration) {
	if interval <= 0 {
		return
	}
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	EnsureTurnTrackerStateLocked(state)
	if state.StallHeartbeat != nil {
		*state.StallHeartbeat = interval
	}
}

// PeekTrackedTurnMeta reads active-turn metadata from adapter-owned tracker state.
func (a *Adapter) PeekTrackedTurnMeta(threadID string) (string, time.Time, bool, bool) {
	activeTurns, turnMu, _, _ := a.trackerState()
	return PeekTrackedTurnMeta(activeTurns, turnMu, threadID)
}

// MarkTrackedTurnStallHint marks one-shot stall hint from adapter-owned tracker state.
func (a *Adapter) MarkTrackedTurnStallHint(threadID, turnID string) bool {
	activeTurns, turnMu, _, _ := a.trackerState()
	return MarkTrackedTurnStallHint(activeTurns, turnMu, threadID, turnID)
}

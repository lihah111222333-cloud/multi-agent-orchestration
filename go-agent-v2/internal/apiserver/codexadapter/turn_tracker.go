package codexadapter

import (
	"sync"
	"time"
)

// TrackedTurn is the turn lifecycle tracking state.
type TrackedTurn struct {
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

func (a *Adapter) trackerNotify() func(string, any) {
	if a != nil && a.ctx != nil {
		return a.ctx.Notify
	}
	return nil
}

func (a *Adapter) trackerState() (map[string]*TrackedTurn, *sync.Mutex, time.Duration, time.Duration) {
	if a == nil {
		return nil, nil, 0, 0
	}
	state := a.trackerHelperState()
	var activeTurns map[string]*TrackedTurn
	if state.ActiveTurns != nil {
		activeTurns = *state.ActiveTurns
	}
	turnMu := state.Mu
	watchdogTimeout := DefaultTurnWatchdogTimeout
	if state.TurnWatchdogTimeout != nil && *state.TurnWatchdogTimeout > 0 {
		watchdogTimeout = *state.TurnWatchdogTimeout
	}
	stallThreshold := DefaultStallThreshold
	if state.StallThreshold != nil && *state.StallThreshold > 0 {
		stallThreshold = *state.StallThreshold
	}
	return activeTurns, turnMu, watchdogTimeout, stallThreshold
}

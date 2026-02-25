package codexadapter

import (
	"fmt"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// peekTrackedTurnMeta returns current tracked turn metadata for one thread.
func (a *Adapter) peekTrackedTurnMeta(threadID string) (string, time.Time, bool, bool) {
	var turnID string
	var startedAt time.Time
	var interruptRequested bool
	ok := a.withActiveTurn(threadID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		turnID = strings.TrimSpace(turn.ID)
		startedAt = turn.StartedAt
		interruptRequested = turn.InterruptRequested
		return true
	})
	return turnID, startedAt, interruptRequested, ok
}

// markTrackedTurnStallHint marks one-shot stall hint flag.
func (a *Adapter) markTrackedTurnStallHint(threadID, turnID string) bool {
	wantTurnID := strings.TrimSpace(turnID)
	return a.withActiveTurn(threadID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		if wantTurnID != "" && !strings.EqualFold(strings.TrimSpace(turn.ID), wantTurnID) {
			return false
		}
		if turn.StallHintLogged {
			return false
		}
		turn.StallHintLogged = true
		return true
	})
}

func shouldLogTrackedTurnStallHint(eventType, method string, startedAt time.Time) bool {
	if isTerminalEventType(eventType, method) {
		return false
	}
	if startedAt.IsZero() {
		return false
	}
	return time.Since(startedAt) >= 30*time.Second
}

// touchTrackedTurnLastEvent updates turn heartbeat using adapter-owned tracker state.
func (a *Adapter) touchTrackedTurnLastEvent(threadID string) {
	a.withActiveTurn(threadID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		turn.LastEventAt = time.Now()
		return true
	})
}

// StartApprovalStallHeartbeat starts approval heartbeat with adapter-owned tracker state.
func (a *Adapter) StartApprovalStallHeartbeat(threadID string) func() {
	_, _, _, stallThreshold := a.trackerState()
	return startApprovalStallHeartbeat(threadID, stallThreshold, defaultStallThreshold, a.touchTrackedTurnLastEvent)
}

// StartDynamicToolStallHeartbeat starts heartbeat while dynamic tools execute.
func (a *Adapter) StartDynamicToolStallHeartbeat(threadID string) func() {
	return startApprovalStallHeartbeat(threadID, a.stallThreshold(), defaultStallThreshold, a.touchTrackedTurnLastEvent)
}

// stallThreshold returns current tracker stall threshold.
func (a *Adapter) stallThreshold() time.Duration {
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	if state.stallThreshold != nil && *state.stallThreshold > 0 {
		return *state.stallThreshold
	}
	return defaultStallThreshold
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
	ensureTurnTrackerStateLocked(state)
	if state.stallThreshold != nil {
		*state.stallThreshold = threshold
	}
}

// stallHeartbeat returns current configured stall heartbeat interval.
func (a *Adapter) stallHeartbeat() time.Duration {
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	if state.stallHeartbeat != nil && *state.stallHeartbeat > 0 {
		return *state.stallHeartbeat
	}
	return defaultStallHeartbeat
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
	ensureTurnTrackerStateLocked(state)
	if state.stallHeartbeat != nil {
		*state.stallHeartbeat = interval
	}
}

// checkTurnStall advances stall detection state machine.
func (a *Adapter) checkTurnStall(threadID string, turnID string) {
	activeTurns, turnMu, _, stallThreshold := a.trackerState()
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	if id == "" || tid == "" || turnMu == nil {
		return
	}

	turnMu.Lock()
	if activeTurns == nil {
		turnMu.Unlock()
		return
	}
	turn, ok := activeTurns[id]
	if !ok || turn == nil || !strings.EqualFold(strings.TrimSpace(turn.ID), tid) {
		turnMu.Unlock()
		return
	}

	silent := time.Since(turn.LastEventAt)
	threshold := stallThreshold
	if threshold <= 0 {
		threshold = defaultStallThreshold
	}

	if silent < threshold {
		rescheduleStallCheck(turn, id, turn.ID, silent, threshold, a.checkTurnStall)
		turnMu.Unlock()
		return
	}
	if turn.StallAutoInterrupted {
		turnMu.Unlock()
		return
	}
	if !turn.StallGraceStarted {
		turn.StallGraceStarted = true
		turnMu.Unlock()
		a.handleStallGracePeriod(id, turn.ID, silent, threshold)
		return
	}

	turn.StallAutoInterrupted = true
	turnMu.Unlock()
	a.executeStallAutoInterrupt(id, turn.ID, silent, threshold)
}

func (a *Adapter) handleStallGracePeriod(threadID, turnID string, silent, threshold time.Duration) {
	logger.Warn("turn tracker: stall detected (grace period)", append(threadLogFields(threadID),
		logger.FieldTurnID, turnID,
		"silent_ms", silent.Milliseconds(),
		"threshold_ms", threshold.Milliseconds(),
		"grace_ms", (30*time.Second).Milliseconds(),
	)...)

	var pushAlert func(threadID, category, message string)
	if runtime := a.uiRuntime(); runtime != nil {
		pushAlert = runtime.PushAlert
	}
	if pushAlert != nil {
		pushAlert(threadID, "stall_warning", "长时间无事件，若持续将自动中断")
	}

	activeTurns, turnMu, _, _ := a.trackerState()
	if turnMu == nil {
		return
	}
	turnMu.Lock()
	defer turnMu.Unlock()
	if activeTurns == nil {
		return
	}
	turn, ok := activeTurns[threadID]
	if !ok || turn == nil || !strings.EqualFold(strings.TrimSpace(turn.ID), strings.TrimSpace(turnID)) {
		return
	}
	turn.StallTimer = time.AfterFunc(30*time.Second, func() { a.checkTurnStall(threadID, turnID) })
}

// executeStallAutoInterrupt performs /interrupt and fallback completion when stalled.
func (a *Adapter) executeStallAutoInterrupt(
	threadID string,
	turnID string,
	silent time.Duration,
	threshold time.Duration,
) {
	if a == nil {
		return
	}

	var pushAlert func(threadID, category, message string)
	if runtime := a.uiRuntime(); runtime != nil {
		pushAlert = runtime.PushAlert
	}
	manager := a.manager()
	cancelCodeRuns := a.cancelCodeRuns
	completeTrackedTurnByID := a.completeTrackedTurnByID
	notify := a.trackerNotify()

	logger.Warn("turn tracker: thinking stall detected - auto interrupting", append(threadLogFields(threadID),
		logger.FieldTurnID, turnID,
		"silent_ms", silent.Milliseconds(),
		"threshold_ms", threshold.Milliseconds(),
	)...)

	if pushAlert != nil {
		pushAlert(threadID, "stall", fmt.Sprintf("思考超时 %ds 未响应，自动中断", int(silent.Seconds())))
	}

	util.SafeGo(func() {
		a.markTrackedTurnInterruptRequested(threadID)
		if cancelled := cancelCodeRuns(threadID); cancelled > 0 {
			logger.Info("turn tracker: cancelled running code_run executions", append(threadLogFields(threadID),
				logger.FieldTurnID, turnID,
				"cancelled_runs", cancelled,
			)...)
		}

		interrupted := false
		if manager != nil {
			if proc := manager.Get(threadID); proc != nil {
				if err := a.SendCommand(proc, "/interrupt", ""); err != nil {
					logger.Warn("turn tracker: stall auto-interrupt failed", append(threadLogFields(threadID),
						logger.FieldTurnID, turnID,
						logger.FieldError, err,
					)...)
				} else {
					interrupted = true
				}
			}
		}
		if !interrupted && notify != nil {
			if completion, ok := completeTrackedTurnByID(threadID, turnID, "failed", "thinking_stall_timeout"); ok {
				notify("turn/completed", completion)
			}
		}
	})
}

func approvalstallHeartbeatInterval(stallThreshold, fallback time.Duration) time.Duration {
	base := stallThreshold
	if base <= 0 {
		base = fallback
	}
	if base <= 0 {
		base = defaultStallThreshold
	}
	interval := base / 3
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	return interval
}

func startApprovalStallHeartbeat(threadID string, stallThreshold, fallback time.Duration, touch func(string)) func() {
	id := strings.TrimSpace(threadID)
	interval := approvalstallHeartbeatInterval(stallThreshold, fallback)
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

func rescheduleStallCheck(turn *trackedTurn, threadID, turnID string, silent, threshold time.Duration, check func(string, string)) {
	if turn == nil || check == nil {
		return
	}
	remaining := threshold - silent
	if remaining <= 0 {
		remaining = 10 * time.Second
	}
	next := max(remaining/2, 10*time.Second)
	turn.StallTimer = time.AfterFunc(next, func() { check(threadID, turnID) })
}

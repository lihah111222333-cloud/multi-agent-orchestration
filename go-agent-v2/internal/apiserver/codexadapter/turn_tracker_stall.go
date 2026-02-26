package codexadapter

import (
	"fmt"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type trackedTurnStallAction int

const (
	trackedTurnStallNoop trackedTurnStallAction = iota
	trackedTurnStallRescheduled
	trackedTurnStallEnterGrace
	trackedTurnStallAutoInterrupt
)

type trackedTurnStallDecision struct {
	Action    trackedTurnStallAction
	ThreadID  string
	TurnID    string
	Silent    time.Duration
	Threshold time.Duration
}

// peekTrackedTurnMeta returns current tracked turn metadata for one thread.
func (a *Adapter) peekTrackedTurnMeta(threadID string) (string, time.Time, bool, bool) {
	state := a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{})
	if !state.Found {
		return "", time.Time{}, false, false
	}
	return state.TurnID, state.StartedAt, state.InterruptRequested, true
}

// markTrackedTurnStallHint marks one-shot stall hint flag.
func (a *Adapter) markTrackedTurnStallHint(threadID, turnID string) bool {
	state := a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{
		MarkStallHint:          true,
		MarkStallHintForTurnID: strings.TrimSpace(turnID),
	})
	return state.StallHintApplied
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
	a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{TouchHeartbeat: true})
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
	return a.trackerDuration(func(state turnTrackerState) *time.Duration {
		return state.stallThreshold
	}, defaultStallThreshold)
}

// SetStallThreshold updates tracker stall threshold.
func (a *Adapter) SetStallThreshold(threshold time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration {
		return state.stallThreshold
	}, threshold)
}

// stallHeartbeat returns current configured stall heartbeat interval.
func (a *Adapter) stallHeartbeat() time.Duration {
	return a.trackerDuration(func(state turnTrackerState) *time.Duration {
		return state.stallHeartbeat
	}, defaultStallHeartbeat)
}

// SetStallHeartbeat updates stall heartbeat interval.
func (a *Adapter) SetStallHeartbeat(interval time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration {
		return state.stallHeartbeat
	}, interval)
}

func (a *Adapter) nextTrackedTurnStallDecision(threadID, turnID string, stallThreshold time.Duration) trackedTurnStallDecision {
	decision := trackedTurnStallDecision{Action: trackedTurnStallNoop}
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	if id == "" || tid == "" {
		return decision
	}

	threshold := stallThreshold
	if threshold <= 0 {
		threshold = defaultStallThreshold
	}

	a.withActiveTurnByID(id, tid, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		currentTurnID := strings.TrimSpace(turn.ID)
		silent := time.Since(turn.LastEventAt)
		decision.ThreadID = id
		decision.TurnID = currentTurnID
		decision.Silent = silent
		decision.Threshold = threshold

		if silent < threshold {
			rescheduleStallCheck(turn, id, currentTurnID, silent, threshold, a.checkTurnStall)
			decision.Action = trackedTurnStallRescheduled
			return true
		}
		if turn.StallAutoInterrupted {
			return true
		}
		if !turn.StallGraceStarted {
			turn.StallGraceStarted = true
			decision.Action = trackedTurnStallEnterGrace
			return true
		}

		turn.StallAutoInterrupted = true
		decision.Action = trackedTurnStallAutoInterrupt
		return true
	})

	return decision
}

// checkTurnStall advances stall detection state machine.
func (a *Adapter) checkTurnStall(threadID string, turnID string) {
	_, _, _, stallThreshold := a.trackerState()
	decision := a.nextTrackedTurnStallDecision(threadID, turnID, stallThreshold)
	switch decision.Action {
	case trackedTurnStallRescheduled, trackedTurnStallNoop:
		return
	case trackedTurnStallEnterGrace:
		a.handleStallGracePeriod(decision.ThreadID, decision.TurnID, decision.Silent, decision.Threshold)
	case trackedTurnStallAutoInterrupt:
		a.executeStallAutoInterrupt(decision.ThreadID, decision.TurnID, decision.Silent, decision.Threshold)
	}
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

	a.withActiveTurnByID(threadID, turnID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		turn.StallTimer = time.AfterFunc(30*time.Second, func() { a.checkTurnStall(threadID, turnID) })
		return true
	})
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

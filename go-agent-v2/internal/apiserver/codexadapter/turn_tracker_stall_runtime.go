package codexadapter

import (
	"fmt"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// CheckTurnStall advances stall detection state machine.
func (a *Adapter) CheckTurnStall(threadID string, turnID string) {
	activeTurns, turnMu, _, stallThreshold := a.trackerState()
	defaultStallThreshold := DefaultStallThreshold
	if turnMu == nil {
		return
	}
	turnMu.Lock()
	if activeTurns == nil {
		turnMu.Unlock()
		return
	}
	turn, ok := activeTurns[threadID]
	if !ok || turn == nil || turn.ID != turnID {
		turnMu.Unlock()
		return
	}

	silent := time.Since(turn.LastEventAt)
	threshold := stallThreshold
	if threshold <= 0 {
		threshold = defaultStallThreshold
	}

	// Not stalled yet — reschedule and check again.
	if silent < threshold {
		RescheduleStallCheck(turn, threadID, turnID, silent, threshold, a.CheckTurnStall)
		turnMu.Unlock()
		return
	}

	// Already auto-interrupted — nothing to do.
	if turn.StallAutoInterrupted {
		turnMu.Unlock()
		return
	}

	// Grace period: first detection → warn + reschedule.
	if !turn.StallGraceStarted {
		turn.StallGraceStarted = true
		turnMu.Unlock()
		a.handleStallGracePeriod(threadID, turnID, silent, threshold)
		return
	}

	// Second detection (after grace period) → actually interrupt.
	turn.StallAutoInterrupted = true
	turnMu.Unlock()
	a.ExecuteStallAutoInterrupt(threadID, turnID, silent, threshold)
}

func (a *Adapter) handleStallGracePeriod(threadID, turnID string, silent, threshold time.Duration) {
	activeTurns, turnMu, _, _ := a.trackerState()
	if turnMu == nil {
		return
	}
	var pushAlert func(threadID, category, message string)
	if a != nil && a.ctx != nil && a.ctx.UIRuntime != nil {
		pushAlert = a.ctx.UIRuntime.PushAlert
	}
	HandleStallGracePeriod(
		activeTurns,
		turnMu,
		threadID,
		turnID,
		silent,
		threshold,
		30*time.Second,
		pushAlert,
		a.CheckTurnStall,
	)
}

// ExecuteStallAutoInterrupt performs /interrupt and fallback completion when stalled.
func (a *Adapter) ExecuteStallAutoInterrupt(
	threadID string,
	turnID string,
	silent time.Duration,
	threshold time.Duration,
) {
	var pushAlert func(threadID, category, message string)
	var manager *runner.AgentManager
	if a != nil && a.ctx != nil {
		if runtime := a.ctx.UIRuntime; runtime != nil {
			pushAlert = runtime.PushAlert
		}
		manager = a.ctx.Manager
	}
	cancelCodeRuns := a.cancelCodeRuns
	completeTrackedTurnByID := a.CompleteTrackedTurnByID
	notify := a.trackerNotify()

	logger.Warn("turn tracker: thinking stall detected — auto interrupting",
		logger.FieldThreadID, threadID,
		logger.FieldTurnID, turnID,
		"silent_ms", silent.Milliseconds(),
		"threshold_ms", threshold.Milliseconds(),
	)

	if pushAlert != nil {
		pushAlert(threadID, "stall",
			fmt.Sprintf("思考超时 %ds 未响应，自动中断", int(silent.Seconds())))
	}

	util.SafeGo(func() {
		a.MarkTrackedTurnInterruptRequested(threadID)
		if cancelled := cancelCodeRuns(threadID); cancelled > 0 {
			logger.Info("turn tracker: cancelled running code_run executions",
				logger.FieldThreadID, threadID,
				logger.FieldTurnID, turnID,
				"cancelled_runs", cancelled,
			)
		}
		interrupted := false
		if manager != nil {
			if proc := manager.Get(threadID); proc != nil {
				if err := a.SendCommand(proc, "/interrupt", ""); err != nil {
					logger.Warn("turn tracker: stall auto-interrupt failed",
						logger.FieldThreadID, threadID,
						logger.FieldTurnID, turnID,
						logger.FieldError, err,
					)
				} else {
					interrupted = true
				}
			}
		}
		// Fallback: if /interrupt failed or process is gone, force-complete the tracker.
		if !interrupted && notify != nil {
			if completion, ok := completeTrackedTurnByID(threadID, turnID, "failed", "thinking_stall_timeout"); ok {
				notify("turn/completed", completion)
			}
		}
	})
}

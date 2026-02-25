package codexadapter

import (
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// TurnInterrupt executes /interrupt using constructor-injected dependencies.
func (a *Adapter) TurnInterrupt(threadID string) (any, error) {
	start := time.Now()
	beforeState := a.ReadThreadRuntimeState(threadID)
	activeTrackedBefore := a.HasActiveTrackedTurn(threadID)
	activeBefore := IsInterruptActiveState(beforeState)
	logger.Info("turn/interrupt: request",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		"state_before", beforeState,
		"active_before", activeBefore,
		"active_tracked_before", activeTrackedBefore,
	)
	if cancelled := a.cancelCodeRuns(threadID); cancelled > 0 {
		logger.Info("turn/interrupt: cancelled running code_run executions",
			logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
			"cancelled_runs", cancelled,
		)
	}
	return withProcess(a, "Server.turnInterrupt", threadID, func(proc *runner.AgentProcess) (any, error) {
		noActiveTurn, err := a.sendInterruptCommand(proc)
		if err != nil {
			logger.Warn("turn/interrupt: send command failed",
				logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
				logger.FieldError, err,
				logger.FieldDurationMS, time.Since(start).Milliseconds(),
			)
			return nil, err
		}
		if noActiveTurn {
			if activeBefore || activeTrackedBefore {
				a.notifyTurnCompleted(threadID, "completed", "interrupt_no_active_turn")
			}
			logger.Info("turn/interrupt: no active turn",
				logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
				"state_before", beforeState,
				logger.FieldDurationMS, time.Since(start).Milliseconds(),
			)
			return map[string]any{
				"confirmed":     false,
				"mode":          "no_active_turn",
				"interruptSent": false,
				"stateBefore":   beforeState,
				"stateAfter":    beforeState,
			}, nil
		}
		logger.Info("turn/interrupt: command sent",
			logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		a.MarkTrackedTurnInterruptRequested(threadID)
		confirmed, afterState, waitedMS, observedActive := a.WaitInterruptOutcome(
			threadID,
			6*time.Second,
			activeBefore || activeTrackedBefore,
		)
		mode := InterruptSettleMode(confirmed, afterState)
		if !observedActive {
			confirmed = false
			mode = "no_active_turn"
		}
		logger.Info("turn/interrupt: settle",
			logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
			"confirmed", confirmed,
			"mode", mode,
			"active_observed", observedActive,
			"state_before", beforeState,
			"state_after", afterState,
			"waited_ms", waitedMS,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return map[string]any{
			"confirmed":      confirmed,
			"mode":           mode,
			"interruptSent":  true,
			"stateBefore":    beforeState,
			"stateAfter":     afterState,
			"waitedMs":       waitedMS,
			"activeObserved": observedActive,
		}, nil
	})
}

// TurnForceComplete forcibly finalizes current turn state using constructor-injected dependencies.
func (a *Adapter) TurnForceComplete(threadID string) (any, error) {
	logger.Info("turn/forceComplete: request",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
	)
	if cancelled := a.cancelCodeRuns(threadID); cancelled > 0 {
		logger.Info("turn/forceComplete: cancelled running code_run executions",
			logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
			"cancelled_runs", cancelled,
		)
	}
	return withProcess(a, "Server.turnForceComplete", threadID, func(proc *runner.AgentProcess) (any, error) {
		noActiveTurn, err := a.sendInterruptCommand(proc)
		if err != nil {
			logger.Warn("turn/forceComplete: interrupt failed (best-effort)",
				logger.FieldAgentID, threadID, logger.FieldError, err)
		} else if noActiveTurn {
			logger.Info("turn/forceComplete: no active turn (best-effort)",
				logger.FieldAgentID, threadID)
		}

		a.notifyTurnCompleted(threadID, "completed", "force_complete")

		return map[string]any{
			"confirmed":      true,
			"forceCompleted": true,
		}, nil
	})
}

package codexadapter

import (
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// normalizeInterruptState normalizes runtime status names used by interrupt flow.
func normalizeInterruptState(raw string) string {
	state := strings.ToLower(strings.TrimSpace(raw))
	if state == "" {
		return "idle"
	}
	switch state {
	case "completed", "complete", "done", "success", "succeeded", "ready", "stopped", "ended", "closed":
		return "idle"
	case "failed", "fail":
		return "error"
	default:
		return state
	}
}

// isInterruptNoActiveTurnError reports whether interrupt failure means no active turn.
func isInterruptNoActiveTurnError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no active turn") ||
		strings.Contains(message, "nothing to interrupt") ||
		strings.Contains(message, "not interruptible")
}

func isInterruptTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded")
}

// isInterruptActiveState reports whether current state is still active.
func isInterruptActiveState(state string) bool {
	s := normalizeInterruptState(state)
	switch s {
	case "inprogress", "in_progress", "running", "streaming", "thinking", "starting", "responding", "editing", "waiting", "syncing":
		return true
	default:
		return false
	}
}

// interruptSettleMode classifies interrupt settle outcome.
func interruptSettleMode(confirmed bool, afterState string) string {
	if confirmed {
		return "interrupt_confirmed"
	}
	switch normalizeInterruptState(afterState) {
	case "error":
		return "interrupt_terminal_failed"
	case "idle":
		return "interrupt_terminal_completed"
	default:
		return "interrupt_timeout"
	}
}

// readThreadRuntimeStateByHooks returns normalized thread state via injected runtime/status hooks.
func readThreadRuntimeStateByHooks(threadID string, readRuntimeStatus func(string) string, hasActiveTrackedTurn func(string) bool) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	if readRuntimeStatus == nil {
		if hasActiveTrackedTurn != nil && hasActiveTrackedTurn(id) {
			return "running"
		}
		return ""
	}
	state := normalizeInterruptState(readRuntimeStatus(id))
	if state == "idle" && hasActiveTrackedTurn != nil && hasActiveTrackedTurn(id) {
		return "running"
	}
	return state
}

// waitInterruptOutcome waits until interrupt settles based on tracker/runtime state.
func waitInterruptOutcome(
	threadID string,
	timeout time.Duration,
	activeHint bool,
	waitTrackedTurnTerminal func(string, time.Duration) (string, bool),
	readThreadRuntimeState func(string) string,
) (bool, string, int64, bool) {
	start := time.Now()
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false, "idle", 0, false
	}
	observedActive := activeHint
	if waitTrackedTurnTerminal != nil {
		if terminalStatus, ok := waitTrackedTurnTerminal(id, timeout); ok {
			afterState := normalizeInterruptState(terminalStatus)
			confirmed := strings.EqualFold(terminalStatus, "interrupted")
			return confirmed, afterState, time.Since(start).Milliseconds(), true
		}
	}
	if readThreadRuntimeState == nil {
		return false, "", time.Since(start).Milliseconds(), observedActive
	}
	deadline := start.Add(timeout)
	lastState := readThreadRuntimeState(id)
	if isInterruptActiveState(lastState) {
		observedActive = true
	}
	for {
		if !isInterruptActiveState(lastState) {
			if !observedActive {
				return false, lastState, time.Since(start).Milliseconds(), false
			}
			return true, lastState, time.Since(start).Milliseconds(), true
		}
		observedActive = true
		if time.Now().After(deadline) {
			return false, lastState, time.Since(start).Milliseconds(), true
		}
		time.Sleep(120 * time.Millisecond)
		lastState = readThreadRuntimeState(id)
	}
}

func sendInterruptCommand(proc *runner.AgentProcess, sendCommand func(*runner.AgentProcess, string, string) error) (bool, error) {
	if sendCommand == nil {
		return false, apperrors.New("Server.turnInterrupt", "interrupt sender is not initialized")
	}
	if err := sendCommand(proc, "/interrupt", ""); err != nil {
		if isInterruptNoActiveTurnError(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func notifyTurnCompleted(
	threadID string,
	status string,
	reason string,
	completeTrackedTurnByID func(string, string, string, string) (map[string]any, bool),
	notify func(string, any),
) {
	if notify == nil {
		return
	}
	if completeTrackedTurnByID != nil {
		if completion, ok := completeTrackedTurnByID(threadID, "", status, reason); ok {
			notify("turn/completed", completion)
			return
		}
	}
	notify("turn/completed", map[string]any{
		"threadId": threadID,
		"status":   status,
		"reason":   reason,
	})
}

func turnInterrupt(
	threadID string,
	readThreadRuntimeState func(string) string,
	hasActiveTrackedTurn func(string) bool,
	cancelCodeRuns func(string) int,
	sendInterrupt func(*runner.AgentProcess) (bool, error),
	withProcess func(string, string, func(*runner.AgentProcess) (any, error)) (any, error),
	markTrackedTurnInterruptRequested func(string) bool,
	waitOutcome func(string, time.Duration, bool) (bool, string, int64, bool),
	notifyTurnCompleted func(string, string, string),
) (any, error) {
	id, err := requireThreadID("Server.turnInterrupt", threadID)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	beforeState := "idle"
	if readThreadRuntimeState != nil {
		beforeState = readThreadRuntimeState(id)
	}
	activeTrackedBefore := hasActiveTrackedTurn != nil && hasActiveTrackedTurn(id)
	activeBefore := isInterruptActiveState(beforeState)
	logger.Info("turn/interrupt: request",
		append(threadLogFields(id),
			"state_before", beforeState,
			"active_before", activeBefore,
			"active_tracked_before", activeTrackedBefore,
		)...,
	)
	if cancelCodeRuns != nil {
		if cancelled := cancelCodeRuns(id); cancelled > 0 {
			logger.Info("turn/interrupt: cancelled running code_run executions",
				append(threadLogFields(id), "cancelled_runs", cancelled)...,
			)
		}
	}
	if withProcess == nil {
		return nil, apperrors.New("Server.turnInterrupt", "thread process resolver is not initialized")
	}
	return withProcess("Server.turnInterrupt", id, func(proc *runner.AgentProcess) (any, error) {
		if sendInterrupt == nil {
			return nil, apperrors.New("Server.turnInterrupt", "interrupt sender is not initialized")
		}
		noActiveTurn, err := sendInterrupt(proc)
		if err != nil {
			if !activeBefore && !activeTrackedBefore && isInterruptTimeoutError(err) {
				logger.Info("turn/interrupt: timeout with no active state, treating as no active turn",
					append(threadLogFields(id),
						"state_before", beforeState,
						logger.FieldError, err,
						logger.FieldDurationMS, time.Since(start).Milliseconds(),
					)...,
				)
				return map[string]any{
					"confirmed":     false,
					"mode":          "no_active_turn",
					"interruptSent": false,
					"stateBefore":   beforeState,
					"stateAfter":    beforeState,
				}, nil
			}
			logger.Warn("turn/interrupt: send command failed",
				append(threadLogFields(id),
					logger.FieldError, err,
					logger.FieldDurationMS, time.Since(start).Milliseconds(),
				)...,
			)
			return nil, err
		}

		if noActiveTurn {
			if (activeBefore || activeTrackedBefore) && notifyTurnCompleted != nil {
				notifyTurnCompleted(id, "completed", "interrupt_no_active_turn")
			}
			logger.Info("turn/interrupt: no active turn",
				append(threadLogFields(id),
					"state_before", beforeState,
					logger.FieldDurationMS, time.Since(start).Milliseconds(),
				)...,
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
			append(threadLogFields(id), logger.FieldDurationMS, time.Since(start).Milliseconds())...,
		)
		if markTrackedTurnInterruptRequested != nil {
			markTrackedTurnInterruptRequested(id)
		}

		confirmed := false
		afterState := beforeState
		waitedMS := int64(0)
		observedActive := activeBefore || activeTrackedBefore
		if waitOutcome != nil {
			confirmed, afterState, waitedMS, observedActive = waitOutcome(id, 6*time.Second, activeBefore || activeTrackedBefore)
		}
		mode := interruptSettleMode(confirmed, afterState)
		if !observedActive {
			confirmed = false
			mode = "no_active_turn"
		}
		logger.Info("turn/interrupt: settle",
			append(threadLogFields(id),
				"confirmed", confirmed,
				"mode", mode,
				"active_observed", observedActive,
				"state_before", beforeState,
				"state_after", afterState,
				"waited_ms", waitedMS,
				logger.FieldDurationMS, time.Since(start).Milliseconds(),
			)...,
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

func turnForceComplete(
	threadID string,
	cancelCodeRuns func(string) int,
	sendInterrupt func(*runner.AgentProcess) (bool, error),
	notifyTurnCompleted func(string, string, string),
	withProcess func(string, string, func(*runner.AgentProcess) (any, error)) (any, error),
) (any, error) {
	id, err := requireThreadID("Server.turnForceComplete", threadID)
	if err != nil {
		return nil, err
	}
	logger.Info("turn/forceComplete: request", threadLogFields(id)...)
	if cancelCodeRuns != nil {
		if cancelled := cancelCodeRuns(id); cancelled > 0 {
			logger.Info("turn/forceComplete: cancelled running code_run executions",
				append(threadLogFields(id), "cancelled_runs", cancelled)...,
			)
		}
	}
	if withProcess == nil {
		return nil, apperrors.New("Server.turnForceComplete", "thread process resolver is not initialized")
	}
	return withProcess("Server.turnForceComplete", id, func(proc *runner.AgentProcess) (any, error) {
		if sendInterrupt != nil {
			noActiveTurn, err := sendInterrupt(proc)
			if err != nil {
				logger.Warn("turn/forceComplete: interrupt failed (best-effort)",
					append(threadLogFields(id), logger.FieldError, err)...,
				)
			} else if noActiveTurn {
				logger.Info("turn/forceComplete: no active turn (best-effort)", threadLogFields(id)...)
			}
		}
		if notifyTurnCompleted != nil {
			notifyTurnCompleted(id, "completed", "force_complete")
		}
		return map[string]any{"confirmed": true, "forceCompleted": true}, nil
	})
}

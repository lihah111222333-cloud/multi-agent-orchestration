package codexadapter

import (
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// resolveClientActiveTurnID extracts active turn ID if client supports it.
func resolveClientActiveTurnID(client agentcore.Client) string {
	if client == nil {
		return ""
	}
	reader, ok := client.(interface{ GetActiveTurnID() string })
	if !ok {
		return ""
	}
	return strings.TrimSpace(reader.GetActiveTurnID())
}

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

// readThreadRuntimeState reads normalized runtime state using adapter-owned tracker/runtime state.
func (a *Adapter) readThreadRuntimeState(threadID string) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	return readThreadRuntimeStateByHooks(id, a.readRuntimeStatus, a.hasActiveTrackedTurn)
}

func (a *Adapter) readRuntimeStatus(threadID string) string {
	uiRuntime := a.uiRuntime()
	if uiRuntime == nil {
		return ""
	}
	snapshot := uiRuntime.Snapshot()
	return snapshot.Statuses[threadID]
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

// waitInterruptOutcome waits for terminal state using adapter-owned tracker/runtime state.
func (a *Adapter) waitInterruptOutcome(threadID string, timeout time.Duration, activeHint bool) (bool, string, int64, bool) {
	return waitInterruptOutcome(threadID, timeout, activeHint, a.waitTrackedTurnTerminal, a.readThreadRuntimeState)
}

func (a *Adapter) sendInterruptCommand(proc *runner.AgentProcess) (bool, error) {
	if err := a.SendCommand(proc, "/interrupt", ""); err != nil {
		if isInterruptNoActiveTurnError(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func (a *Adapter) notifyTurnCompleted(threadID, status, reason string) {
	if completion, ok := a.completeTrackedTurnByID(threadID, "", status, reason); ok {
		a.notify("turn/completed", completion)
		return
	}
	a.notify("turn/completed", map[string]any{
		"threadId": threadID,
		"status":   status,
		"reason":   reason,
	})
}

// TurnInterrupt executes /interrupt using constructor-injected dependencies.
func (a *Adapter) TurnInterrupt(threadID string) (any, error) {
	threadID, err := requireThreadID("Server.turnInterrupt", threadID)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	beforeState := a.readThreadRuntimeState(threadID)
	activeTrackedBefore := a.hasActiveTrackedTurn(threadID)
	activeBefore := isInterruptActiveState(beforeState)
	logger.Info("turn/interrupt: request",
		append(threadLogFields(threadID),
			"state_before", beforeState,
			"active_before", activeBefore,
			"active_tracked_before", activeTrackedBefore,
		)...,
	)
	if cancelled := a.cancelCodeRuns(threadID); cancelled > 0 {
		logger.Info("turn/interrupt: cancelled running code_run executions",
			append(threadLogFields(threadID),
				"cancelled_runs", cancelled,
			)...,
		)
	}
	return withProcess(a, "Server.turnInterrupt", threadID, func(proc *runner.AgentProcess) (any, error) {
		noActiveTurn, err := a.sendInterruptCommand(proc)
		if err != nil {
			if !activeBefore && !activeTrackedBefore && isInterruptTimeoutError(err) {
				logger.Info("turn/interrupt: timeout with no active state, treating as no active turn",
					append(threadLogFields(threadID),
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
				append(threadLogFields(threadID),
					logger.FieldError, err,
					logger.FieldDurationMS, time.Since(start).Milliseconds(),
				)...,
			)
			return nil, err
		}
		if noActiveTurn {
			if activeBefore || activeTrackedBefore {
				a.notifyTurnCompleted(threadID, "completed", "interrupt_no_active_turn")
			}
			logger.Info("turn/interrupt: no active turn",
				append(threadLogFields(threadID),
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
			append(threadLogFields(threadID),
				logger.FieldDurationMS, time.Since(start).Milliseconds(),
			)...,
		)
		a.markTrackedTurnInterruptRequested(threadID)
		confirmed, afterState, waitedMS, observedActive := a.waitInterruptOutcome(
			threadID,
			6*time.Second,
			activeBefore || activeTrackedBefore,
		)
		mode := interruptSettleMode(confirmed, afterState)
		if !observedActive {
			confirmed = false
			mode = "no_active_turn"
		}
		logger.Info("turn/interrupt: settle",
			append(threadLogFields(threadID),
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

// TurnForceComplete forcibly finalizes current turn state using constructor-injected dependencies.
func (a *Adapter) TurnForceComplete(threadID string) (any, error) {
	threadID, err := requireThreadID("Server.turnForceComplete", threadID)
	if err != nil {
		return nil, err
	}
	logger.Info("turn/forceComplete: request", threadLogFields(threadID)...)
	if cancelled := a.cancelCodeRuns(threadID); cancelled > 0 {
		logger.Info("turn/forceComplete: cancelled running code_run executions",
			append(threadLogFields(threadID),
				"cancelled_runs", cancelled,
			)...,
		)
	}
	return withProcess(a, "Server.turnForceComplete", threadID, func(proc *runner.AgentProcess) (any, error) {
		noActiveTurn, err := a.sendInterruptCommand(proc)
		if err != nil {
			logger.Warn("turn/forceComplete: interrupt failed (best-effort)",
				append(threadLogFields(threadID), logger.FieldError, err)...,
			)
		} else if noActiveTurn {
			logger.Info("turn/forceComplete: no active turn (best-effort)", threadLogFields(threadID)...)
		}

		a.notifyTurnCompleted(threadID, "completed", "force_complete")

		return map[string]any{
			"confirmed":      true,
			"forceCompleted": true,
		}, nil
	})
}

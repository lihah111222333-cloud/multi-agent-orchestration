package interrupt

import (
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/support"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

var (
	normalizeInterruptState      = support.NormalizeInterruptState
	isInterruptNoActiveTurnError = support.IsInterruptNoActiveTurnError
	isInterruptTimeoutError      = support.IsInterruptTimeoutError
	isInterruptActiveState       = support.IsInterruptActiveState
	interruptSettleMode          = support.InterruptSettleMode
)

func readThreadRuntimeStateByHooks(threadID string, readRuntimeStatus func(string) string, hasActiveTrackedTurn func(string) bool) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	activeTracked := hasActiveTrackedTurn != nil && hasActiveTrackedTurn(id)
	if readRuntimeStatus == nil {
		if activeTracked {
			return "running"
		}
		return ""
	}
	state := normalizeInterruptState(readRuntimeStatus(id))
	if state == "idle" && activeTracked {
		return "running"
	}
	return state
}

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
			return strings.EqualFold(terminalStatus, "interrupted"), afterState, time.Since(start).Milliseconds(), true
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
			return observedActive, lastState, time.Since(start).Milliseconds(), observedActive
		}
		observedActive = true
		if time.Now().After(deadline) {
			return false, lastState, time.Since(start).Milliseconds(), true
		}
		time.Sleep(120 * time.Millisecond)
		lastState = readThreadRuntimeState(id)
	}
}

func sendInterruptCommand(proc any, sendCommand func(any, string, string) error) (bool, error) {
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

func interruptPayload(confirmed bool, mode string, sent bool, beforeState, afterState string, waitedMS int64, activeObserved bool) map[string]any {
	payload := map[string]any{
		"confirmed":     confirmed,
		"mode":          mode,
		"interruptSent": sent,
		"stateBefore":   beforeState,
		"stateAfter":    afterState,
	}
	if sent {
		payload["waitedMs"] = waitedMS
		payload["activeObserved"] = activeObserved
	}
	return payload
}

func turnInterrupt(
	threadID string,
	readThreadRuntimeState func(string) string,
	hasActiveTrackedTurn func(string) bool,
	cancelCodeRuns func(string) int,
	sendInterrupt func(any) (bool, error),
	withProcess func(string, string, func(any) (any, error)) (any, error),
	markTrackedTurnInterruptRequested func(string) bool,
	waitOutcome func(string, time.Duration, bool) (bool, string, int64, bool),
	notifyTurnCompleted func(string, string, string),
) (any, error) {
	id, err := common.RequireThreadID("Server.turnInterrupt", threadID)
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
	fields := func(extra ...any) []any { return append(common.ThreadLogFields(id), extra...) }
	logger.Info("turn/interrupt: request", fields(
		"state_before", beforeState,
		"active_before", activeBefore,
		"active_tracked_before", activeTrackedBefore,
	)...)
	if cancelCodeRuns != nil {
		if cancelled := cancelCodeRuns(id); cancelled > 0 {
			logger.Info("turn/interrupt: cancelled running code_run executions", fields("cancelled_runs", cancelled)...)
		}
	}
	if withProcess == nil {
		return nil, apperrors.New("Server.turnInterrupt", "thread process resolver is not initialized")
	}
	return withProcess("Server.turnInterrupt", id, func(proc any) (any, error) {
		if sendInterrupt == nil {
			return nil, apperrors.New("Server.turnInterrupt", "interrupt sender is not initialized")
		}
		noActiveTurn, err := sendInterrupt(proc)
		if err != nil {
			durationMS := time.Since(start).Milliseconds()
			if !activeBefore && !activeTrackedBefore && isInterruptTimeoutError(err) {
				logger.Info("turn/interrupt: timeout with no active state, treating as no active turn",
					fields("state_before", beforeState, logger.FieldError, err, logger.FieldDurationMS, durationMS)...,
				)
				return interruptPayload(false, "no_active_turn", false, beforeState, beforeState, 0, false), nil
			}
			logger.Warn("turn/interrupt: send command failed", fields(logger.FieldError, err, logger.FieldDurationMS, durationMS)...)
			return nil, err
		}

		if noActiveTurn {
			if (activeBefore || activeTrackedBefore) && notifyTurnCompleted != nil {
				notifyTurnCompleted(id, "completed", "interrupt_no_active_turn")
			}
			logger.Info("turn/interrupt: no active turn",
				fields("state_before", beforeState, logger.FieldDurationMS, time.Since(start).Milliseconds())...,
			)
			return interruptPayload(false, "no_active_turn", false, beforeState, beforeState, 0, false), nil
		}

		logger.Info("turn/interrupt: command sent", fields(logger.FieldDurationMS, time.Since(start).Milliseconds())...)
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
			confirmed, mode = false, "no_active_turn"
		}
		logger.Info("turn/interrupt: settle", fields(
			"confirmed", confirmed,
			"mode", mode,
			"active_observed", observedActive,
			"state_before", beforeState,
			"state_after", afterState,
			"waited_ms", waitedMS,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)...)
		return interruptPayload(confirmed, mode, true, beforeState, afterState, waitedMS, observedActive), nil
	})
}

func turnForceComplete(
	threadID string,
	cancelCodeRuns func(string) int,
	sendInterrupt func(any) (bool, error),
	notifyTurnCompleted func(string, string, string),
	withProcess func(string, string, func(any) (any, error)) (any, error),
) (any, error) {
	id, err := common.RequireThreadID("Server.turnForceComplete", threadID)
	if err != nil {
		return nil, err
	}
	fields := func(extra ...any) []any { return append(common.ThreadLogFields(id), extra...) }
	logger.Info("turn/forceComplete: request", fields()...)
	if cancelCodeRuns != nil {
		if cancelled := cancelCodeRuns(id); cancelled > 0 {
			logger.Info("turn/forceComplete: cancelled running code_run executions", fields("cancelled_runs", cancelled)...)
		}
	}
	if withProcess == nil {
		return nil, apperrors.New("Server.turnForceComplete", "thread process resolver is not initialized")
	}
	return withProcess("Server.turnForceComplete", id, func(proc any) (any, error) {
		if sendInterrupt != nil {
			noActiveTurn, err := sendInterrupt(proc)
			if err != nil {
				logger.Warn("turn/forceComplete: interrupt failed (best-effort)", fields(logger.FieldError, err)...)
			} else if noActiveTurn {
				logger.Info("turn/forceComplete: no active turn (best-effort)", fields()...)
			}
		}
		if notifyTurnCompleted != nil {
			notifyTurnCompleted(id, "completed", "force_complete")
		}
		return map[string]any{"confirmed": true, "forceCompleted": true}, nil
	})
}

var (
	ReadThreadRuntimeStateByHooks = readThreadRuntimeStateByHooks
	WaitInterruptOutcome          = waitInterruptOutcome
	SendInterruptCommand          = sendInterruptCommand
	NotifyTurnCompleted           = notifyTurnCompleted
	TurnInterrupt                 = turnInterrupt
	TurnForceComplete             = turnForceComplete
)

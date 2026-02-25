package codexadapter

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// TrackedTurn 是 turn 跟踪状态的跨包表示。
type TrackedTurn = contracts.TrackedTurn

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
	state := a.deps.Tracker
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

// BeginTrackedTurn 建立新的 turn 跟踪，并处理被覆盖 turn 的回收通知。
func (a *Adapter) BeginTrackedTurn(threadID, turnID string) string {
	activeTurns, turnMu, watchdogTimeout, stallThreshold := a.trackerState()
	completeTrackedTurnByID := a.CompleteTrackedTurnByID
	notify := a.trackerNotify()
	checkTurnStall := a.CheckTurnStall

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
	a.EnsureTurnTrackerStateLocked()
	if prev, ok := activeTurns[id]; ok {
		hadPrevTurn = true
		prevTurnID = prev.ID
		delete(activeTurns, id)
		if prev.Timer != nil {
			prev.Timer.Stop()
		}
		if prev.StallTimer != nil {
			prev.StallTimer.Stop()
		}
		doneSent := false
		select {
		case prev.Done <- "failed":
			doneSent = true
		default:
		}
		// Short-lived turns superseded without interrupt are normal (rapid user input).
		prevAge := time.Since(prev.StartedAt)
		prevLastEventAge := time.Since(prev.LastEventAt)
		logFn := logger.Warn
		if prevAge < 5*time.Second && !prev.InterruptRequested {
			logFn = logger.Info
		}
		logFn("turn tracker: superseding active turn",
			logger.FieldThreadID, id,
			"prev_turn_id", prev.ID,
			"next_turn_id", tid,
			"prev_age_ms", prevAge.Milliseconds(),
			"prev_last_event_age_ms", prevLastEventAge.Milliseconds(),
			"prev_interrupt_requested", prev.InterruptRequested,
			"prev_done_sent", doneSent,
			"prev_stall_hint_logged", prev.StallHintLogged,
			"prev_stall_grace_started", prev.StallGraceStarted,
			"prev_stall_auto_interrupted", prev.StallAutoInterrupted,
		)
		superseded = map[string]any{
			"threadId": id,
			"turn": map[string]any{
				"id":     prev.ID,
				"status": "failed",
			},
			"status": "failed",
			"reason": "superseded_by_new_turn",
		}
	} else {
		logger.Warn("DIAG: beginTrackedTurn no prev turn in activeTurns",
			logger.FieldThreadID, id,
			"new_turn_id", tid,
			"active_turns_count", len(activeTurns),
		)
	}

	turn := &TrackedTurn{
		ID:          tid,
		ThreadID:    id,
		StartedAt:   time.Now(),
		LastEventAt: time.Now(),
		Done:        make(chan string, 1),
	}
	watchdogTurnID := tid
	watchdogThreadID := id
	watchdogStartedAt := turn.StartedAt
	turn.Timer = time.AfterFunc(watchdogTimeout, func() {
		logger.Warn("turn tracker: watchdog timeout reached",
			logger.FieldThreadID, watchdogThreadID,
			logger.FieldTurnID, watchdogTurnID,
			"watchdog_timeout_ms", watchdogTimeout.Milliseconds(),
			"turn_age_ms", time.Since(watchdogStartedAt).Milliseconds(),
		)
		if notify == nil {
			return
		}
		if completion, ok := completeTrackedTurnByID(watchdogThreadID, watchdogTurnID, "failed", "watchdog_timeout"); ok {
			notify("turn/completed", completion)
		}
	})
	activeTurns[id] = turn

	// Start stall detection timer.
	stallThreadID := id
	stallTurnID := tid
	stallInterval := max(stallThreshold/3, 10*time.Second)
	turn.StallTimer = time.AfterFunc(stallInterval, func() {
		checkTurnStall(stallThreadID, stallTurnID)
	})
	turnMu.Unlock()

	logger.Info("turn tracker: begin turn tracking",
		logger.FieldThreadID, id,
		logger.FieldTurnID, tid,
		"source_turn_id", strings.TrimSpace(turnID),
		"watchdog_timeout_ms", watchdogTimeout.Milliseconds(),
		"had_prev_turn", hadPrevTurn,
		"prev_turn_id", prevTurnID,
	)

	if superseded != nil && notify != nil {
		logger.Warn("DIAG: emitting turn/completed for superseded turn BEFORE returning",
			logger.FieldThreadID, id,
			"superseded_turn_id", prevTurnID,
			"new_turn_id", tid,
		)
		notify("turn/completed", superseded)
		logger.Warn("DIAG: emitted turn/completed for superseded turn AFTER notify",
			logger.FieldThreadID, id,
			"superseded_turn_id", prevTurnID,
			"new_turn_id", tid,
		)
	}
	return tid
}

// HasActiveTrackedTurn checks whether a thread has an active tracked turn.
func (a *Adapter) HasActiveTrackedTurn(threadID string) bool {
	activeTurns, turnMu, _, _ := a.trackerState()
	id := strings.TrimSpace(threadID)
	if id == "" || turnMu == nil {
		return false
	}
	turnMu.Lock()
	defer turnMu.Unlock()
	if activeTurns == nil {
		return false
	}
	_, ok := activeTurns[id]
	return ok
}

// MarkTrackedTurnInterruptRequested marks interrupt intent on current tracked turn.
func (a *Adapter) MarkTrackedTurnInterruptRequested(threadID string) bool {
	activeTurns, turnMu, _, _ := a.trackerState()
	id := strings.TrimSpace(threadID)
	if id == "" || turnMu == nil {
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
	turn.InterruptRequested = true
	turn.InterruptRequestedAt = time.Now()
	return true
}

// WaitTrackedTurnTerminal waits until tracked turn reaches terminal status or timeout.
func (a *Adapter) WaitTrackedTurnTerminal(threadID string, timeout time.Duration) (string, bool) {
	activeTurns, turnMu, _, _ := a.trackerState()
	id := strings.TrimSpace(threadID)
	if id == "" || timeout <= 0 || turnMu == nil {
		return "", false
	}

	turnMu.Lock()
	if activeTurns == nil {
		turnMu.Unlock()
		return "", false
	}
	turn, ok := activeTurns[id]
	if !ok || turn == nil || turn.Done == nil {
		turnMu.Unlock()
		return "", false
	}
	done := turn.Done
	turnMu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case status := <-done:
		return NormalizeTrackedTurnStatus(status), true
	case <-timer.C:
		return "", false
	}
}

// CompleteTrackedTurnByID closes a tracked turn and returns completion payload.
func (a *Adapter) CompleteTrackedTurnByID(threadID, turnID, status, reason string) (map[string]any, bool) {
	activeTurns, turnMu, _, _ := a.trackerState()
	id := strings.TrimSpace(threadID)
	if id == "" || turnMu == nil {
		return nil, false
	}
	wantTurnID := strings.TrimSpace(turnID)

	turnMu.Lock()
	if activeTurns == nil {
		turnMu.Unlock()
		return nil, false
	}
	turn, ok := activeTurns[id]
	if !ok || turn == nil {
		turnMu.Unlock()
		return nil, false
	}
	if wantTurnID != "" && !strings.EqualFold(strings.TrimSpace(turn.ID), wantTurnID) {
		// Turn ID mismatch is informational; completion still proceeds to avoid stuck state.
		logger.Info("turn tracker: turn id mismatch, completing anyway to avoid stuck state",
			logger.FieldThreadID, id,
			"active_turn_id", strings.TrimSpace(turn.ID),
			"event_turn_id", wantTurnID,
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
		)
	}
	delete(activeTurns, id)
	if turn.Timer != nil {
		turn.Timer.Stop()
	}
	if turn.StallTimer != nil {
		turn.StallTimer.Stop()
	}
	finalStatus := NormalizeTrackedTurnStatus(status)
	if turn.InterruptRequested && finalStatus == "completed" {
		finalStatus = "interrupted"
	}
	select {
	case turn.Done <- finalStatus:
	default:
	}
	turnMu.Unlock()

	reasonText := strings.TrimSpace(reason)

	payload := map[string]any{
		"threadId": id,
		"turn": map[string]any{
			"id":     turn.ID,
			"status": finalStatus,
		},
		"status": finalStatus,
		"reason": reasonText,
	}
	logger.Info("turn tracker: turn completed",
		logger.FieldThreadID, id,
		logger.FieldTurnID, turn.ID,
		logger.FieldStatus, finalStatus,
		"reason", reasonText,
		"duration_ms", time.Since(turn.StartedAt).Milliseconds(),
		"interrupt_requested", turn.InterruptRequested,
	)
	return payload, true
}

// RememberTrackedTurnSummary records summary into adapter-owned cache state.
func (a *Adapter) RememberTrackedTurnSummary(threadID, turnID, summary string) {
	if a == nil {
		return
	}
	state := a.trackerHelperState()
	RememberTrackedTurnSummary(state, state.Mu, threadID, turnID, summary)
}

// CaptureAndInjectTurnSummary captures terminal summaries and injects them into completion payloads.
func (a *Adapter) CaptureAndInjectTurnSummary(threadID, eventType, method string, payload map[string]any) {
	if a == nil {
		return
	}
	CaptureAndInjectTurnSummary(
		threadID,
		eventType,
		method,
		payload,
		a.PeekTrackedTurnMeta,
		nil,
		nil,
		nil,
		a.RememberTrackedTurnSummary,
		a.LookupTrackedTurnSummary,
		nil,
	)
}

// MaybeFinalizeTrackedTurn applies turn-terminal events to tracked-turn state.
func (a *Adapter) MaybeFinalizeTrackedTurn(threadID, eventType, method string, payload map[string]any) {
	if a == nil {
		return
	}

	notify := a.trackerNotify()

	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	turnID, startedAt, interruptRequested, ok := a.PeekTrackedTurnMeta(id)
	if !ok {
		return
	}

	eventTurnID, status, reason, terminal, synthetic := TrackedTurnTerminalFromEvent(eventType, method, payload)
	if !terminal {
		if ShouldLogTrackedTurnStallHint(eventType, method, startedAt) && a.MarkTrackedTurnStallHint(id, turnID) {
			fields := []any{
				logger.FieldThreadID, id,
				"tracked_turn_id", turnID,
				"event_turn_id", eventTurnID,
				logger.FieldEventType, strings.TrimSpace(eventType),
				logger.FieldMethod, strings.TrimSpace(method),
				"turn_age_ms", time.Since(startedAt).Milliseconds(),
				"interrupt_requested", interruptRequested,
			}
			fields = append(fields, TrackedTurnPayloadDiagKV(payload)...)
			logger.Warn("turn tracker: active turn not terminal yet at tail event", fields...)
		}
		return
	}

	if strings.TrimSpace(eventTurnID) == "" {
		fields := []any{
			logger.FieldThreadID, id,
			"tracked_turn_id", turnID,
			logger.FieldEventType, strings.TrimSpace(eventType),
			logger.FieldMethod, strings.TrimSpace(method),
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
		}
		fields = append(fields, TrackedTurnPayloadDiagKV(payload)...)
		logger.Warn("DIAG: terminal event missing turn_id", fields...)
	}

	completion, ok := a.CompleteTrackedTurnByID(id, eventTurnID, status, reason)
	if !ok {
		fields := []any{
			logger.FieldThreadID, id,
			"tracked_turn_id", turnID,
			"event_turn_id", eventTurnID,
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
			logger.FieldEventType, strings.TrimSpace(eventType),
			logger.FieldMethod, strings.TrimSpace(method),
		}
		fields = append(fields, TrackedTurnPayloadDiagKV(payload)...)
		logger.Warn("turn tracker: terminal event failed to close tracked turn", fields...)
		return
	}
	logger.Info("turn tracker: finalized by event",
		logger.FieldThreadID, id,
		"tracked_turn_id", turnID,
		"event_turn_id", eventTurnID,
		logger.FieldStatus, strings.TrimSpace(status),
		"reason", strings.TrimSpace(reason),
		"synthetic", synthetic,
		logger.FieldEventType, strings.TrimSpace(eventType),
		logger.FieldMethod, strings.TrimSpace(method),
	)

	summary := TrackedTurnSummaryFromPayload(payload)
	if summary == "" {
		summary = a.LookupTrackedTurnSummary(id, util.FirstNonEmpty(eventTurnID, ExtractTrackedTurnID(payload), turnID))
	}
	if summary != "" {
		InjectTrackedTurnSummary(completion, summary)
		a.RememberTrackedTurnSummary(id, util.FirstNonEmpty(ExtractTrackedTurnID(completion), eventTurnID, ExtractTrackedTurnID(payload)), summary)
	}

	if synthetic {
		if notify != nil {
			notify("turn/completed", completion)
		}
		return
	}
	MergeTrackedTurnCompletionPayload(payload, completion)
}

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
		a.RescheduleStallCheck(turn, threadID, turnID, silent, threshold, a.CheckTurnStall)
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
	if a != nil && a.ctx != nil && a.ctx.UIRuntime() != nil {
		pushAlert = a.ctx.UIRuntime().PushAlert
	}
	a.HandleStallGracePeriod(
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
		if runtime := a.ctx.UIRuntime(); runtime != nil {
			pushAlert = runtime.PushAlert
		}
		manager = a.ctx.Manager()
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

// NormalizeTrackedTurnStatus maps raw turn status strings to canonical values.
func NormalizeTrackedTurnStatus(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "completed", "complete", "done", "success", "succeeded":
		return "completed"
	case "interrupted", "cancelled", "canceled", "aborted":
		return "interrupted"
	case "failed", "error", "timeout":
		return "failed"
	default:
		if s == "" {
			return "completed"
		}
		return s
	}
}

package codexadapter

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// TrackedTurn 是 turn 跟踪状态的跨包表示。
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

// BeginTrackedTurnHooks 提供 begin-tracking 所需的服务端回调。
type BeginTrackedTurnHooks struct {
	EnsureTurnTrackerLocked func()
	CompleteTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool)
	Notify                  func(method string, params any)
	CheckTurnStall          func(threadID, turnID string)
}

// BeginTrackedTurn 建立新的 turn 跟踪，并处理被覆盖 turn 的回收通知。
func (a *Adapter) BeginTrackedTurn(
	activeTurns map[string]*TrackedTurn,
	turnMu *sync.Mutex,
	turnWatchdogTimeout time.Duration,
	stallThreshold time.Duration,
	threadID,
	turnID string,
	hooks BeginTrackedTurnHooks,
) string {
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
	if hooks.EnsureTurnTrackerLocked != nil {
		hooks.EnsureTurnTrackerLocked()
	}
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
	turn.Timer = time.AfterFunc(turnWatchdogTimeout, func() {
		logger.Warn("turn tracker: watchdog timeout reached",
			logger.FieldThreadID, watchdogThreadID,
			logger.FieldTurnID, watchdogTurnID,
			"watchdog_timeout_ms", turnWatchdogTimeout.Milliseconds(),
			"turn_age_ms", time.Since(watchdogStartedAt).Milliseconds(),
		)
		if hooks.CompleteTrackedTurnByID == nil || hooks.Notify == nil {
			return
		}
		if completion, ok := hooks.CompleteTrackedTurnByID(watchdogThreadID, watchdogTurnID, "failed", "watchdog_timeout"); ok {
			hooks.Notify("turn/completed", completion)
		}
	})
	activeTurns[id] = turn

	// Start stall detection timer.
	stallThreadID := id
	stallTurnID := tid
	stallInterval := max(stallThreshold/3, 10*time.Second)
	turn.StallTimer = time.AfterFunc(stallInterval, func() {
		if hooks.CheckTurnStall != nil {
			hooks.CheckTurnStall(stallThreadID, stallTurnID)
		}
	})
	turnMu.Unlock()

	logger.Info("turn tracker: begin turn tracking",
		logger.FieldThreadID, id,
		logger.FieldTurnID, tid,
		"source_turn_id", strings.TrimSpace(turnID),
		"watchdog_timeout_ms", turnWatchdogTimeout.Milliseconds(),
		"had_prev_turn", hadPrevTurn,
		"prev_turn_id", prevTurnID,
	)

	if superseded != nil && hooks.Notify != nil {
		logger.Warn("DIAG: emitting turn/completed for superseded turn BEFORE returning",
			logger.FieldThreadID, id,
			"superseded_turn_id", prevTurnID,
			"new_turn_id", tid,
		)
		hooks.Notify("turn/completed", superseded)
		logger.Warn("DIAG: emitted turn/completed for superseded turn AFTER notify",
			logger.FieldThreadID, id,
			"superseded_turn_id", prevTurnID,
			"new_turn_id", tid,
		)
	}
	return tid
}

// HasActiveTrackedTurn checks whether a thread has an active tracked turn.
func (a *Adapter) HasActiveTrackedTurn(activeTurns map[string]*TrackedTurn, turnMu *sync.Mutex, threadID string) bool {
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
func (a *Adapter) MarkTrackedTurnInterruptRequested(activeTurns map[string]*TrackedTurn, turnMu *sync.Mutex, threadID string) bool {
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
func (a *Adapter) WaitTrackedTurnTerminal(activeTurns map[string]*TrackedTurn, turnMu *sync.Mutex, threadID string, timeout time.Duration) (string, bool) {
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

// CompleteTrackedTurnByIDOptions carries completion dependencies and identifiers.
type CompleteTrackedTurnByIDOptions struct {
	ActiveTurns map[string]*TrackedTurn
	TurnMu      *sync.Mutex
	ThreadID    string
	TurnID      string
	Status      string
	Reason      string
}

// CompleteTrackedTurnByID closes a tracked turn and returns completion payload.
func (a *Adapter) CompleteTrackedTurnByID(opt CompleteTrackedTurnByIDOptions) (map[string]any, bool) {
	id := strings.TrimSpace(opt.ThreadID)
	if id == "" || opt.TurnMu == nil {
		return nil, false
	}
	wantTurnID := strings.TrimSpace(opt.TurnID)

	opt.TurnMu.Lock()
	if opt.ActiveTurns == nil {
		opt.TurnMu.Unlock()
		return nil, false
	}
	turn, ok := opt.ActiveTurns[id]
	if !ok || turn == nil {
		opt.TurnMu.Unlock()
		return nil, false
	}
	if wantTurnID != "" && !strings.EqualFold(strings.TrimSpace(turn.ID), wantTurnID) {
		// Turn ID mismatch is informational; completion still proceeds to avoid stuck state.
		logger.Info("turn tracker: turn id mismatch, completing anyway to avoid stuck state",
			logger.FieldThreadID, id,
			"active_turn_id", strings.TrimSpace(turn.ID),
			"event_turn_id", wantTurnID,
			logger.FieldStatus, strings.TrimSpace(opt.Status),
			"reason", strings.TrimSpace(opt.Reason),
		)
	}
	delete(opt.ActiveTurns, id)
	if turn.Timer != nil {
		turn.Timer.Stop()
	}
	if turn.StallTimer != nil {
		turn.StallTimer.Stop()
	}
	finalStatus := NormalizeTrackedTurnStatus(opt.Status)
	if turn.InterruptRequested && finalStatus == "completed" {
		finalStatus = "interrupted"
	}
	select {
	case turn.Done <- finalStatus:
	default:
	}
	opt.TurnMu.Unlock()

	payload := map[string]any{
		"threadId": id,
		"turn": map[string]any{
			"id":     turn.ID,
			"status": finalStatus,
		},
		"status": finalStatus,
		"reason": strings.TrimSpace(opt.Reason),
	}
	logger.Info("turn tracker: turn completed",
		logger.FieldThreadID, id,
		logger.FieldTurnID, turn.ID,
		logger.FieldStatus, finalStatus,
		"reason", strings.TrimSpace(opt.Reason),
		"duration_ms", time.Since(turn.StartedAt).Milliseconds(),
		"interrupt_requested", turn.InterruptRequested,
	)
	return payload, true
}

// MaybeFinalizeTrackedTurnOptions carries event-finalization hooks.
type MaybeFinalizeTrackedTurnOptions struct {
	ThreadID  string
	EventType string
	Method    string
	Payload   map[string]any

	PeekTrackedTurnMeta func(threadID string) (turnID string, startedAt time.Time, interruptRequested bool, ok bool)

	TrackedTurnTerminalFromEvent  func(eventType, method string, payload map[string]any) (eventTurnID, status, reason string, terminal bool, synthetic bool)
	ShouldLogTrackedTurnStallHint func(eventType, method string, startedAt time.Time) bool
	MarkTrackedTurnStallHint      func(threadID, turnID string) bool
	TrackedTurnPayloadDiagKV      func(payload map[string]any) []any
	CompleteTrackedTurnByID       func(threadID, turnID, status, reason string) (map[string]any, bool)

	TrackedTurnSummaryFromPayload     func(payload map[string]any) string
	LookupTrackedTurnSummary          func(threadID, turnID string) string
	ExtractTrackedTurnID              func(payload map[string]any) string
	InjectTrackedTurnSummary          func(payload map[string]any, summary string)
	RememberTrackedTurnSummary        func(threadID, turnID, summary string)
	MergeTrackedTurnCompletionPayload func(payload, completion map[string]any)
	Notify                            func(method string, params any)
	FirstNonEmpty                     func(...string) string
}

// MaybeFinalizeTrackedTurn applies turn-terminal events to tracked-turn state.
func (a *Adapter) MaybeFinalizeTrackedTurn(opt MaybeFinalizeTrackedTurnOptions) {
	id := strings.TrimSpace(opt.ThreadID)
	if id == "" || opt.PeekTrackedTurnMeta == nil || opt.TrackedTurnTerminalFromEvent == nil || opt.CompleteTrackedTurnByID == nil {
		return
	}
	turnID, startedAt, interruptRequested, ok := opt.PeekTrackedTurnMeta(id)
	if !ok {
		return
	}

	eventTurnID, status, reason, terminal, synthetic := opt.TrackedTurnTerminalFromEvent(opt.EventType, opt.Method, opt.Payload)
	if !terminal {
		if opt.ShouldLogTrackedTurnStallHint != nil &&
			opt.MarkTrackedTurnStallHint != nil &&
			opt.ShouldLogTrackedTurnStallHint(opt.EventType, opt.Method, startedAt) &&
			opt.MarkTrackedTurnStallHint(id, turnID) {
			fields := []any{
				logger.FieldThreadID, id,
				"tracked_turn_id", turnID,
				"event_turn_id", eventTurnID,
				logger.FieldEventType, strings.TrimSpace(opt.EventType),
				logger.FieldMethod, strings.TrimSpace(opt.Method),
				"turn_age_ms", time.Since(startedAt).Milliseconds(),
				"interrupt_requested", interruptRequested,
			}
			if opt.TrackedTurnPayloadDiagKV != nil {
				fields = append(fields, opt.TrackedTurnPayloadDiagKV(opt.Payload)...)
			}
			logger.Warn("turn tracker: active turn not terminal yet at tail event", fields...)
		}
		return
	}

	if strings.TrimSpace(eventTurnID) == "" {
		fields := []any{
			logger.FieldThreadID, id,
			"tracked_turn_id", turnID,
			logger.FieldEventType, strings.TrimSpace(opt.EventType),
			logger.FieldMethod, strings.TrimSpace(opt.Method),
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
		}
		if opt.TrackedTurnPayloadDiagKV != nil {
			fields = append(fields, opt.TrackedTurnPayloadDiagKV(opt.Payload)...)
		}
		logger.Warn("DIAG: terminal event missing turn_id", fields...)
	}

	completion, ok := opt.CompleteTrackedTurnByID(id, eventTurnID, status, reason)
	if !ok {
		fields := []any{
			logger.FieldThreadID, id,
			"tracked_turn_id", turnID,
			"event_turn_id", eventTurnID,
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
			logger.FieldEventType, strings.TrimSpace(opt.EventType),
			logger.FieldMethod, strings.TrimSpace(opt.Method),
		}
		if opt.TrackedTurnPayloadDiagKV != nil {
			fields = append(fields, opt.TrackedTurnPayloadDiagKV(opt.Payload)...)
		}
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
		logger.FieldEventType, strings.TrimSpace(opt.EventType),
		logger.FieldMethod, strings.TrimSpace(opt.Method),
	)

	firstNonEmpty := opt.FirstNonEmpty
	if firstNonEmpty == nil {
		firstNonEmpty = func(values ...string) string {
			for _, v := range values {
				if strings.TrimSpace(v) != "" {
					return v
				}
			}
			return ""
		}
	}

	summary := ""
	if opt.TrackedTurnSummaryFromPayload != nil {
		summary = opt.TrackedTurnSummaryFromPayload(opt.Payload)
	}
	if summary == "" && opt.LookupTrackedTurnSummary != nil {
		extractID := func(payload map[string]any) string {
			if opt.ExtractTrackedTurnID == nil {
				return ""
			}
			return opt.ExtractTrackedTurnID(payload)
		}
		summary = opt.LookupTrackedTurnSummary(id, firstNonEmpty(eventTurnID, extractID(opt.Payload), turnID))
	}
	if summary != "" {
		if opt.InjectTrackedTurnSummary != nil {
			opt.InjectTrackedTurnSummary(completion, summary)
		}
		if opt.RememberTrackedTurnSummary != nil {
			extractID := func(payload map[string]any) string {
				if opt.ExtractTrackedTurnID == nil {
					return ""
				}
				return opt.ExtractTrackedTurnID(payload)
			}
			opt.RememberTrackedTurnSummary(id, firstNonEmpty(extractID(completion), eventTurnID, extractID(opt.Payload)), summary)
		}
	}

	if synthetic {
		if opt.Notify != nil {
			opt.Notify("turn/completed", completion)
		}
		return
	}
	if opt.MergeTrackedTurnCompletionPayload != nil {
		opt.MergeTrackedTurnCompletionPayload(opt.Payload, completion)
	}
}

// CheckTurnStallOptions carries turn-stall detection dependencies.
type CheckTurnStallOptions struct {
	ActiveTurns            map[string]*TrackedTurn
	TurnMu                 *sync.Mutex
	ThreadID               string
	TurnID                 string
	StallThreshold         time.Duration
	DefaultStallThreshold  time.Duration
	Reschedule             func(turn *TrackedTurn, threadID, turnID string, silent, threshold time.Duration)
	HandleStallGracePeriod func(threadID, turnID string, silent, threshold time.Duration)
	ExecuteStallInterrupt  func(threadID, turnID string, silent, threshold time.Duration)
}

// CheckTurnStall advances stall detection state machine.
func (a *Adapter) CheckTurnStall(opt CheckTurnStallOptions) {
	if opt.TurnMu == nil {
		return
	}
	opt.TurnMu.Lock()
	if opt.ActiveTurns == nil {
		opt.TurnMu.Unlock()
		return
	}
	turn, ok := opt.ActiveTurns[opt.ThreadID]
	if !ok || turn == nil || turn.ID != opt.TurnID {
		opt.TurnMu.Unlock()
		return
	}

	silent := time.Since(turn.LastEventAt)
	threshold := opt.StallThreshold
	if threshold <= 0 {
		threshold = opt.DefaultStallThreshold
	}

	// Not stalled yet — reschedule and check again.
	if silent < threshold {
		if opt.Reschedule != nil {
			opt.Reschedule(turn, opt.ThreadID, opt.TurnID, silent, threshold)
		}
		opt.TurnMu.Unlock()
		return
	}

	// Already auto-interrupted — nothing to do.
	if turn.StallAutoInterrupted {
		opt.TurnMu.Unlock()
		return
	}

	// Grace period: first detection → warn + reschedule.
	if !turn.StallGraceStarted {
		turn.StallGraceStarted = true
		opt.TurnMu.Unlock()
		if opt.HandleStallGracePeriod != nil {
			opt.HandleStallGracePeriod(opt.ThreadID, opt.TurnID, silent, threshold)
		}
		return
	}

	// Second detection (after grace period) → actually interrupt.
	turn.StallAutoInterrupted = true
	opt.TurnMu.Unlock()
	if opt.ExecuteStallInterrupt != nil {
		opt.ExecuteStallInterrupt(opt.ThreadID, opt.TurnID, silent, threshold)
	}
}

// ExecuteStallAutoInterruptOptions carries auto-interrupt execution dependencies.
type ExecuteStallAutoInterruptOptions struct {
	ThreadID                          string
	TurnID                            string
	Silent                            time.Duration
	Threshold                         time.Duration
	PushAlert                         func(threadID, category, message string)
	MarkTrackedTurnInterruptRequested func(threadID string) bool
	CancelCodeRuns                    func(threadID string) int
	Manager                           *runner.AgentManager
	CompleteTrackedTurnByID           func(threadID, turnID, status, reason string) (map[string]any, bool)
	Notify                            func(method string, params any)
}

// ExecuteStallAutoInterrupt performs /interrupt and fallback completion when stalled.
func (a *Adapter) ExecuteStallAutoInterrupt(opt ExecuteStallAutoInterruptOptions) {
	logger.Warn("turn tracker: thinking stall detected — auto interrupting",
		logger.FieldThreadID, opt.ThreadID,
		logger.FieldTurnID, opt.TurnID,
		"silent_ms", opt.Silent.Milliseconds(),
		"threshold_ms", opt.Threshold.Milliseconds(),
	)

	if opt.PushAlert != nil {
		opt.PushAlert(opt.ThreadID, "stall",
			fmt.Sprintf("思考超时 %ds 未响应，自动中断", int(opt.Silent.Seconds())))
	}

	util.SafeGo(func() {
		if opt.MarkTrackedTurnInterruptRequested != nil {
			opt.MarkTrackedTurnInterruptRequested(opt.ThreadID)
		}
		if opt.CancelCodeRuns != nil {
			if cancelled := opt.CancelCodeRuns(opt.ThreadID); cancelled > 0 {
				logger.Info("turn tracker: cancelled running code_run executions",
					logger.FieldThreadID, opt.ThreadID,
					logger.FieldTurnID, opt.TurnID,
					"cancelled_runs", cancelled,
				)
			}
		}
		interrupted := false
		if opt.Manager != nil {
			if proc := opt.Manager.Get(opt.ThreadID); proc != nil {
				if err := a.SendCommand(proc, "/interrupt", ""); err != nil {
					logger.Warn("turn tracker: stall auto-interrupt failed",
						logger.FieldThreadID, opt.ThreadID,
						logger.FieldTurnID, opt.TurnID,
						logger.FieldError, err,
					)
				} else {
					interrupted = true
				}
			}
		}
		// Fallback: if /interrupt failed or process is gone, force-complete the tracker.
		if !interrupted && opt.CompleteTrackedTurnByID != nil && opt.Notify != nil {
			if completion, ok := opt.CompleteTrackedTurnByID(opt.ThreadID, opt.TurnID, "failed", "thinking_stall_timeout"); ok {
				opt.Notify("turn/completed", completion)
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

package apiserver

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const defaultTurnWatchdogTimeout = 10 * time.Minute
const defaultTrackedTurnSummaryTTL = 30 * time.Minute
const trackedTurnSummaryCacheMaxEntries = 512
const defaultStallThreshold = 480 * time.Second
const defaultStallHeartbeat = 300 * time.Second

type trackedTurn struct {
	ID                   string
	ThreadID             string
	StartedAt            time.Time
	LastEventAt          time.Time
	InterruptRequested   bool
	InterruptRequestedAt time.Time
	stallHintLogged      bool
	stallGraceStarted    bool
	stallAutoInterrupted bool
	done                 chan string
	timer                *time.Timer
	stallTimer           *time.Timer
}

type trackedTurnSummaryCacheEntry struct {
	TurnID    string
	Summary   string
	UpdatedAt time.Time
}

func (s *Server) ensureTurnTrackerLocked() {
	if s.activeTurns == nil {
		s.activeTurns = make(map[string]*trackedTurn)
	}
	if s.turnWatchdogTimeout <= 0 {
		s.turnWatchdogTimeout = defaultTurnWatchdogTimeout
	}
	if s.turnSummaryCache == nil {
		s.turnSummaryCache = make(map[string]trackedTurnSummaryCacheEntry)
	}
	if s.turnSummaryTTL <= 0 {
		s.turnSummaryTTL = defaultTrackedTurnSummaryTTL
	}
	if s.stallThreshold <= 0 {
		s.stallThreshold = defaultStallThreshold
	}
	if s.stallHeartbeat <= 0 {
		s.stallHeartbeat = defaultStallHeartbeat
	}
}

func (s *Server) beginTrackedTurn(threadID, turnID string) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return ""
	}
	tid := strings.TrimSpace(turnID)
	if tid == "" {
		tid = fmt.Sprintf("turn-%d", time.Now().UnixMilli())
	}

	var superseded map[string]any

	s.turnMu.Lock()
	s.ensureTurnTrackerLocked()
	if prev, ok := s.activeTurns[id]; ok {
		delete(s.activeTurns, id)
		if prev.timer != nil {
			prev.timer.Stop()
		}
		if prev.stallTimer != nil {
			prev.stallTimer.Stop()
		}
		select {
		case prev.done <- "failed":
		default:
		}
		// Short-lived turns superseded without interrupt are normal (rapid user input).
		prevAge := time.Since(prev.StartedAt)
		logFn := logger.Warn
		if prevAge < 5*time.Second && !prev.InterruptRequested {
			logFn = logger.Info
		}
		logFn("turn tracker: superseding active turn",
			logger.FieldThreadID, id,
			"prev_turn_id", prev.ID,
			"next_turn_id", tid,
			"prev_age_ms", prevAge.Milliseconds(),
			"prev_interrupt_requested", prev.InterruptRequested,
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
	}

	turn := &trackedTurn{
		ID:          tid,
		ThreadID:    id,
		StartedAt:   time.Now(),
		LastEventAt: time.Now(),
		done:        make(chan string, 1),
	}
	watchdogTurnID := tid
	watchdogThreadID := id
	watchdogStartedAt := turn.StartedAt
	turn.timer = time.AfterFunc(s.turnWatchdogTimeout, func() {
		logger.Warn("turn tracker: watchdog timeout reached",
			logger.FieldThreadID, watchdogThreadID,
			"turn_id", watchdogTurnID,
			"watchdog_timeout_ms", s.turnWatchdogTimeout.Milliseconds(),
			"turn_age_ms", time.Since(watchdogStartedAt).Milliseconds(),
		)
		if completion, ok := s.completeTrackedTurnByID(watchdogThreadID, watchdogTurnID, "failed", "watchdog_timeout"); ok {
			s.Notify("turn/completed", completion)
		}
	})
	s.activeTurns[id] = turn

	// Start stall detection timer
	stallThreadID := id
	stallTurnID := tid
	stallInterval := s.stallThreshold / 3
	if stallInterval < 10*time.Second {
		stallInterval = 10 * time.Second
	}
	turn.stallTimer = time.AfterFunc(stallInterval, func() {
		s.checkTurnStall(stallThreadID, stallTurnID)
	})

	s.turnMu.Unlock()

	logger.Info("turn tracker: begin turn tracking",
		logger.FieldThreadID, id,
		"turn_id", tid,
		"source_turn_id", strings.TrimSpace(turnID),
		"watchdog_timeout_ms", s.turnWatchdogTimeout.Milliseconds(),
	)

	if superseded != nil {
		s.Notify("turn/completed", superseded)
	}
	return tid
}

func (s *Server) hasActiveTrackedTurn(threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.activeTurns == nil {
		return false
	}
	_, ok := s.activeTurns[id]
	return ok
}

func (s *Server) markTrackedTurnInterruptRequested(threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.activeTurns == nil {
		return false
	}
	turn, ok := s.activeTurns[id]
	if !ok {
		return false
	}
	turn.InterruptRequested = true
	turn.InterruptRequestedAt = time.Now()
	return true
}

func (s *Server) waitTrackedTurnTerminal(threadID string, timeout time.Duration) (string, bool) {
	id := strings.TrimSpace(threadID)
	if id == "" || timeout <= 0 {
		return "", false
	}

	s.turnMu.Lock()
	if s.activeTurns == nil {
		s.turnMu.Unlock()
		return "", false
	}
	turn, ok := s.activeTurns[id]
	if !ok || turn == nil || turn.done == nil {
		s.turnMu.Unlock()
		return "", false
	}
	done := turn.done
	s.turnMu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case status := <-done:
		return normalizeTrackedTurnStatus(status), true
	case <-timer.C:
		return "", false
	}
}

func (s *Server) completeTrackedTurn(threadID, status, reason string) (map[string]any, bool) {
	return s.completeTrackedTurnByID(threadID, "", status, reason)
}

func (s *Server) completeTrackedTurnByID(threadID, turnID, status, reason string) (map[string]any, bool) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, false
	}
	wantTurnID := strings.TrimSpace(turnID)

	s.turnMu.Lock()
	if s.activeTurns == nil {
		s.turnMu.Unlock()
		return nil, false
	}
	turn, ok := s.activeTurns[id]
	if !ok || turn == nil {
		s.turnMu.Unlock()
		return nil, false
	}
	if wantTurnID != "" && !strings.EqualFold(strings.TrimSpace(turn.ID), wantTurnID) {
		// Turn ID mismatch is handled correctly (completing anyway),
		// so this is informational, not a warning.
		logger.Info("turn tracker: turn id mismatch, completing anyway to avoid stuck state",
			logger.FieldThreadID, id,
			"active_turn_id", strings.TrimSpace(turn.ID),
			"event_turn_id", wantTurnID,
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
		)
	}
	delete(s.activeTurns, id)
	if turn.timer != nil {
		turn.timer.Stop()
	}
	if turn.stallTimer != nil {
		turn.stallTimer.Stop()
	}
	finalStatus := normalizeTrackedTurnStatus(status)
	if turn.InterruptRequested && finalStatus == "completed" {
		finalStatus = "interrupted"
	}
	select {
	case turn.done <- finalStatus:
	default:
	}
	s.turnMu.Unlock()

	payload := map[string]any{
		"threadId": id,
		"turn": map[string]any{
			"id":     turn.ID,
			"status": finalStatus,
		},
		"status": finalStatus,
		"reason": strings.TrimSpace(reason),
	}
	logger.Info("turn tracker: turn completed",
		logger.FieldThreadID, id,
		"turn_id", turn.ID,
		logger.FieldStatus, finalStatus,
		"reason", strings.TrimSpace(reason),
		"duration_ms", time.Since(turn.StartedAt).Milliseconds(),
		"interrupt_requested", turn.InterruptRequested,
	)
	return payload, true
}

func trackedTurnSummaryFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if summary := extractTrackedString(payload, "lastAgentMessage", "last_agent_message"); summary != "" {
		return summary
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if summary := extractTrackedString(turn, "lastAgentMessage", "last_agent_message"); summary != "" {
			return summary
		}
	}
	if msg, ok := payload["msg"].(map[string]any); ok {
		if summary := extractTrackedString(msg, "lastAgentMessage", "last_agent_message"); summary != "" {
			return summary
		}
	}
	return ""
}

func trackedTurnSummaryCacheKey(threadID, turnID string) string {
	return strings.TrimSpace(threadID) + "\x00" + strings.TrimSpace(turnID)
}

func (s *Server) pruneTrackedTurnSummaryCacheLocked(now time.Time) {
	s.ensureTurnTrackerLocked()
	if len(s.turnSummaryCache) == 0 {
		return
	}

	cutoff := now.Add(-s.turnSummaryTTL)
	for key, entry := range s.turnSummaryCache {
		if entry.UpdatedAt.Before(cutoff) {
			delete(s.turnSummaryCache, key)
		}
	}

	if len(s.turnSummaryCache) <= trackedTurnSummaryCacheMaxEntries {
		return
	}

	type summaryCacheKV struct {
		key       string
		updatedAt time.Time
	}
	entries := make([]summaryCacheKV, 0, len(s.turnSummaryCache))
	for key, entry := range s.turnSummaryCache {
		entries = append(entries, summaryCacheKV{key: key, updatedAt: entry.UpdatedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].updatedAt.Before(entries[j].updatedAt)
	})

	trimCount := len(s.turnSummaryCache) - trackedTurnSummaryCacheMaxEntries
	for i := 0; i < trimCount && i < len(entries); i++ {
		delete(s.turnSummaryCache, entries[i].key)
	}
}

func (s *Server) rememberTrackedTurnSummary(threadID, turnID, summary string) {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	msg := strings.TrimSpace(summary)
	if id == "" || msg == "" {
		return
	}

	now := time.Now()

	s.turnMu.Lock()
	s.ensureTurnTrackerLocked()
	s.pruneTrackedTurnSummaryCacheLocked(now)
	entry := trackedTurnSummaryCacheEntry{
		TurnID:    tid,
		Summary:   msg,
		UpdatedAt: now,
	}
	s.turnSummaryCache[trackedTurnSummaryCacheKey(id, "")] = entry
	if tid != "" {
		s.turnSummaryCache[trackedTurnSummaryCacheKey(id, tid)] = entry
	}
	s.turnMu.Unlock()
}

func (s *Server) lookupTrackedTurnSummary(threadID, turnID string) string {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	if id == "" {
		return ""
	}

	now := time.Now()

	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	s.ensureTurnTrackerLocked()
	s.pruneTrackedTurnSummaryCacheLocked(now)

	if tid != "" {
		if entry, ok := s.turnSummaryCache[trackedTurnSummaryCacheKey(id, tid)]; ok {
			return strings.TrimSpace(entry.Summary)
		}
	}
	if entry, ok := s.turnSummaryCache[trackedTurnSummaryCacheKey(id, "")]; ok {
		entryTurnID := strings.TrimSpace(entry.TurnID)
		if tid != "" && entryTurnID != "" && !strings.EqualFold(tid, entryTurnID) {
			return ""
		}
		return strings.TrimSpace(entry.Summary)
	}
	return ""
}

func injectTrackedTurnSummary(payload map[string]any, summary string) {
	if payload == nil {
		return
	}
	msg := strings.TrimSpace(summary)
	if msg == "" {
		return
	}

	payload["lastAgentMessage"] = msg

	turnPayload, _ := payload["turn"].(map[string]any)
	if turnPayload == nil {
		turnPayload = make(map[string]any)
	}
	turnPayload["lastAgentMessage"] = msg
	payload["turn"] = turnPayload
}

func (s *Server) captureAndInjectTurnSummary(threadID, eventType, method string, payload map[string]any) {
	if payload == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}

	turnID := extractTrackedTurnID(payload)
	resolvedTurnID := turnID
	if resolvedTurnID == "" {
		if activeTurnID, _, _, ok := s.peekTrackedTurnMeta(id); ok {
			resolvedTurnID = activeTurnID
		}
	}
	summary := trackedTurnSummaryFromPayload(payload)
	if summary != "" {
		_, _, _, terminal, _ := trackedTurnTerminalFromEvent(eventType, method, payload)
		methodKey := strings.ToLower(strings.TrimSpace(method))
		eventKey := strings.ToLower(strings.TrimSpace(eventType))
		if terminal || methodKey == "codex/event/task_complete" || eventKey == "codex/event/task_complete" {
			s.rememberTrackedTurnSummary(id, resolvedTurnID, summary)
		}
	}

	if !strings.EqualFold(strings.TrimSpace(method), "turn/completed") {
		return
	}
	if summary == "" {
		summary = s.lookupTrackedTurnSummary(id, resolvedTurnID)
	}
	if summary == "" {
		return
	}
	injectTrackedTurnSummary(payload, summary)
	s.rememberTrackedTurnSummary(id, resolvedTurnID, summary)
}

func mergeTrackedTurnCompletionPayload(payload, completion map[string]any) {
	if payload == nil || completion == nil {
		return
	}
	for key, value := range completion {
		if key != "turn" {
			payload[key] = value
			continue
		}
		completionTurn, ok := value.(map[string]any)
		if !ok {
			payload[key] = value
			continue
		}
		currentTurn, ok := payload[key].(map[string]any)
		if !ok || currentTurn == nil {
			currentTurn = make(map[string]any, len(completionTurn))
		}
		for turnKey, turnValue := range completionTurn {
			currentTurn[turnKey] = turnValue
		}
		payload[key] = currentTurn
	}
}

func threadStatusTerminalFromPayload(payload map[string]any) (status string, reason string, terminal bool) {
	if payload == nil {
		return "", "", false
	}

	statusType := ""
	switch raw := payload["status"].(type) {
	case string:
		statusType = strings.ToLower(strings.TrimSpace(raw))
	case map[string]any:
		statusType = strings.ToLower(strings.TrimSpace(extractTrackedString(raw, "type")))
	}

	if statusType == "" {
		return "", "", false
	}

	switch statusType {
	case "idle":
		return "completed", "thread_status_idle", true
	case "systemerror", "system_error", "error":
		return "failed", "thread_status_system_error", true
	case "notloaded", "not_loaded":
		return "failed", "thread_status_not_loaded", true
	default:
		return "", "", false
	}
}

func normalizeTrackedTurnStatus(status string) string {
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

func (s *Server) maybeFinalizeTrackedTurn(threadID, eventType, method string, payload map[string]any) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	turnID, startedAt, interruptRequested, ok := s.peekTrackedTurnMeta(id)
	if !ok {
		return
	}

	eventTurnID, status, reason, terminal, synthetic := trackedTurnTerminalFromEvent(eventType, method, payload)
	if !terminal {
		if shouldLogTrackedTurnStallHint(eventType, method, startedAt) && s.markTrackedTurnStallHint(id, turnID) {
			logger.Warn("turn tracker: active turn not terminal yet at tail event",
				logger.FieldThreadID, id,
				"tracked_turn_id", turnID,
				"event_turn_id", eventTurnID,
				logger.FieldEventType, strings.TrimSpace(eventType),
				logger.FieldMethod, strings.TrimSpace(method),
				"turn_age_ms", time.Since(startedAt).Milliseconds(),
				"interrupt_requested", interruptRequested,
			)
		}
		return
	}

	completion, ok := s.completeTrackedTurnByID(id, eventTurnID, status, reason)
	if !ok {
		logger.Warn("turn tracker: terminal event failed to close tracked turn",
			logger.FieldThreadID, id,
			"tracked_turn_id", turnID,
			"event_turn_id", eventTurnID,
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
			logger.FieldEventType, strings.TrimSpace(eventType),
			logger.FieldMethod, strings.TrimSpace(method),
		)
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

	summary := trackedTurnSummaryFromPayload(payload)
	if summary == "" {
		summary = s.lookupTrackedTurnSummary(id, util.FirstNonEmpty(eventTurnID, extractTrackedTurnID(payload), turnID))
	}
	if summary != "" {
		injectTrackedTurnSummary(completion, summary)
		s.rememberTrackedTurnSummary(id, util.FirstNonEmpty(extractTrackedTurnID(completion), eventTurnID, extractTrackedTurnID(payload)), summary)
	}

	if synthetic {
		s.Notify("turn/completed", completion)
		return
	}
	mergeTrackedTurnCompletionPayload(payload, completion)
}

func (s *Server) peekTrackedTurnMeta(threadID string) (string, time.Time, bool, bool) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", time.Time{}, false, false
	}

	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.activeTurns == nil {
		return "", time.Time{}, false, false
	}
	turn, ok := s.activeTurns[id]
	if !ok || turn == nil {
		return "", time.Time{}, false, false
	}
	return turn.ID, turn.StartedAt, turn.InterruptRequested, true
}

func (s *Server) markTrackedTurnStallHint(threadID, turnID string) bool {
	id := strings.TrimSpace(threadID)
	wantTurnID := strings.TrimSpace(turnID)
	if id == "" || wantTurnID == "" {
		return false
	}

	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.activeTurns == nil {
		return false
	}
	turn, ok := s.activeTurns[id]
	if !ok || turn == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(turn.ID), wantTurnID) {
		return false
	}
	if turn.stallHintLogged {
		return false
	}
	turn.stallHintLogged = true
	return true
}

func shouldLogTrackedTurnStallHint(eventType, method string, startedAt time.Time) bool {
	if startedAt.IsZero() {
		return false
	}
	age := time.Since(startedAt)
	if age < 60*time.Second {
		return false
	}

	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))
	switch methodKey {
	case "turn/diff/updated", "turn/plan/updated", "item/completed", "item/plan/delta", "item/agentmessage/delta", "codex/event/turn_diff", "codex/event/plan_delta":
		return true
	}
	switch eventKey {
	case "turn_diff", "plan_delta", "item/completed":
		return true
	default:
		return false
	}
}

// checkTurnStall is called periodically by the stall timer.
// If no events have been received for the configured stall threshold, it pushes
// an alert and auto-interrupts the turn.
func (s *Server) checkTurnStall(threadID, turnID string) {
	s.turnMu.Lock()
	if s.activeTurns == nil {
		s.turnMu.Unlock()
		return
	}
	turn, ok := s.activeTurns[threadID]
	if !ok || turn == nil || turn.ID != turnID {
		s.turnMu.Unlock()
		return
	}

	silent := time.Since(turn.LastEventAt)
	threshold := s.stallThreshold
	if threshold <= 0 {
		threshold = defaultStallThreshold
	}

	// Not stalled yet — reschedule and check again.
	if silent < threshold {
		s.rescheduleStallCheck(turn, threadID, turnID, silent, threshold)
		s.turnMu.Unlock()
		return
	}

	// Already auto-interrupted — nothing to do.
	if turn.stallAutoInterrupted {
		s.turnMu.Unlock()
		return
	}

	// Grace period: first detection → warn + reschedule 30s.
	if !turn.stallGraceStarted {
		turn.stallGraceStarted = true
		s.turnMu.Unlock()
		s.handleStallGracePeriod(threadID, turnID, silent, threshold)
		return
	}

	// Second detection (after grace period) → actually interrupt.
	turn.stallAutoInterrupted = true
	s.turnMu.Unlock()
	s.executeStallAutoInterrupt(threadID, turnID, silent, threshold)
}

// rescheduleStallCheck schedules the next stall check timer.
// Must be called with s.turnMu held.
func (s *Server) rescheduleStallCheck(turn *trackedTurn, threadID, turnID string, silent, threshold time.Duration) {
	interval := threshold / 3
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	remaining := interval
	if remaining > threshold-silent {
		remaining = threshold - silent + time.Second
	}
	turn.stallTimer = time.AfterFunc(remaining, func() {
		s.checkTurnStall(threadID, turnID)
	})
}

// handleStallGracePeriod begins the grace period on first stall detection:
// logs a warning, pushes a UI alert, and schedules a final check after the grace period.
// Must be called with s.turnMu released.
func (s *Server) handleStallGracePeriod(threadID, turnID string, silent, threshold time.Duration) {
	const stallGracePeriod = 30 * time.Second

	logger.Warn("turn tracker: thinking stall detected — grace period started",
		logger.FieldThreadID, threadID,
		"turn_id", turnID,
		"silent_ms", silent.Milliseconds(),
		"threshold_ms", threshold.Milliseconds(),
		"grace_period_ms", stallGracePeriod.Milliseconds(),
	)

	if s.uiRuntime != nil {
		s.uiRuntime.PushAlert(threadID, "stall_warning",
			fmt.Sprintf("思考已 %ds 未响应，将在 %ds 后自动中断",
				int(silent.Seconds()), int(stallGracePeriod.Seconds())))
	}

	// Re-lock to schedule grace period timer.
	s.turnMu.Lock()
	turn, ok := s.activeTurns[threadID]
	if ok && turn != nil && turn.ID == turnID {
		turn.stallTimer = time.AfterFunc(stallGracePeriod, func() {
			s.checkTurnStall(threadID, turnID)
		})
	}
	s.turnMu.Unlock()
}

// executeStallAutoInterrupt performs the actual auto-interrupt after the grace period expires.
// Must be called with s.turnMu released and turn.stallAutoInterrupted already set.
func (s *Server) executeStallAutoInterrupt(threadID, turnID string, silent, threshold time.Duration) {
	logger.Warn("turn tracker: thinking stall detected — auto interrupting",
		logger.FieldThreadID, threadID,
		"turn_id", turnID,
		"silent_ms", silent.Milliseconds(),
		"threshold_ms", threshold.Milliseconds(),
	)

	if s.uiRuntime != nil {
		s.uiRuntime.PushAlert(threadID, "stall",
			fmt.Sprintf("思考超时 %ds 未响应，自动中断", int(silent.Seconds())))
	}

	// Auto-interrupt: send /interrupt to codex process (same as turnInterrupt).
	util.SafeGo(func() {
		s.markTrackedTurnInterruptRequested(threadID)
		interrupted := false
		if proc := s.mgr.Get(threadID); proc != nil {
			if err := proc.Client.SendCommand("/interrupt", ""); err != nil {
				logger.Warn("turn tracker: stall auto-interrupt failed",
					logger.FieldThreadID, threadID,
					"turn_id", turnID,
					logger.FieldError, err,
				)
			} else {
				interrupted = true
			}
		}
		// Fallback: if /interrupt failed or process is gone, force-complete the tracker.
		if !interrupted {
			if completion, ok := s.completeTrackedTurnByID(threadID, turnID, "failed", "thinking_stall_timeout"); ok {
				s.Notify("turn/completed", completion)
			}
		}
	})
}

// touchTrackedTurnLastEvent updates the LastEventAt heartbeat for the turn.
// Call this whenever any event arrives for a tracked turn.
func (s *Server) touchTrackedTurnLastEvent(threadID string) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.activeTurns == nil {
		return
	}
	turn, ok := s.activeTurns[id]
	if !ok || turn == nil {
		return
	}
	turn.LastEventAt = time.Now()
	turn.stallGraceStarted = false
}

func trackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))

	switch {
	case eventKey == "turn_aborted",
		methodKey == "turn/aborted":
		reason := extractTrackedTurnReason(payload)
		if reason == "" {
			reason = "turn_aborted"
		}
		return extractTrackedTurnID(payload), "interrupted", reason, true, false
	case methodKey == "turn/completed",
		eventKey == "turn_complete",
		eventKey == "turn/completed",
		eventKey == "idle",
		eventKey == "codex/event/task_complete",
		methodKey == "codex/event/task_complete":
		status := extractTrackedTurnStatus(payload)
		if status == "" {
			status = "completed"
		}
		reason := extractTrackedTurnReason(payload)
		if reason == "" {
			reason = "turn_complete"
		}
		return extractTrackedTurnID(payload), status, reason, true, false
	case eventKey == "stream_error",
		eventKey == "error",
		methodKey == "error",
		methodKey == "codex/event/stream_error":
		retryable, known := extractTrackedRetryable(payload)
		if known && retryable {
			return "", "", "", false, false
		}
		// willRetry 缺失 (known=false) → 不视为 terminal, codex 会自行处理。
		// 只有明确 willRetry=false 时才终止 turn。
		if !known {
			return "", "", "", false, false
		}
		reason := extractTrackedTurnReason(payload)
		if reason == "" {
			reason = util.FirstNonEmpty(
				extractTrackedString(payload, "phase"),
				eventKey,
				methodKey,
				"stream_error",
			)
		}
		return extractTrackedTurnID(payload), "failed", reason, true, true
	case methodKey == "thread/status/changed",
		eventKey == "thread/status/changed":
		status, reason, ok := threadStatusTerminalFromPayload(payload)
		if !ok {
			return "", "", "", false, false
		}
		return extractTrackedTurnID(payload), status, reason, true, true
	default:
		return "", "", "", false, false
	}
}

func extractTrackedRetryable(payload map[string]any) (bool, bool) {
	if payload == nil {
		return false, false
	}
	for _, key := range []string{"willRetry", "will_retry", "recoverable"} {
		value, exists := payload[key]
		if !exists {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "y":
				return true, true
			case "false", "0", "no", "n":
				return false, true
			}
		}
	}
	return false, false
}

func extractTrackedTurnID(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if id := extractTrackedString(turn, "id", "turnId", "turn_id"); id != "" {
			return id
		}
	}
	return extractTrackedString(payload, "turnId", "turn_id", "id")
}

func extractTrackedTurnStatus(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if status := extractTrackedString(turn, "status", "state"); status != "" {
			return status
		}
	}
	return extractTrackedString(payload, "status", "state")
}

func extractTrackedTurnReason(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if reason := extractTrackedString(turn, "reason", "message"); reason != "" {
			return reason
		}
	}
	return extractTrackedString(payload, "reason", "message")
}

func extractTrackedString(payload map[string]any, keys ...string) string {
	if payload == nil {
		return ""
	}
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}
	return ""
}

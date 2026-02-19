package apiserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const defaultTurnWatchdogTimeout = 10 * time.Minute

type trackedTurn struct {
	ID                   string
	ThreadID             string
	StartedAt            time.Time
	InterruptRequested   bool
	InterruptRequestedAt time.Time
	stallHintLogged      bool
	done                 chan string
	timer                *time.Timer
}

func (s *Server) ensureTurnTrackerLocked() {
	if s.activeTurns == nil {
		s.activeTurns = make(map[string]*trackedTurn)
	}
	if s.turnWatchdogTimeout <= 0 {
		s.turnWatchdogTimeout = defaultTurnWatchdogTimeout
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
		select {
		case prev.done <- "failed":
		default:
		}
		logger.Warn("turn tracker: superseding active turn",
			logger.FieldThreadID, id,
			"prev_turn_id", prev.ID,
			"next_turn_id", tid,
			"prev_age_ms", time.Since(prev.StartedAt).Milliseconds(),
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
		ID:        tid,
		ThreadID:  id,
		StartedAt: time.Now(),
		done:      make(chan string, 1),
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
		logger.Warn("turn tracker: turn id mismatch, skip completion",
			logger.FieldThreadID, id,
			"active_turn_id", strings.TrimSpace(turn.ID),
			"event_turn_id", wantTurnID,
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
		)
		s.turnMu.Unlock()
		return nil, false
	}
	delete(s.activeTurns, id)
	if turn.timer != nil {
		turn.timer.Stop()
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

	if synthetic {
		s.Notify("turn/completed", completion)
		return
	}
	for key, value := range completion {
		payload[key] = value
	}
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
	if age < 20*time.Second {
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
		reason := extractTrackedTurnReason(payload)
		if reason == "" {
			reason = firstTrackedTurnNonEmpty(
				extractTrackedString(payload, "phase"),
				eventKey,
				methodKey,
				"stream_error",
			)
		}
		return extractTrackedTurnID(payload), "failed", reason, true, true
	default:
		return "", "", "", false, false
	}
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

func firstTrackedTurnNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

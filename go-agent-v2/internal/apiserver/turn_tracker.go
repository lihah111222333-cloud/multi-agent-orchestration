package apiserver

import (
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const defaultTurnWatchdogTimeout = 10 * time.Minute
const defaultTrackedTurnSummaryTTL = 30 * time.Minute
const trackedTurnSummaryCacheMaxEntries = 512
const defaultStallThreshold = 480 * time.Second
const defaultStallHeartbeat = 300 * time.Second

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
		maps.Copy(currentTurn, completionTurn)
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
	return codexadapter.NormalizeTrackedTurnStatus(status)
}

func trackedTurnPayloadDiagKV(payload map[string]any) []any {
	if payload == nil {
		return []any{"payload_nil", true}
	}

	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	const maxKeySample = 12
	keysTruncated := false
	if len(keys) > maxKeySample {
		keys = keys[:maxKeySample]
		keysTruncated = true
	}
	_, hasTurnObj := payload["turn"].(map[string]any)

	return []any{
		"payload_key_count", len(payload),
		"payload_keys_sample", strings.Join(keys, ","),
		"payload_keys_truncated", keysTruncated,
		"payload_has_turn_obj", hasTurnObj,
		"payload_turn_id", extractTrackedTurnID(payload),
		"payload_turn_status", extractTrackedTurnStatus(payload),
		"payload_turn_reason", extractTrackedTurnReason(payload),
		"payload_status_raw", extractTrackedString(payload, "status", "state"),
		"payload_reason_raw", extractTrackedString(payload, "reason", "message"),
	}
}

// checkTurnStall is called periodically by the stall timer.
// If no events have been received for the configured stall threshold, it pushes
// an alert and auto-interrupts the turn.

// rescheduleStallCheck schedules the next stall check timer.
// Must be called with s.turnMu held.

// handleStallGracePeriod begins the grace period on first stall detection:
// logs a warning, pushes a UI alert, and schedules a final check after the grace period.
// Must be called with s.turnMu released.

// executeStallAutoInterrupt performs the actual auto-interrupt after the grace period expires.
// Must be called with s.turnMu released and turn.stallAutoInterrupted already set.

// touchTrackedTurnLastEvent updates the LastEventAt heartbeat for the turn.
// Call this whenever any event arrives for a tracked turn.

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

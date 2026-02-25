package codexadapter

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// normalizeTrackedTurnStatus maps raw turn status strings to canonical values.
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

// extractTrackedString returns first non-empty trimmed string value by keys.
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

// ExtractTrackedString exposes tracked string extraction through adapter APIs.
func (a *Adapter) ExtractTrackedString(payload map[string]any, keys ...string) string {
	return extractTrackedString(payload, keys...)
}

// extractTrackedRetryable reads retryability hint from event payload.
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

// extractTrackedTurnID reads turn id from payload.
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

// extractTrackedTurnStatus reads turn status from payload.
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

// extractTrackedTurnReason reads turn reason from payload.
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

// threadStatusTerminalFromPayload parses thread/status/changed payload terminal status.
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

// trackedTurnTerminalFromEvent maps incoming event to tracked turn terminal state.
func trackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))

	switch {
	case eventKey == "turn_aborted", methodKey == "turn/aborted":
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
	case methodKey == "thread/status/changed", eventKey == "thread/status/changed":
		status, reason, ok := threadStatusTerminalFromPayload(payload)
		if !ok {
			return "", "", "", false, false
		}
		return extractTrackedTurnID(payload), status, reason, true, true
	default:
		return "", "", "", false, false
	}
}

// TrackedTurnTerminalFromEvent exposes terminal event classification through adapter APIs.
func (a *Adapter) TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	return trackedTurnTerminalFromEvent(eventType, method, payload)
}

// isTerminalEventType reports whether event type or method indicates a turn terminal event.
func isTerminalEventType(eventType, method string) bool {
	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))
	switch {
	case eventKey == "turn_complete" || eventKey == "turn/completed" || eventKey == "idle" ||
		eventKey == "turn_aborted" || eventKey == "codex/event/task_complete" ||
		eventKey == "shutdown_complete":
		return true
	case methodKey == "turn/completed" || methodKey == "turn/aborted" ||
		methodKey == "codex/event/task_complete" || methodKey == "thread/status/changed":
		return true
	case eventKey == "error" || eventKey == "stream_error" ||
		methodKey == "error" || methodKey == "codex/event/stream_error":
		return true
	default:
		return false
	}
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

func trackedTurnSummaryCacheKey(threadID, turnID string) string {
	return strings.TrimSpace(threadID) + "\x00" + strings.TrimSpace(turnID)
}

// pruneTrackedTurnSummaryCacheLocked removes expired/overflow entries.
func pruneTrackedTurnSummaryCacheLocked(cache map[string]trackedTurnSummaryCacheEntry, now time.Time, ttl time.Duration, maxEntries int) {
	if cache == nil {
		return
	}
	if ttl > 0 {
		for key, entry := range cache {
			if entry.UpdatedAt.IsZero() || now.Sub(entry.UpdatedAt) > ttl {
				delete(cache, key)
			}
		}
	}
	if maxEntries <= 0 || len(cache) <= maxEntries {
		return
	}
	keys := make([]string, 0, len(cache))
	for key := range cache {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left := cache[keys[i]].UpdatedAt
		right := cache[keys[j]].UpdatedAt
		if !left.Equal(right) {
			return left.Before(right)
		}
		return keys[i] < keys[j]
	})
	for len(keys) > maxEntries {
		delete(cache, keys[0])
		keys = keys[1:]
	}
}

// rememberTrackedTurnSummary stores summary in per-turn cache.
func rememberTrackedTurnSummary(state turnTrackerState, turnMu *sync.Mutex, threadID, turnID, summary string) {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	text := strings.TrimSpace(summary)
	if id == "" || tid == "" || text == "" {
		return
	}
	if turnMu != nil {
		turnMu.Lock()
		defer turnMu.Unlock()
	}
	ensureTurnTrackerStateLocked(state)
	if state.TurnSummaryCache == nil {
		return
	}
	cache := *state.TurnSummaryCache
	if cache == nil {
		cache = make(map[string]trackedTurnSummaryCacheEntry)
		*state.TurnSummaryCache = cache
	}
	cache[trackedTurnSummaryCacheKey(id, tid)] = trackedTurnSummaryCacheEntry{TurnID: tid, Summary: text, UpdatedAt: time.Now()}
	ttl := DefaultTrackedTurnSummaryTTL
	if state.TurnSummaryTTL != nil && *state.TurnSummaryTTL > 0 {
		ttl = *state.TurnSummaryTTL
	}
	pruneTrackedTurnSummaryCacheLocked(cache, time.Now(), ttl, TrackedTurnSummaryCacheMaxEntries)
}

// lookupTrackedTurnSummary retrieves summary from per-turn cache.
func lookupTrackedTurnSummary(state turnTrackerState, turnMu *sync.Mutex, threadID, turnID string) string {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	if id == "" || tid == "" {
		return ""
	}
	if turnMu != nil {
		turnMu.Lock()
		defer turnMu.Unlock()
	}
	if state.TurnSummaryCache == nil {
		return ""
	}
	cache := *state.TurnSummaryCache
	if cache == nil {
		return ""
	}
	entry, ok := cache[trackedTurnSummaryCacheKey(id, tid)]
	if !ok {
		return ""
	}
	return strings.TrimSpace(entry.Summary)
}

// trackedTurnSummaryFromPayload extracts summary text from terminal payload.
func trackedTurnSummaryFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if summary := extractTrackedString(payload, "lastAgentMessage", "last_agent_message", "summary", "result", "message"); summary != "" {
		return summary
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if summary := extractTrackedString(turn, "lastAgentMessage", "last_agent_message", "summary", "result", "message"); summary != "" {
			return summary
		}
	}
	if msg, ok := payload["msg"].(map[string]any); ok {
		if summary := extractTrackedString(msg, "lastAgentMessage", "last_agent_message", "summary", "result", "message"); summary != "" {
			return summary
		}
	}
	return ""
}

// TrackedTurnSummaryFromPayload exposes summary extraction through adapter APIs.
func (a *Adapter) TrackedTurnSummaryFromPayload(payload map[string]any) string {
	return trackedTurnSummaryFromPayload(payload)
}

// injectTrackedTurnSummary injects summary into completion payload.
func injectTrackedTurnSummary(payload map[string]any, summary string) {
	if payload == nil {
		return
	}
	text := strings.TrimSpace(summary)
	if text == "" {
		return
	}
	if strings.TrimSpace(extractTrackedString(payload, "lastAgentMessage")) == "" {
		payload["lastAgentMessage"] = text
	}
	if strings.TrimSpace(extractTrackedString(payload, "summary")) == "" {
		payload["summary"] = text
	}
	turn, ok := payload["turn"].(map[string]any)
	if !ok || turn == nil {
		turn = map[string]any{}
		payload["turn"] = turn
	}
	if strings.TrimSpace(extractTrackedString(turn, "lastAgentMessage")) == "" {
		turn["lastAgentMessage"] = text
	}
	if strings.TrimSpace(extractTrackedString(turn, "summary")) == "" {
		turn["summary"] = text
	}
}

// mergeTrackedTurnCompletionPayload merges tracker completion fields into terminal payload.
func mergeTrackedTurnCompletionPayload(target map[string]any, completion map[string]any) {
	if target == nil || completion == nil {
		return
	}
	for _, key := range []string{"threadId", "status", "reason", "summary"} {
		if value, ok := completion[key]; ok {
			target[key] = value
		}
	}
	if completionTurn, ok := completion["turn"].(map[string]any); ok {
		targetTurn, ok := target["turn"].(map[string]any)
		if !ok || targetTurn == nil {
			targetTurn = map[string]any{}
			target["turn"] = targetTurn
		}
		for _, key := range []string{"id", "status", "reason", "summary"} {
			if value, ok := completionTurn[key]; ok {
				targetTurn[key] = value
			}
		}
	}
}

// rememberTrackedTurnSummary records summary into adapter-owned cache state.
func (a *Adapter) rememberTrackedTurnSummary(threadID, turnID, summary string) {
	if a == nil {
		return
	}
	state := a.trackerHelperState()
	rememberTrackedTurnSummary(state, state.Mu, threadID, turnID, summary)
}

// lookupTrackedTurnSummary reads tracked-turn summary from adapter-owned cache state.
func (a *Adapter) lookupTrackedTurnSummary(threadID, turnID string) string {
	if a == nil {
		return ""
	}
	state := a.trackerHelperState()
	return lookupTrackedTurnSummary(state, state.Mu, threadID, turnID)
}

func resolveTurnIDFromPayload(_ string, payload map[string]any) string {
	return extractTrackedTurnID(payload)
}

// captureAndInjectTurnSummary captures terminal summaries and injects them into completion payloads.
func captureAndInjectTurnSummary(
	threadID string,
	eventType string,
	method string,
	payload map[string]any,
	resolveTurnID func(threadID string, payload map[string]any) string,
	rememberTrackedTurnSummary func(threadID, turnID, summary string),
	lookupTrackedTurnSummary func(threadID, turnID string) string,
) {
	if payload == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}

	resolve := resolveTurnID
	if resolve == nil {
		resolve = resolveTurnIDFromPayload
	}
	remember := rememberTrackedTurnSummary
	if remember == nil {
		remember = func(string, string, string) {}
	}
	lookup := lookupTrackedTurnSummary
	if lookup == nil {
		lookup = func(string, string) string { return "" }
	}

	turnID := strings.TrimSpace(resolve(id, payload))
	summary := trackedTurnSummaryFromPayload(payload)
	if summary != "" {
		_, _, _, terminal, _ := trackedTurnTerminalFromEvent(eventType, method, payload)
		methodKey := strings.ToLower(strings.TrimSpace(method))
		eventKey := strings.ToLower(strings.TrimSpace(eventType))
		if terminal || methodKey == "codex/event/task_complete" || eventKey == "codex/event/task_complete" {
			remember(id, turnID, summary)
		}
	}

	if !strings.EqualFold(strings.TrimSpace(method), "turn/completed") {
		return
	}
	if summary == "" {
		summary = lookup(id, turnID)
	}
	if summary == "" {
		return
	}
	injectTrackedTurnSummary(payload, summary)
	remember(id, turnID, summary)
}

// CaptureAndInjectTurnSummary captures terminal summaries and injects them into completion payloads.
func (a *Adapter) CaptureAndInjectTurnSummary(threadID, eventType, method string, payload map[string]any) {
	if a == nil {
		return
	}
	resolveTurnID := func(id string, source map[string]any) string {
		turnID := extractTrackedTurnID(source)
		if turnID != "" {
			return turnID
		}
		activeTurnID, _, _, ok := a.peekTrackedTurnMeta(id)
		if ok {
			return activeTurnID
		}
		return ""
	}
	captureAndInjectTurnSummary(threadID, eventType, method, payload, resolveTurnID, a.rememberTrackedTurnSummary, a.lookupTrackedTurnSummary)
}

func maybeFinalizeDiagFields(
	threadID string,
	trackedTurnID string,
	eventTurnID string,
	eventType string,
	method string,
	status string,
	reason string,
	payload map[string]any,
	extra ...any,
) []any {
	fields := append(threadLogFields(threadID),
		"tracked_turn_id", strings.TrimSpace(trackedTurnID),
		"event_turn_id", strings.TrimSpace(eventTurnID),
		logger.FieldStatus, strings.TrimSpace(status),
		"reason", strings.TrimSpace(reason),
		logger.FieldEventType, strings.TrimSpace(eventType),
		logger.FieldMethod, strings.TrimSpace(method),
	)
	fields = append(fields, extra...)
	fields = append(fields, trackedTurnPayloadDiagKV(payload)...)
	return fields
}

// maybeFinalizeTrackedTurn applies turn-terminal events to tracked-turn state.
func (a *Adapter) maybeFinalizeTrackedTurn(threadID, eventType, method string, payload map[string]any) {
	if a == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}

	turnID, startedAt, interruptRequested, ok := a.peekTrackedTurnMeta(id)
	if !ok {
		return
	}

	eventTurnID, status, reason, terminal, synthetic := trackedTurnTerminalFromEvent(eventType, method, payload)
	if !terminal {
		if shouldLogTrackedTurnStallHint(eventType, method, startedAt) && a.markTrackedTurnStallHint(id, turnID) {
			logger.Warn("turn tracker: active turn not terminal yet at tail event", maybeFinalizeDiagFields(
				id,
				turnID,
				eventTurnID,
				eventType,
				method,
				status,
				reason,
				payload,
				"turn_age_ms", time.Since(startedAt).Milliseconds(),
				"interrupt_requested", interruptRequested,
			)...)
		}
		return
	}

	if strings.TrimSpace(eventTurnID) == "" {
		logger.Warn("turn tracker: terminal event missing turn_id", maybeFinalizeDiagFields(
			id,
			turnID,
			eventTurnID,
			eventType,
			method,
			status,
			reason,
			payload,
		)...)
	}

	completion, completed := a.completeTrackedTurnByID(id, eventTurnID, status, reason)
	if !completed {
		logger.Warn("turn tracker: terminal event failed to close tracked turn", maybeFinalizeDiagFields(
			id,
			turnID,
			eventTurnID,
			eventType,
			method,
			status,
			reason,
			payload,
		)...)
		return
	}

	logger.Info("turn tracker: finalized by event", append(threadLogFields(id),
		"tracked_turn_id", turnID,
		"event_turn_id", eventTurnID,
		logger.FieldStatus, strings.TrimSpace(status),
		"reason", strings.TrimSpace(reason),
		"synthetic", synthetic,
		logger.FieldEventType, strings.TrimSpace(eventType),
		logger.FieldMethod, strings.TrimSpace(method),
	)...)

	summary := trackedTurnSummaryFromPayload(payload)
	if summary == "" {
		summary = a.lookupTrackedTurnSummary(id, util.FirstNonEmpty(eventTurnID, extractTrackedTurnID(payload), turnID))
	}
	if summary != "" {
		injectTrackedTurnSummary(completion, summary)
		a.rememberTrackedTurnSummary(id, util.FirstNonEmpty(extractTrackedTurnID(completion), eventTurnID, extractTrackedTurnID(payload)), summary)
	}

	if synthetic {
		if notify := a.trackerNotify(); notify != nil {
			notify("turn/completed", completion)
		}
		return
	}
	mergeTrackedTurnCompletionPayload(payload, completion)
}

// FinalizeTrackedTurnEvent updates heartbeat and finalizes turn state from an incoming event.
func (a *Adapter) FinalizeTrackedTurnEvent(
	threadID string,
	eventType string,
	method string,
	payload map[string]any,
) {
	if a == nil {
		return
	}
	a.touchTrackedTurnLastEvent(threadID)
	a.maybeFinalizeTrackedTurn(threadID, eventType, method, payload)
}

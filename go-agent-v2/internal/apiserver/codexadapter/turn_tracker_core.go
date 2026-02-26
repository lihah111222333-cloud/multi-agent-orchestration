package codexadapter

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)


// trackedTurnSummaryCacheEntry caches the latest summary for one tracked turn.
type trackedTurnSummaryCacheEntry struct {
	TurnID    string
	Summary   string
	UpdatedAt time.Time
}

// turnTrackerState carries mutable turn-tracker state owned by the caller.
type turnTrackerState struct {
	Mu                  *sync.Mutex
	ActiveTurns         *map[string]*trackedTurn
	TurnWatchdogTimeout *time.Duration
	TurnSummaryCache    *map[string]trackedTurnSummaryCacheEntry
	TurnSummaryTTL      *time.Duration
	stallThreshold      *time.Duration
	stallHeartbeat      *time.Duration
}

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

func resolveTurnIDFromPayload(_ string, payload map[string]any) string {
	return extractTrackedTurnID(payload)
}

// captureAndInjectTurnSummary captures terminal summaries and injects them into completion payloads.
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


func withTrackerStateLockCore(state turnTrackerState, fn func(turnTrackerState)) {
	if fn == nil {
		return
	}
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	ensureTurnTrackerStateLocked(state)
	fn(state)
}

func trackerDurationCore(state turnTrackerState, getter func(turnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	if getter == nil {
		return fallback
	}
	value := fallback
	withTrackerStateLockCore(state, func(lockedState turnTrackerState) {
		value = trackerDurationOrDefault(getter(lockedState), fallback)
	})
	return value
}

func setTrackerDurationCore(state turnTrackerState, getter func(turnTrackerState) *time.Duration, value time.Duration) {
	if value <= 0 || getter == nil {
		return
	}
	withTrackerStateLockCore(state, func(lockedState turnTrackerState) {
		target := getter(lockedState)
		if target != nil {
			*target = value
		}
	})
}

func trackerStateCore(state turnTrackerState) (map[string]*trackedTurn, *sync.Mutex, time.Duration, time.Duration) {
	var activeTurns map[string]*trackedTurn
	if state.ActiveTurns != nil {
		activeTurns = *state.ActiveTurns
	}
	turnMu := state.Mu
	watchdogTimeout := trackerDurationOrDefault(state.TurnWatchdogTimeout, DefaultTurnWatchdogTimeout)
	stallThreshold := trackerDurationOrDefault(state.stallThreshold, defaultStallThreshold)
	return activeTurns, turnMu, watchdogTimeout, stallThreshold
}

func applyTrackedTurnTransitionCore(state turnTrackerState, threadID string, req trackedTurnTransitionRequest) trackedTurnTransitionResult {
	result := trackedTurnTransitionResult{}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return result
	}

	activeTurns, turnMu, _, _ := trackerStateCore(state)
	if turnMu == nil || activeTurns == nil {
		return result
	}

	turnMu.Lock()
	defer turnMu.Unlock()
	ensureTurnTrackerStateLocked(state)

	turn, ok := activeTurns[id]
	if !ok || turn == nil {
		return result
	}

	result.Found = true
	result.ThreadID = id
	result.TurnID = strings.TrimSpace(turn.ID)
	result.StartedAt = turn.StartedAt
	result.LastEventAt = turn.LastEventAt
	result.InterruptRequested = turn.InterruptRequested
	result.StallHintLogged = turn.StallHintLogged

	if req.TouchHeartbeat {
		turn.LastEventAt = time.Now()
		result.LastEventAt = turn.LastEventAt
	}
	if req.MarkInterruptRequested {
		turn.InterruptRequested = true
		turn.InterruptRequestedAt = time.Now()
		result.InterruptRequested = true
	}
	if req.MarkStallHint {
		wantTurnID := strings.TrimSpace(req.MarkStallHintForTurnID)
		if wantTurnID != "" && !strings.EqualFold(result.TurnID, wantTurnID) {
			return result
		}
		if !turn.StallHintLogged {
			turn.StallHintLogged = true
			result.StallHintLogged = true
			result.StallHintApplied = true
		}
	}

	if req.Finalize != nil {
		wantTurnID := strings.TrimSpace(req.Finalize.TurnID)
		result.ExpectedTurnID = wantTurnID
		if wantTurnID != "" && !strings.EqualFold(result.TurnID, wantTurnID) {
			result.TurnIDMismatch = true
		}

		delete(activeTurns, id)
		if turn.Timer != nil {
			turn.Timer.Stop()
		}
		if turn.StallTimer != nil {
			turn.StallTimer.Stop()
		}

		finalStatus := normalizeTrackedTurnStatus(req.Finalize.Status)
		if turn.InterruptRequested && finalStatus == "completed" {
			finalStatus = "interrupted"
		}
		if turn.Done != nil {
			select {
			case turn.Done <- finalStatus:
			default:
			}
		}
		reasonText := strings.TrimSpace(req.Finalize.Reason)
		result.Finalized = true
		result.FinalStatus = finalStatus
		result.FinalReason = reasonText
		result.Completion = map[string]any{
			"threadId": id,
			"turn": map[string]any{
				"id":     result.TurnID,
				"status": finalStatus,
			},
			"status": finalStatus,
			"reason": reasonText,
		}
	}

	return result
}

func withActiveTurnCore(state turnTrackerState, threadID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	activeTurns, turnMu, _, _ := trackerStateCore(state)
	id := strings.TrimSpace(threadID)
	if id == "" || turnMu == nil || fn == nil {
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
	return fn(id, turn, activeTurns)
}

func withActiveTurnByIDCore(state turnTrackerState, threadID, turnID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	expectedTurnID := strings.TrimSpace(turnID)
	if expectedTurnID == "" || fn == nil {
		return false
	}
	return withActiveTurnCore(state, threadID, func(id string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool {
		if !strings.EqualFold(strings.TrimSpace(turn.ID), expectedTurnID) {
			return false
		}
		return fn(id, turn, activeTurns)
	})
}

func beginTrackedTurnCore(
	state turnTrackerState,
	threadID string,
	turnID string,
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
	checkTurnStall func(string, string),
) string {
	activeTurns, turnMu, watchdogTimeout, stallThreshold := trackerStateCore(state)

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
	ensureTurnTrackerStateLocked(state)
	superseded, prevTurnID, hadPrevTurn = supersedeActiveTurn(activeTurns, id, tid)

	now := time.Now()
	turn := &trackedTurn{
		ID:          tid,
		ThreadID:    id,
		StartedAt:   now,
		LastEventAt: now,
		Done:        make(chan string, 1),
	}

	watchdogTurnID := tid
	watchdogThreadID := id
	watchdogStartedAt := turn.StartedAt
	turn.Timer = time.AfterFunc(watchdogTimeout, func() {
		logger.Warn("turn tracker: watchdog timeout reached", append(threadLogFields(watchdogThreadID),
			logger.FieldTurnID, watchdogTurnID,
			"watchdog_timeout_ms", watchdogTimeout.Milliseconds(),
			"turn_age_ms", time.Since(watchdogStartedAt).Milliseconds(),
		)...)
		if notify == nil || completeTrackedTurnByID == nil {
			return
		}
		if completion, ok := completeTrackedTurnByID(watchdogThreadID, watchdogTurnID, "failed", "watchdog_timeout"); ok {
			notify("turn/completed", completion)
		}
	})

	activeTurns[id] = turn
	if checkTurnStall != nil {
		stallInterval := max(stallThreshold/3, 10*time.Second)
		turn.StallTimer = time.AfterFunc(stallInterval, func() {
			checkTurnStall(id, tid)
		})
	}
	turnMu.Unlock()

	logger.Info("turn tracker: begin turn tracking", append(threadLogFields(id),
		logger.FieldTurnID, tid,
		"source_turn_id", strings.TrimSpace(turnID),
		"watchdog_timeout_ms", watchdogTimeout.Milliseconds(),
		"had_prev_turn", hadPrevTurn,
		"prev_turn_id", prevTurnID,
	)...)

	if superseded != nil && notify != nil {
		notify("turn/completed", superseded)
	}
	return tid
}

func waitTrackedTurnTerminalCore(state turnTrackerState, threadID string, timeout time.Duration) (string, bool) {
	if timeout <= 0 {
		return "", false
	}
	var done chan string
	ok := withActiveTurnCore(state, threadID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		if turn.Done == nil {
			return false
		}
		done = turn.Done
		return true
	})
	if !ok || done == nil {
		return "", false
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case status := <-done:
		return normalizeTrackedTurnStatus(status), true
	case <-timer.C:
		return "", false
	}
}

func completeTrackedTurnByIDCore(state turnTrackerState, threadID, turnID, status, reason string) (map[string]any, bool) {
	transition := applyTrackedTurnTransitionCore(state, threadID, trackedTurnTransitionRequest{
		Finalize: &trackedTurnFinalizeRequest{
			TurnID: turnID,
			Status: status,
			Reason: reason,
		},
	})
	if !transition.Finalized || transition.Completion == nil {
		return nil, false
	}

	if transition.TurnIDMismatch {
		logger.Info("turn tracker: turn id mismatch, completing anyway to avoid stuck state", append(threadLogFields(transition.ThreadID),
			"active_turn_id", transition.TurnID,
			"event_turn_id", transition.ExpectedTurnID,
			logger.FieldStatus, strings.TrimSpace(status),
			"reason", strings.TrimSpace(reason),
		)...)
	}

	logger.Info("turn tracker: turn completed", append(threadLogFields(transition.ThreadID),
		logger.FieldTurnID, transition.TurnID,
		logger.FieldStatus, transition.FinalStatus,
		"reason", transition.FinalReason,
		"duration_ms", time.Since(transition.StartedAt).Milliseconds(),
		"interrupt_requested", transition.InterruptRequested,
	)...)
	return transition.Completion, true
}

func peekTrackedTurnMetaCore(state turnTrackerState, threadID string) (string, time.Time, bool, bool) {
	transition := applyTrackedTurnTransitionCore(state, threadID, trackedTurnTransitionRequest{})
	if !transition.Found {
		return "", time.Time{}, false, false
	}
	return transition.TurnID, transition.StartedAt, transition.InterruptRequested, true
}

func markTrackedTurnStallHintCore(state turnTrackerState, threadID, turnID string) bool {
	transition := applyTrackedTurnTransitionCore(state, threadID, trackedTurnTransitionRequest{
		MarkStallHint:          true,
		MarkStallHintForTurnID: strings.TrimSpace(turnID),
	})
	return transition.StallHintApplied
}

func touchTrackedTurnLastEventCore(state turnTrackerState, threadID string) {
	applyTrackedTurnTransitionCore(state, threadID, trackedTurnTransitionRequest{TouchHeartbeat: true})
}

func nextTrackedTurnStallDecisionCore(
	state turnTrackerState,
	threadID string,
	turnID string,
	stallThreshold time.Duration,
	checkTurnStall func(string, string),
) trackedTurnStallDecision {
	decision := trackedTurnStallDecision{Action: trackedTurnStallNoop}
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	if id == "" || tid == "" {
		return decision
	}

	threshold := stallThreshold
	if threshold <= 0 {
		threshold = defaultStallThreshold
	}

	withActiveTurnByIDCore(state, id, tid, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		currentTurnID := strings.TrimSpace(turn.ID)
		silent := time.Since(turn.LastEventAt)
		decision.ThreadID = id
		decision.TurnID = currentTurnID
		decision.Silent = silent
		decision.Threshold = threshold

		if silent < threshold {
			rescheduleStallCheck(turn, id, currentTurnID, silent, threshold, checkTurnStall)
			decision.Action = trackedTurnStallRescheduled
			return true
		}
		if turn.StallAutoInterrupted {
			return true
		}
		if !turn.StallGraceStarted {
			turn.StallGraceStarted = true
			decision.Action = trackedTurnStallEnterGrace
			return true
		}

		turn.StallAutoInterrupted = true
		decision.Action = trackedTurnStallAutoInterrupt
		return true
	})

	return decision
}

func checkTurnStallCore(
	state turnTrackerState,
	threadID string,
	turnID string,
	handleStallGracePeriod func(threadID, turnID string, silent, threshold time.Duration),
	executeStallAutoInterrupt func(threadID, turnID string, silent, threshold time.Duration),
	checkTurnStall func(string, string),
) {
	_, _, _, stallThreshold := trackerStateCore(state)
	decision := nextTrackedTurnStallDecisionCore(state, threadID, turnID, stallThreshold, checkTurnStall)
	switch decision.Action {
	case trackedTurnStallRescheduled, trackedTurnStallNoop:
		return
	case trackedTurnStallEnterGrace:
		if handleStallGracePeriod != nil {
			handleStallGracePeriod(decision.ThreadID, decision.TurnID, decision.Silent, decision.Threshold)
		}
	case trackedTurnStallAutoInterrupt:
		if executeStallAutoInterrupt != nil {
			executeStallAutoInterrupt(decision.ThreadID, decision.TurnID, decision.Silent, decision.Threshold)
		}
	}
}

func handleStallGracePeriodCore(
	state turnTrackerState,
	threadID string,
	turnID string,
	silent time.Duration,
	threshold time.Duration,
	pushAlert func(threadID, category, message string),
	checkTurnStall func(string, string),
) {
	logger.Warn("turn tracker: stall detected (grace period)", append(threadLogFields(threadID),
		logger.FieldTurnID, turnID,
		"silent_ms", silent.Milliseconds(),
		"threshold_ms", threshold.Milliseconds(),
		"grace_ms", (30 * time.Second).Milliseconds(),
	)...)

	if pushAlert != nil {
		pushAlert(threadID, "stall_warning", "长时间无事件，若持续将自动中断")
	}

	withActiveTurnByIDCore(state, threadID, turnID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		turn.StallTimer = time.AfterFunc(30*time.Second, func() {
			if checkTurnStall != nil {
				checkTurnStall(threadID, turnID)
			}
		})
		return true
	})
}

type trackerAlertRuntime interface {
	PushAlert(threadID, category, message string)
}

func trackerRuntimePushAlert(runtime trackerAlertRuntime) func(threadID, category, message string) {
	if runtime == nil {
		return nil
	}
	return runtime.PushAlert
}

func trackerInterruptSender(manager *runner.AgentManager, sendCommand func(*runner.AgentProcess, string, string) error) func(string) (bool, error) {
	if manager == nil || sendCommand == nil {
		return nil
	}
	return func(threadID string) (bool, error) {
		proc := manager.Get(threadID)
		if proc == nil {
			return false, nil
		}
		if err := sendCommand(proc, "/interrupt", ""); err != nil {
			return true, err
		}
		return true, nil
	}
}

func executeStallAutoInterruptCore(
	threadID string,
	turnID string,
	silent time.Duration,
	threshold time.Duration,
	pushAlert func(threadID, category, message string),
	markTrackedTurnInterruptRequested func(string) bool,
	cancelCodeRuns func(string) int,
	sendInterrupt func(string) (bool, error),
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
) {
	logger.Warn("turn tracker: thinking stall detected - auto interrupting", append(threadLogFields(threadID),
		logger.FieldTurnID, turnID,
		"silent_ms", silent.Milliseconds(),
		"threshold_ms", threshold.Milliseconds(),
	)...)

	if pushAlert != nil {
		pushAlert(threadID, "stall", fmt.Sprintf("思考超时 %ds 未响应，自动中断", int(silent.Seconds())))
	}

	util.SafeGo(func() {
		if markTrackedTurnInterruptRequested != nil {
			markTrackedTurnInterruptRequested(threadID)
		}
		if cancelCodeRuns != nil {
			if cancelled := cancelCodeRuns(threadID); cancelled > 0 {
				logger.Info("turn tracker: cancelled running code_run executions", append(threadLogFields(threadID),
					logger.FieldTurnID, turnID,
					"cancelled_runs", cancelled,
				)...)
			}
		}

		interrupted := false
		if sendInterrupt != nil {
			attempted, err := sendInterrupt(threadID)
			if err != nil {
				logger.Warn("turn tracker: stall auto-interrupt failed", append(threadLogFields(threadID),
					logger.FieldTurnID, turnID,
					logger.FieldError, err,
				)...)
			}
			interrupted = attempted && err == nil
		}
		if !interrupted && notify != nil && completeTrackedTurnByID != nil {
			if completion, ok := completeTrackedTurnByID(threadID, turnID, "failed", "thinking_stall_timeout"); ok {
				notify("turn/completed", completion)
			}
		}
	})
}

func captureAndInjectTurnSummaryCore(state turnTrackerState, threadID, eventType, method string, payload map[string]any) {
	resolveTurnID := func(id string, source map[string]any) string {
		turnID := extractTrackedTurnID(source)
		if turnID != "" {
			return turnID
		}
		activeTurnID, _, _, ok := peekTrackedTurnMetaCore(state, id)
		if ok {
			return activeTurnID
		}
		return ""
	}
	captureAndInjectTurnSummary(
		threadID,
		eventType,
		method,
		payload,
		resolveTurnID,
		func(id, turnID, summary string) {
			rememberTrackedTurnSummary(state, state.Mu, id, turnID, summary)
		},
		func(id, turnID string) string {
			return lookupTrackedTurnSummary(state, state.Mu, id, turnID)
		},
	)
}

func maybeFinalizeTrackedTurnCore(
	state turnTrackerState,
	threadID string,
	eventType string,
	method string,
	payload map[string]any,
	notify func(string, any),
) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}

	turnID, startedAt, interruptRequested, ok := peekTrackedTurnMetaCore(state, id)
	if !ok {
		return
	}

	eventTurnID, status, reason, terminal, synthetic := trackedTurnTerminalFromEvent(eventType, method, payload)
	if !terminal {
		if shouldLogTrackedTurnStallHint(eventType, method, startedAt) && markTrackedTurnStallHintCore(state, id, turnID) {
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

	completion, completed := completeTrackedTurnByIDCore(state, id, eventTurnID, status, reason)
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
		summary = lookupTrackedTurnSummary(state, state.Mu, id, util.FirstNonEmpty(eventTurnID, extractTrackedTurnID(payload), turnID))
	}
	if summary != "" {
		injectTrackedTurnSummary(completion, summary)
		rememberTrackedTurnSummary(state, state.Mu, id, util.FirstNonEmpty(extractTrackedTurnID(completion), eventTurnID, extractTrackedTurnID(payload)), summary)
	}

	if synthetic {
		if notify != nil {
			notify("turn/completed", completion)
		}
		return
	}
	mergeTrackedTurnCompletionPayload(payload, completion)
}

func finalizeTrackedTurnEventCore(state turnTrackerState, threadID, eventType, method string, payload map[string]any, notify func(string, any)) {
	touchTrackedTurnLastEventCore(state, threadID)
	maybeFinalizeTrackedTurnCore(state, threadID, eventType, method, payload, notify)
}

package codexadapter

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	DefaultTurnWatchdogTimeout        = 10 * time.Minute
	DefaultTrackedTurnSummaryTTL      = 30 * time.Minute
	TrackedTurnSummaryCacheMaxEntries = 512
	DefaultStallThreshold             = 480 * time.Second
	DefaultStallHeartbeat             = 300 * time.Second
)

// TrackedTurnSummaryCacheEntry caches the latest summary for one tracked turn.
type TrackedTurnSummaryCacheEntry = contracts.TrackedTurnSummaryCacheEntry

// TurnTrackerState carries mutable turn-tracker state owned by the caller.
type TurnTrackerState struct {
	Mu                  *sync.Mutex
	ActiveTurns         *map[string]*TrackedTurn
	TurnWatchdogTimeout *time.Duration
	TurnSummaryCache    *map[string]TrackedTurnSummaryCacheEntry
	TurnSummaryTTL      *time.Duration
	StallThreshold      *time.Duration
	StallHeartbeat      *time.Duration
}

// EnsureTurnTrackerStateLocked initializes turn-tracker state and defaults.
func EnsureTurnTrackerStateLocked(state TurnTrackerState) {
	if state.ActiveTurns != nil && *state.ActiveTurns == nil {
		*state.ActiveTurns = make(map[string]*TrackedTurn)
	}
	if state.TurnWatchdogTimeout != nil && *state.TurnWatchdogTimeout <= 0 {
		*state.TurnWatchdogTimeout = DefaultTurnWatchdogTimeout
	}
	if state.TurnSummaryCache != nil && *state.TurnSummaryCache == nil {
		*state.TurnSummaryCache = make(map[string]TrackedTurnSummaryCacheEntry)
	}
	if state.TurnSummaryTTL != nil && *state.TurnSummaryTTL <= 0 {
		*state.TurnSummaryTTL = DefaultTrackedTurnSummaryTTL
	}
	if state.StallThreshold != nil && *state.StallThreshold <= 0 {
		*state.StallThreshold = DefaultStallThreshold
	}
	if state.StallHeartbeat != nil && *state.StallHeartbeat <= 0 {
		*state.StallHeartbeat = DefaultStallHeartbeat
	}
}

func (a *Adapter) trackerHelperState() TurnTrackerState {
	if a == nil {
		return TurnTrackerState{}
	}
	return a.tracker
}

// EnsureTurnTrackerStateLocked initializes tracker defaults using adapter-owned state.
func (a *Adapter) EnsureTurnTrackerStateLocked() {
	if a == nil {
		return
	}
	EnsureTurnTrackerStateLocked(a.trackerHelperState())
}

// LookupTrackedTurnSummary reads tracked-turn summary from adapter-owned cache state.
func (a *Adapter) LookupTrackedTurnSummary(threadID, turnID string) string {
	if a == nil {
		return ""
	}
	state := a.trackerHelperState()
	return LookupTrackedTurnSummary(state, state.Mu, threadID, turnID)
}

// TouchTrackedTurnLastEvent updates turn heartbeat using adapter-owned tracker state.
func (a *Adapter) TouchTrackedTurnLastEvent(threadID string) {
	activeTurns, turnMu, _, _ := a.trackerState()
	TouchTrackedTurnLastEvent(activeTurns, turnMu, threadID)
}

// RescheduleStallCheck schedules next stall check via adapter entry.
func (a *Adapter) RescheduleStallCheck(turn *TrackedTurn, threadID, turnID string, silent, threshold time.Duration, checkTurnStall func(threadID, turnID string)) {
	RescheduleStallCheck(turn, threadID, turnID, silent, threshold, checkTurnStall)
}

// HandleStallGracePeriod starts stall grace period via adapter entry.
func (a *Adapter) HandleStallGracePeriod(
	activeTurns map[string]*TrackedTurn,
	turnMu *sync.Mutex,
	threadID,
	turnID string,
	silent,
	threshold,
	stallGracePeriod time.Duration,
	pushAlert func(threadID, category, message string),
	checkTurnStall func(threadID, turnID string),
) {
	HandleStallGracePeriod(activeTurns, turnMu, threadID, turnID, silent, threshold, stallGracePeriod, pushAlert, checkTurnStall)
}

// StartApprovalStallHeartbeat starts approval heartbeat with adapter-owned tracker state.
func (a *Adapter) StartApprovalStallHeartbeat(threadID string) func() {
	_, _, _, stallThreshold := a.trackerState()
	return StartApprovalStallHeartbeat(threadID, stallThreshold, DefaultStallThreshold, a.TouchTrackedTurnLastEvent)
}

// StartDynamicToolStallHeartbeat starts heartbeat while dynamic tools execute.
func (a *Adapter) StartDynamicToolStallHeartbeat(threadID string) func() {
	return StartApprovalStallHeartbeat(threadID, a.StallThreshold(), DefaultStallThreshold, a.TouchTrackedTurnLastEvent)
}

// StallThreshold returns current tracker stall threshold.
func (a *Adapter) StallThreshold() time.Duration {
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	if state.StallThreshold != nil && *state.StallThreshold > 0 {
		return *state.StallThreshold
	}
	return DefaultStallThreshold
}

// SetStallThreshold updates tracker stall threshold.
func (a *Adapter) SetStallThreshold(threshold time.Duration) {
	if threshold <= 0 {
		return
	}
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	EnsureTurnTrackerStateLocked(state)
	if state.StallThreshold != nil {
		*state.StallThreshold = threshold
	}
}

// StallHeartbeat returns current configured stall heartbeat interval.
func (a *Adapter) StallHeartbeat() time.Duration {
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	if state.StallHeartbeat != nil && *state.StallHeartbeat > 0 {
		return *state.StallHeartbeat
	}
	return DefaultStallHeartbeat
}

// SetStallHeartbeat updates stall heartbeat interval.
func (a *Adapter) SetStallHeartbeat(interval time.Duration) {
	if interval <= 0 {
		return
	}
	state := a.trackerHelperState()
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	EnsureTurnTrackerStateLocked(state)
	if state.StallHeartbeat != nil {
		*state.StallHeartbeat = interval
	}
}

// PeekTrackedTurnMeta reads active-turn metadata from adapter-owned tracker state.
func (a *Adapter) PeekTrackedTurnMeta(threadID string) (string, time.Time, bool, bool) {
	activeTurns, turnMu, _, _ := a.trackerState()
	return PeekTrackedTurnMeta(activeTurns, turnMu, threadID)
}

// MarkTrackedTurnStallHint marks one-shot stall hint from adapter-owned tracker state.
func (a *Adapter) MarkTrackedTurnStallHint(threadID, turnID string) bool {
	activeTurns, turnMu, _, _ := a.trackerState()
	return MarkTrackedTurnStallHint(activeTurns, turnMu, threadID, turnID)
}

// TrackedTurnSummaryFromPayload extracts last agent summary from event payload.
func TrackedTurnSummaryFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if summary := ExtractTrackedString(payload, "lastAgentMessage", "last_agent_message"); summary != "" {
		return summary
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if summary := ExtractTrackedString(turn, "lastAgentMessage", "last_agent_message"); summary != "" {
			return summary
		}
	}
	if msg, ok := payload["msg"].(map[string]any); ok {
		if summary := ExtractTrackedString(msg, "lastAgentMessage", "last_agent_message"); summary != "" {
			return summary
		}
	}
	return ""
}

// TrackedTurnSummaryFromPayload extracts summary via adapter entry.
func (a *Adapter) TrackedTurnSummaryFromPayload(payload map[string]any) string {
	return TrackedTurnSummaryFromPayload(payload)
}

// ExtractTrackedString extracts first non-empty string via adapter entry.
func (a *Adapter) ExtractTrackedString(payload map[string]any, keys ...string) string {
	return ExtractTrackedString(payload, keys...)
}

// TrackedTurnTerminalFromEvent classifies terminal event via adapter entry.
func (a *Adapter) TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (turnID, status, reason string, terminal bool, synthetic bool) {
	return TrackedTurnTerminalFromEvent(eventType, method, payload)
}

// TrackedTurnSummaryCacheKey returns summary cache key for thread + optional turn.
func TrackedTurnSummaryCacheKey(threadID, turnID string) string {
	return strings.TrimSpace(threadID) + "\x00" + strings.TrimSpace(turnID)
}

// PruneTrackedTurnSummaryCacheLocked removes expired and overflow entries.
func PruneTrackedTurnSummaryCacheLocked(cache map[string]TrackedTurnSummaryCacheEntry, ttl time.Duration, now time.Time) {
	if len(cache) == 0 {
		return
	}
	if ttl <= 0 {
		ttl = DefaultTrackedTurnSummaryTTL
	}

	cutoff := now.Add(-ttl)
	for key, entry := range cache {
		if entry.UpdatedAt.Before(cutoff) {
			delete(cache, key)
		}
	}

	if len(cache) <= TrackedTurnSummaryCacheMaxEntries {
		return
	}

	type summaryCacheKV struct {
		key       string
		updatedAt time.Time
	}
	entries := make([]summaryCacheKV, 0, len(cache))
	for key, entry := range cache {
		entries = append(entries, summaryCacheKV{key: key, updatedAt: entry.UpdatedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].updatedAt.Before(entries[j].updatedAt)
	})

	trimCount := len(cache) - TrackedTurnSummaryCacheMaxEntries
	for i := 0; i < trimCount && i < len(entries); i++ {
		delete(cache, entries[i].key)
	}
}

// RememberTrackedTurnSummary records the latest non-empty summary for thread/turn.
func RememberTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID, summary string) {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	msg := strings.TrimSpace(summary)
	if id == "" || msg == "" || turnMu == nil {
		return
	}

	now := time.Now()

	turnMu.Lock()
	defer turnMu.Unlock()
	EnsureTurnTrackerStateLocked(state)
	if state.TurnSummaryCache == nil || *state.TurnSummaryCache == nil {
		return
	}

	ttl := DefaultTrackedTurnSummaryTTL
	if state.TurnSummaryTTL != nil {
		ttl = *state.TurnSummaryTTL
	}
	cache := *state.TurnSummaryCache
	PruneTrackedTurnSummaryCacheLocked(cache, ttl, now)
	entry := TrackedTurnSummaryCacheEntry{
		TurnID:    tid,
		Summary:   msg,
		UpdatedAt: now,
	}
	cache[TrackedTurnSummaryCacheKey(id, "")] = entry
	if tid != "" {
		cache[TrackedTurnSummaryCacheKey(id, tid)] = entry
	}
}

// LookupTrackedTurnSummary finds a cached summary by thread + optional turn.
func LookupTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID string) string {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	if id == "" || turnMu == nil {
		return ""
	}

	now := time.Now()

	turnMu.Lock()
	defer turnMu.Unlock()
	EnsureTurnTrackerStateLocked(state)
	if state.TurnSummaryCache == nil || *state.TurnSummaryCache == nil {
		return ""
	}

	ttl := DefaultTrackedTurnSummaryTTL
	if state.TurnSummaryTTL != nil {
		ttl = *state.TurnSummaryTTL
	}
	cache := *state.TurnSummaryCache
	PruneTrackedTurnSummaryCacheLocked(cache, ttl, now)

	if tid != "" {
		if entry, ok := cache[TrackedTurnSummaryCacheKey(id, tid)]; ok {
			return strings.TrimSpace(entry.Summary)
		}
	}
	if entry, ok := cache[TrackedTurnSummaryCacheKey(id, "")]; ok {
		entryTurnID := strings.TrimSpace(entry.TurnID)
		if tid != "" && entryTurnID != "" && !strings.EqualFold(tid, entryTurnID) {
			return ""
		}
		return strings.TrimSpace(entry.Summary)
	}
	return ""
}

// InjectTrackedTurnSummary writes summary into top-level and turn object payload.
func InjectTrackedTurnSummary(payload map[string]any, summary string) {
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

// MergeTrackedTurnCompletionPayload merges completion payload into original event payload.
func MergeTrackedTurnCompletionPayload(payload, completion map[string]any) {
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

// ThreadStatusTerminalFromPayload parses thread/status/changed payload terminal status.
func ThreadStatusTerminalFromPayload(payload map[string]any) (status string, reason string, terminal bool) {
	if payload == nil {
		return "", "", false
	}

	statusType := ""
	switch raw := payload["status"].(type) {
	case string:
		statusType = strings.ToLower(strings.TrimSpace(raw))
	case map[string]any:
		statusType = strings.ToLower(strings.TrimSpace(ExtractTrackedString(raw, "type")))
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

// TrackedTurnTerminalFromEvent maps incoming event to tracked turn terminal state.
func TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))

	switch {
	case eventKey == "turn_aborted",
		methodKey == "turn/aborted":
		reason := ExtractTrackedTurnReason(payload)
		if reason == "" {
			reason = "turn_aborted"
		}
		return ExtractTrackedTurnID(payload), "interrupted", reason, true, false
	case methodKey == "turn/completed",
		eventKey == "turn_complete",
		eventKey == "turn/completed",
		eventKey == "idle",
		eventKey == "codex/event/task_complete",
		methodKey == "codex/event/task_complete":
		status := ExtractTrackedTurnStatus(payload)
		if status == "" {
			status = "completed"
		}
		reason := ExtractTrackedTurnReason(payload)
		if reason == "" {
			reason = "turn_complete"
		}
		return ExtractTrackedTurnID(payload), status, reason, true, false
	case eventKey == "stream_error",
		eventKey == "error",
		methodKey == "error",
		methodKey == "codex/event/stream_error":
		retryable, known := ExtractTrackedRetryable(payload)
		if known && retryable {
			return "", "", "", false, false
		}
		// willRetry 缺失 (known=false) -> 不视为 terminal, codex 会自行处理。
		// 只有明确 willRetry=false 时才终止 turn。
		if !known {
			return "", "", "", false, false
		}
		reason := ExtractTrackedTurnReason(payload)
		if reason == "" {
			reason = util.FirstNonEmpty(
				ExtractTrackedString(payload, "phase"),
				eventKey,
				methodKey,
				"stream_error",
			)
		}
		return ExtractTrackedTurnID(payload), "failed", reason, true, true
	case methodKey == "thread/status/changed",
		eventKey == "thread/status/changed":
		status, reason, ok := ThreadStatusTerminalFromPayload(payload)
		if !ok {
			return "", "", "", false, false
		}
		return ExtractTrackedTurnID(payload), status, reason, true, true
	default:
		return "", "", "", false, false
	}
}

// ExtractTrackedRetryable reads retryability hint from event payload.
func ExtractTrackedRetryable(payload map[string]any) (bool, bool) {
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

// ExtractTrackedTurnID reads turn id from payload.
func ExtractTrackedTurnID(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if id := ExtractTrackedString(turn, "id", "turnId", "turn_id"); id != "" {
			return id
		}
	}
	return ExtractTrackedString(payload, "turnId", "turn_id", "id")
}

// ExtractTrackedTurnStatus reads turn status from payload.
func ExtractTrackedTurnStatus(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if status := ExtractTrackedString(turn, "status", "state"); status != "" {
			return status
		}
	}
	return ExtractTrackedString(payload, "status", "state")
}

// ExtractTrackedTurnReason reads turn reason from payload.
func ExtractTrackedTurnReason(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if reason := ExtractTrackedString(turn, "reason", "message"); reason != "" {
			return reason
		}
	}
	return ExtractTrackedString(payload, "reason", "message")
}

// ExtractTrackedString returns first non-empty trimmed string value by keys.
func ExtractTrackedString(payload map[string]any, keys ...string) string {
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

// TrackedTurnPayloadDiagKV builds structured diagnostic key-value pairs from event payload.
func TrackedTurnPayloadDiagKV(payload map[string]any) []any {
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
		"payload_turn_id", ExtractTrackedTurnID(payload),
		"payload_turn_status", ExtractTrackedTurnStatus(payload),
		"payload_turn_reason", ExtractTrackedTurnReason(payload),
		"payload_status_raw", ExtractTrackedString(payload, "status", "state"),
		"payload_reason_raw", ExtractTrackedString(payload, "reason", "message"),
	}
}

// CaptureAndInjectTurnSummary captures summary at terminal events and injects it into turn/completed.
func CaptureAndInjectTurnSummary(
	threadID string,
	eventType string,
	method string,
	payload map[string]any,
	peekTrackedTurnMeta func(threadID string) (turnID string, startedAt time.Time, interruptRequested bool, ok bool),
	trackedTurnTerminalFromEvent func(eventType, method string, payload map[string]any) (turnID, status, reason string, terminal bool, synthetic bool),
	extractTrackedTurnID func(payload map[string]any) string,
	trackedTurnSummaryFromPayload func(payload map[string]any) string,
	rememberTrackedTurnSummary func(threadID, turnID, summary string),
	lookupTrackedTurnSummary func(threadID, turnID string) string,
	injectTrackedTurnSummary func(payload map[string]any, summary string),
) {
	if payload == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}

	terminalFromEvent := trackedTurnTerminalFromEvent
	if terminalFromEvent == nil {
		terminalFromEvent = TrackedTurnTerminalFromEvent
	}
	extractTurnID := extractTrackedTurnID
	if extractTurnID == nil {
		extractTurnID = ExtractTrackedTurnID
	}
	summaryFromPayload := trackedTurnSummaryFromPayload
	if summaryFromPayload == nil {
		summaryFromPayload = TrackedTurnSummaryFromPayload
	}
	rememberSummary := rememberTrackedTurnSummary
	if rememberSummary == nil {
		rememberSummary = func(string, string, string) {}
	}
	lookupSummary := lookupTrackedTurnSummary
	if lookupSummary == nil {
		lookupSummary = func(string, string) string { return "" }
	}
	injectSummary := injectTrackedTurnSummary
	if injectSummary == nil {
		injectSummary = InjectTrackedTurnSummary
	}

	turnID := extractTurnID(payload)
	resolvedTurnID := turnID
	if resolvedTurnID == "" && peekTrackedTurnMeta != nil {
		if activeTurnID, _, _, ok := peekTrackedTurnMeta(id); ok {
			resolvedTurnID = activeTurnID
		}
	}

	summary := summaryFromPayload(payload)
	if summary != "" {
		_, _, _, terminal, _ := terminalFromEvent(eventType, method, payload)
		methodKey := strings.ToLower(strings.TrimSpace(method))
		eventKey := strings.ToLower(strings.TrimSpace(eventType))
		if terminal || methodKey == "codex/event/task_complete" || eventKey == "codex/event/task_complete" {
			rememberSummary(id, resolvedTurnID, summary)
		}
	}

	if !strings.EqualFold(strings.TrimSpace(method), "turn/completed") {
		return
	}
	if summary == "" {
		summary = lookupSummary(id, resolvedTurnID)
	}
	if summary == "" {
		return
	}
	injectSummary(payload, summary)
	rememberSummary(id, resolvedTurnID, summary)
}

// ApprovalStallHeartbeatInterval computes heartbeat interval while waiting approvals.
func ApprovalStallHeartbeatInterval(stallThreshold, defaultStallThreshold time.Duration) time.Duration {
	hbInterval := stallThreshold / 6
	if hbInterval <= 0 {
		hbInterval = defaultStallThreshold / 6
	}
	if hbInterval < 10*time.Second {
		hbInterval = 10 * time.Second
	}
	return hbInterval
}

// StartApprovalStallHeartbeat starts periodic turn heartbeat and returns stop func.
func StartApprovalStallHeartbeat(threadID string, stallThreshold, defaultStallThreshold time.Duration, touch func(string)) func() {
	id := strings.TrimSpace(threadID)
	if id == "" || touch == nil {
		return func() {}
	}
	heartbeatDone := make(chan struct{})
	hbInterval := ApprovalStallHeartbeatInterval(stallThreshold, defaultStallThreshold)
	util.SafeGo(func() {
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				touch(id)
			case <-heartbeatDone:
				return
			}
		}
	})
	var once sync.Once
	return func() {
		once.Do(func() { close(heartbeatDone) })
	}
}

// PeekTrackedTurnMeta reads active turn metadata.
func PeekTrackedTurnMeta(activeTurns map[string]*TrackedTurn, turnMu *sync.Mutex, threadID string) (string, time.Time, bool, bool) {
	id := strings.TrimSpace(threadID)
	if id == "" || turnMu == nil {
		return "", time.Time{}, false, false
	}

	turnMu.Lock()
	defer turnMu.Unlock()
	if activeTurns == nil {
		return "", time.Time{}, false, false
	}
	turn, ok := activeTurns[id]
	if !ok || turn == nil {
		return "", time.Time{}, false, false
	}
	return turn.ID, turn.StartedAt, turn.InterruptRequested, true
}

// MarkTrackedTurnStallHint marks whether stall hint was already logged for active turn.
func MarkTrackedTurnStallHint(activeTurns map[string]*TrackedTurn, turnMu *sync.Mutex, threadID, turnID string) bool {
	id := strings.TrimSpace(threadID)
	wantTurnID := strings.TrimSpace(turnID)
	if id == "" || wantTurnID == "" || turnMu == nil {
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
	if !strings.EqualFold(strings.TrimSpace(turn.ID), wantTurnID) {
		return false
	}
	if turn.StallHintLogged {
		return false
	}
	turn.StallHintLogged = true
	return true
}

// ShouldLogTrackedTurnStallHint reports whether current tail event implies a stall hint should be logged.
func ShouldLogTrackedTurnStallHint(eventType, method string, startedAt time.Time) bool {
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
	case "turn_diff", "plan_delta", "item/completed", "exec_command_end":
		return true
	default:
		return false
	}
}

// TouchTrackedTurnLastEvent updates active-turn heartbeat timestamp.
func TouchTrackedTurnLastEvent(activeTurns map[string]*TrackedTurn, turnMu *sync.Mutex, threadID string) {
	id := strings.TrimSpace(threadID)
	if id == "" || turnMu == nil {
		return
	}
	turnMu.Lock()
	defer turnMu.Unlock()
	if activeTurns == nil {
		logger.Warn("DIAG: touchTrackedTurnLastEvent called but activeTurns map is nil",
			logger.FieldThreadID, id,
		)
		return
	}
	turn, ok := activeTurns[id]
	if !ok || turn == nil {
		logger.Warn("DIAG: touchTrackedTurnLastEvent called but no active turn found",
			logger.FieldThreadID, id,
			"active_turns_count", len(activeTurns),
		)
		return
	}
	turn.LastEventAt = time.Now()
	turn.StallGraceStarted = false
}

// RescheduleStallCheck schedules next stall check before threshold.
func RescheduleStallCheck(turn *TrackedTurn, threadID, turnID string, silent, threshold time.Duration, checkTurnStall func(threadID, turnID string)) {
	if turn == nil || checkTurnStall == nil {
		return
	}
	interval := max(threshold/3, 10*time.Second)
	remaining := interval
	if remaining > threshold-silent {
		remaining = threshold - silent + time.Second
	}
	turn.StallTimer = time.AfterFunc(remaining, func() {
		checkTurnStall(threadID, turnID)
	})
}

// HandleStallGracePeriod starts grace timer before auto interrupt.
func HandleStallGracePeriod(
	activeTurns map[string]*TrackedTurn,
	turnMu *sync.Mutex,
	threadID,
	turnID string,
	silent,
	threshold,
	stallGracePeriod time.Duration,
	pushAlert func(threadID, category, message string),
	checkTurnStall func(threadID, turnID string),
) {
	if stallGracePeriod <= 0 {
		stallGracePeriod = 30 * time.Second
	}
	logger.Warn("turn tracker: thinking stall detected — grace period started",
		logger.FieldThreadID, threadID,
		logger.FieldTurnID, turnID,
		"silent_ms", silent.Milliseconds(),
		"threshold_ms", threshold.Milliseconds(),
		"grace_period_ms", stallGracePeriod.Milliseconds(),
	)

	if pushAlert != nil {
		pushAlert(threadID, "stall_warning",
			fmt.Sprintf("思考已 %ds 未响应，将在 %ds 后自动中断",
				int(silent.Seconds()), int(stallGracePeriod.Seconds())))
	}

	if turnMu == nil {
		return
	}
	turnMu.Lock()
	turn, ok := activeTurns[threadID]
	if ok && turn != nil && turn.ID == turnID {
		turn.StallTimer = time.AfterFunc(stallGracePeriod, func() {
			if checkTurnStall != nil {
				checkTurnStall(threadID, turnID)
			}
		})
	}
	turnMu.Unlock()
}

// IsTerminalEventType reports whether event type or method indicates a turn terminal event.
func IsTerminalEventType(eventType, method string) bool {
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

	a.TouchTrackedTurnLastEvent(threadID)

	if IsTerminalEventType(eventType, method) {
		hasActive := false
		hasActive = a.HasActiveTrackedTurn(threadID)
		logger.Warn("DIAG: AgentEventHandler received terminal event",
			logger.FieldThreadID, threadID,
			logger.FieldEventType, eventType,
			logger.FieldMethod, method,
			"has_active_tracked_turn", hasActive,
		)
	}
	a.MaybeFinalizeTrackedTurn(threadID, eventType, method, payload)
}

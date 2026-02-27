package tracker

import (
	"fmt"
	"github.com/multi-agent/go-agent-v2/pkg/util"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTurnWatchdogTimeout        = 10 * time.Minute
	DefaultTrackedTurnSummaryTTL      = 30 * time.Minute
	TrackedTurnSummaryCacheMaxEntries = 512
	DefaultStallThreshold             = 480 * time.Second
	DefaultStallHeartbeat             = 300 * time.Second
)

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
type trackedTurn = TrackedTurn
type TrackedTurnFinalizeRequest struct {
	TurnID string
	Status string
	Reason string
}
type TrackedTurnTransitionRequest struct {
	TouchHeartbeat         bool
	MarkInterruptRequested bool
	MarkStallHint          bool
	MarkStallHintForTurnID string
	Finalize               *TrackedTurnFinalizeRequest
}
type TrackedTurnTransitionResult struct {
	Found              bool
	ThreadID           string
	TurnID             string
	StartedAt          time.Time
	LastEventAt        time.Time
	InterruptRequested bool
	StallHintLogged    bool
	StallHintApplied   bool
	Finalized          bool
	FinalStatus        string
	FinalReason        string
	Completion         map[string]any
	ExpectedTurnID     string
	TurnIDMismatch     bool
}
type TrackedTurnStallAction int

const (
	TrackedTurnStallNoop TrackedTurnStallAction = iota
	TrackedTurnStallRescheduled
	TrackedTurnStallEnterGrace
	TrackedTurnStallAutoInterrupt
)

type TrackedTurnStallDecision struct {
	Action    TrackedTurnStallAction
	ThreadID  string
	TurnID    string
	Silent    time.Duration
	Threshold time.Duration
}
type TrackedTurnSummaryCacheEntry struct {
	TurnID    string
	Summary   string
	UpdatedAt time.Time
}
type trackedTurnSummaryCacheEntry = TrackedTurnSummaryCacheEntry
type TurnTrackerState struct {
	Mu                  *sync.Mutex
	ActiveTurns         *map[string]*trackedTurn
	TurnWatchdogTimeout *time.Duration
	TurnSummaryCache    *map[string]trackedTurnSummaryCacheEntry
	TurnSummaryTTL      *time.Duration
	StallThreshold      *time.Duration
	StallHeartbeat      *time.Duration
}

func EnsureTurnTrackerStateLocked(state TurnTrackerState) {
	if state.ActiveTurns != nil && *state.ActiveTurns == nil {
		*state.ActiveTurns = make(map[string]*trackedTurn)
	}
	if state.TurnSummaryCache != nil && *state.TurnSummaryCache == nil {
		*state.TurnSummaryCache = make(map[string]trackedTurnSummaryCacheEntry)
	}
	ensureTrackerDurationDefault(state.TurnWatchdogTimeout, DefaultTurnWatchdogTimeout)
	ensureTrackerDurationDefault(state.TurnSummaryTTL, DefaultTrackedTurnSummaryTTL)
	ensureTrackerDurationDefault(state.StallThreshold, DefaultStallThreshold)
	ensureTrackerDurationDefault(state.StallHeartbeat, DefaultStallHeartbeat)
}
func ensureTrackerDurationDefault(target *time.Duration, fallback time.Duration) {
	if target != nil && *target <= 0 {
		*target = fallback
	}
}
func TrackerDurationOrDefault(value *time.Duration, fallback time.Duration) time.Duration {
	if value != nil && *value > 0 {
		return *value
	}
	return fallback
}
func ApprovalStallHeartbeatInterval(stallThreshold, fallback, defaultThreshold time.Duration) time.Duration {
	base := defaultThreshold
	if fallback > 0 {
		base = fallback
	}
	if stallThreshold > 0 {
		base = stallThreshold
	}
	interval := base / 3
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	return interval
}

func StartStallHeartbeat(threadID string, stallThreshold, fallback, defaultThreshold time.Duration, touch func(string)) func() {
	id := strings.TrimSpace(threadID)
	interval := ApprovalStallHeartbeatInterval(stallThreshold, fallback, defaultThreshold)
	ticker := time.NewTicker(interval)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if touch != nil {
					touch(id)
				}
			case <-stop:
				ticker.Stop()
				return
			}
		}
	}()
	return func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
}

func TrackerInterruptSender(getProcess func(string) any, sendCommand func(any, string, string) error) func(string) (bool, error) {
	if getProcess == nil || sendCommand == nil {
		return nil
	}
	return func(threadID string) (bool, error) {
		proc := getProcess(threadID)
		if proc == nil {
			return false, nil
		}
		return true, sendCommand(proc, "/interrupt", "")
	}
}
func scheduleTrackedTurnStallCheck(turn *trackedTurn, delay time.Duration, threadID, turnID string, check func(string, string)) {
	if turn == nil || check == nil || delay <= 0 {
		return
	}
	turn.StallTimer = time.AfterFunc(delay, func() { check(threadID, turnID) })
}
func rescheduleStallCheck(turn *trackedTurn, threadID, turnID string, silent, threshold time.Duration, check func(string, string)) {
	if turn == nil || check == nil {
		return
	}
	remaining := threshold - silent
	if remaining <= 0 {
		remaining = 10 * time.Second
	}
	next := max(remaining/2, 10*time.Second)
	scheduleTrackedTurnStallCheck(turn, next, threadID, turnID, check)
}
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
		return util.FirstNonEmpty(s, "completed")
	}
}
func ExtractTrackedString(payload map[string]any, keys ...string) string {
	if payload == nil {
		return ""
	}
	for _, key := range keys {
		text, _ := payload[key].(string)
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	return ""
}
func extractTrackedRetryable(payload map[string]any) (bool, bool) {
	if payload == nil {
		return false, false
	}
	for _, key := range []string{"willRetry", "will_retry", "recoverable"} {
		switch typed := payload[key].(type) {
		case bool:
			return typed, true
		case string:
			switch normalizeTrackedEventKey(typed) {
			case "true", "1", "yes", "y":
				return true, true
			case "false", "0", "no", "n":
				return false, true
			}
		}
	}
	return false, false
}
func extractTrackedTurnNestedField(payload map[string]any, nestedKeys []string, rootKeys []string) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if value := ExtractTrackedString(turn, nestedKeys...); value != "" {
			return value
		}
	}
	return ExtractTrackedString(payload, rootKeys...)
}
func ExtractTrackedTurnID(payload map[string]any) string {
	return extractTrackedTurnNestedField(payload, []string{"id", "turnId", "turn_id"}, []string{"turnId", "turn_id", "id"})
}
func ExtractTrackedTurnStatus(payload map[string]any) string {
	return extractTrackedTurnNestedField(payload, []string{"status", "state"}, []string{"status", "state"})
}
func ExtractTrackedTurnReason(payload map[string]any) string {
	return extractTrackedTurnNestedField(payload, []string{"reason", "message"}, []string{"reason", "message"})
}

type trackedTurnTerminalKind uint8

const (
	trackedTurnTerminalNone trackedTurnTerminalKind = iota
	trackedTurnTerminalAborted
	trackedTurnTerminalCompleted
	trackedTurnTerminalConnectionDead
	trackedTurnTerminalShutdownComplete
	trackedTurnTerminalStreamError
	trackedTurnTerminalThreadStatusChanged
)

var trackedTurnTerminalPriority = []trackedTurnTerminalKind{
	trackedTurnTerminalAborted,
	trackedTurnTerminalCompleted,
	trackedTurnTerminalConnectionDead,
	trackedTurnTerminalShutdownComplete,
	trackedTurnTerminalStreamError,
	trackedTurnTerminalThreadStatusChanged,
}
var trackedTurnTerminalByEvent = map[string]trackedTurnTerminalKind{
	"turn_aborted":              trackedTurnTerminalAborted,
	"turn_complete":             trackedTurnTerminalCompleted,
	"turn/completed":            trackedTurnTerminalCompleted,
	"idle":                      trackedTurnTerminalCompleted,
	"codex/event/task_complete": trackedTurnTerminalCompleted,
	"connection_dead":           trackedTurnTerminalConnectionDead,
	"shutdown_complete":         trackedTurnTerminalShutdownComplete,
	"stream_error":              trackedTurnTerminalStreamError,
	"error":                     trackedTurnTerminalStreamError,
	"thread/status/changed":     trackedTurnTerminalThreadStatusChanged,
}
var trackedTurnTerminalByMethod = map[string]trackedTurnTerminalKind{
	"turn/aborted":              trackedTurnTerminalAborted,
	"turn/completed":            trackedTurnTerminalCompleted,
	"codex/event/task_complete": trackedTurnTerminalCompleted,
	"codex/event/stream_error":  trackedTurnTerminalStreamError,
	"error":                     trackedTurnTerminalStreamError,
	"thread/status/changed":     trackedTurnTerminalThreadStatusChanged,
}
var trackedThreadStatusTerminal = map[string]struct {
	Status string
	Reason string
}{
	"idle":         {Status: "completed", Reason: "thread_status_idle"},
	"systemerror":  {Status: "failed", Reason: "thread_status_system_error"},
	"system_error": {Status: "failed", Reason: "thread_status_system_error"},
	"error":        {Status: "failed", Reason: "thread_status_system_error"},
	"notloaded":    {Status: "failed", Reason: "thread_status_not_loaded"},
	"not_loaded":   {Status: "failed", Reason: "thread_status_not_loaded"},
}

func normalizeTrackedEventKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
func trackedTurnTerminalKindForEvent(eventType, method string) trackedTurnTerminalKind {
	eventKey := normalizeTrackedEventKey(eventType)
	methodKey := normalizeTrackedEventKey(method)
	eventKind, methodKind := trackedTurnTerminalByEvent[eventKey], trackedTurnTerminalByMethod[methodKey]
	for _, kind := range trackedTurnTerminalPriority {
		if eventKind == kind || methodKind == kind {
			return kind
		}
	}
	return trackedTurnTerminalNone
}
func trackedTurnTerminalResult(payload map[string]any, status, fallbackReason string, synthetic bool) (string, string, string, bool, bool) {
	return ExtractTrackedTurnID(payload), status, util.FirstNonEmpty(ExtractTrackedTurnReason(payload), fallbackReason), true, synthetic
}
func ThreadStatusTerminalFromPayload(payload map[string]any) (status string, reason string, terminal bool) {
	if payload == nil {
		return "", "", false
	}
	statusType := ""
	switch raw := payload["status"].(type) {
	case string:
		statusType = normalizeTrackedEventKey(raw)
	case map[string]any:
		statusType = normalizeTrackedEventKey(ExtractTrackedString(raw, "type"))
	}
	terminalStatus, ok := trackedThreadStatusTerminal[statusType]
	if !ok {
		return "", "", false
	}
	return terminalStatus.Status, terminalStatus.Reason, true
}
func TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	eventKey := normalizeTrackedEventKey(eventType)
	methodKey := normalizeTrackedEventKey(method)
	switch trackedTurnTerminalKindForEvent(eventType, method) {
	case trackedTurnTerminalAborted:
		return trackedTurnTerminalResult(payload, "interrupted", "turn_aborted", false)
	case trackedTurnTerminalCompleted:
		return trackedTurnTerminalResult(payload, util.FirstNonEmpty(ExtractTrackedTurnStatus(payload), "completed"), "turn_complete", false)
	case trackedTurnTerminalConnectionDead:
		return trackedTurnTerminalResult(payload, "failed", "connection_dead", true)
	case trackedTurnTerminalShutdownComplete:
		return trackedTurnTerminalResult(payload, "completed", "shutdown_complete", true)
	case trackedTurnTerminalStreamError:
		retryable, known := extractTrackedRetryable(payload)
		if !known || retryable {
			return "", "", "", false, false
		}
		reason := util.FirstNonEmpty(
			ExtractTrackedTurnReason(payload),
			ExtractTrackedString(payload, "phase"),
			eventKey,
			methodKey,
			"stream_error",
		)
		return ExtractTrackedTurnID(payload), "failed", reason, true, true
	case trackedTurnTerminalThreadStatusChanged:
		status, reason, terminal := ThreadStatusTerminalFromPayload(payload)
		if !terminal {
			return "", "", "", false, false
		}
		return ExtractTrackedTurnID(payload), status, reason, true, true
	default:
		return "", "", "", false, false
	}
}
func IsTerminalEventType(eventType, method string) bool {
	return trackedTurnTerminalKindForEvent(eventType, method) != trackedTurnTerminalNone
}
func TrackedTurnSummaryCacheKey(threadID, turnID string) string {
	return strings.TrimSpace(threadID) + "\x00" + strings.TrimSpace(turnID)
}
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
	if len(cache) <= maxEntries || maxEntries <= 0 {
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
	for _, key := range keys[:len(keys)-maxEntries] {
		delete(cache, key)
	}
}
func withTrackedTurnSummaryCache(state TurnTrackerState, turnMu *sync.Mutex, create bool, fn func(cache map[string]trackedTurnSummaryCacheEntry)) {
	if turnMu != nil {
		turnMu.Lock()
		defer turnMu.Unlock()
	}
	EnsureTurnTrackerStateLocked(state)
	if state.TurnSummaryCache == nil {
		return
	}
	cache := *state.TurnSummaryCache
	if cache == nil && create {
		cache = make(map[string]trackedTurnSummaryCacheEntry)
		*state.TurnSummaryCache = cache
	}
	if cache != nil {
		fn(cache)
	}
}
func RememberTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID, summary string) {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	text := strings.TrimSpace(summary)
	if id == "" || tid == "" || text == "" {
		return
	}
	withTrackedTurnSummaryCache(state, turnMu, true, func(cache map[string]trackedTurnSummaryCacheEntry) {
		now := time.Now()
		cache[TrackedTurnSummaryCacheKey(id, tid)] = trackedTurnSummaryCacheEntry{TurnID: tid, Summary: text, UpdatedAt: now}
		pruneTrackedTurnSummaryCacheLocked(cache, now, TrackerDurationOrDefault(state.TurnSummaryTTL, DefaultTrackedTurnSummaryTTL), TrackedTurnSummaryCacheMaxEntries)
	})
}
func LookupTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID string) string {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	if id == "" || tid == "" {
		return ""
	}
	summary := ""
	withTrackedTurnSummaryCache(state, turnMu, false, func(cache map[string]trackedTurnSummaryCacheEntry) {
		if entry, ok := cache[TrackedTurnSummaryCacheKey(id, tid)]; ok {
			summary = strings.TrimSpace(entry.Summary)
		}
	})
	return summary
}

var trackedTurnSummaryKeys = []string{"lastAgentMessage", "last_agent_message", "summary", "result", "message"}
var trackedTurnCompletionRootKeys = []string{"threadId", "status", "reason", "summary"}
var trackedTurnCompletionTurnKeys = []string{"id", "status", "reason", "summary"}

func copyTrackedTurnPayloadKeys(target, source map[string]any, keys []string) {
	if target == nil || source == nil {
		return
	}
	for _, key := range keys {
		if value, ok := source[key]; ok {
			target[key] = value
		}
	}
}
func ensureTrackedPayloadMap(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	nested, _ := payload[key].(map[string]any)
	if nested == nil {
		nested = map[string]any{}
		payload[key] = nested
	}
	return nested
}
func setTrackedPayloadStringIfMissing(payload map[string]any, key, value string) {
	if payload == nil || strings.TrimSpace(value) == "" {
		return
	}
	if strings.TrimSpace(ExtractTrackedString(payload, key)) == "" {
		payload[key] = value
	}
}
func trackedTurnCompletionPayload(threadID, turnID, status, reason string) map[string]any {
	return map[string]any{
		"threadId": strings.TrimSpace(threadID),
		"turn": map[string]any{
			"id":     strings.TrimSpace(turnID),
			"status": status,
		},
		"status": status,
		"reason": strings.TrimSpace(reason),
	}
}
func BuildTrackedTurnCompletionPayload(threadID, turnID, status, reason string) map[string]any {
	return trackedTurnCompletionPayload(threadID, turnID, status, reason)
}
func TrackedTurnSummaryFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if summary := ExtractTrackedString(payload, trackedTurnSummaryKeys...); summary != "" {
		return summary
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if summary := ExtractTrackedString(turn, trackedTurnSummaryKeys...); summary != "" {
			return summary
		}
	}
	if msg, ok := payload["msg"].(map[string]any); ok {
		if summary := ExtractTrackedString(msg, trackedTurnSummaryKeys...); summary != "" {
			return summary
		}
	}
	return ""
}
func InjectTrackedTurnSummary(payload map[string]any, summary string) {
	if payload == nil {
		return
	}
	text := strings.TrimSpace(summary)
	if text == "" {
		return
	}
	setTrackedPayloadStringIfMissing(payload, "lastAgentMessage", text)
	setTrackedPayloadStringIfMissing(payload, "summary", text)
	turn := ensureTrackedPayloadMap(payload, "turn")
	setTrackedPayloadStringIfMissing(turn, "lastAgentMessage", text)
	setTrackedPayloadStringIfMissing(turn, "summary", text)
}
func MergeTrackedTurnCompletionPayload(target map[string]any, completion map[string]any) {
	if target == nil || completion == nil {
		return
	}
	copyTrackedTurnPayloadKeys(target, completion, trackedTurnCompletionRootKeys)
	if completionTurn, ok := completion["turn"].(map[string]any); ok {
		copyTrackedTurnPayloadKeys(ensureTrackedPayloadMap(target, "turn"), completionTurn, trackedTurnCompletionTurnKeys)
	}
}
func WithTrackerStateLockCore(state TurnTrackerState, fn func(TurnTrackerState)) {
	if fn == nil {
		return
	}
	if state.Mu != nil {
		state.Mu.Lock()
		defer state.Mu.Unlock()
	}
	EnsureTurnTrackerStateLocked(state)
	fn(state)
}
func TrackerDurationCore(state TurnTrackerState, getter func(TurnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	if getter == nil {
		return fallback
	}
	value := fallback
	WithTrackerStateLockCore(state, func(lockedState TurnTrackerState) {
		value = TrackerDurationOrDefault(getter(lockedState), fallback)
	})
	return value
}
func SetTrackerDurationCore(state TurnTrackerState, getter func(TurnTrackerState) *time.Duration, value time.Duration) {
	if value <= 0 || getter == nil {
		return
	}
	WithTrackerStateLockCore(state, func(lockedState TurnTrackerState) {
		target := getter(lockedState)
		if target != nil {
			*target = value
		}
	})
}
func TrackerStateCore(state TurnTrackerState) (map[string]*trackedTurn, *sync.Mutex, time.Duration, time.Duration) {
	var activeTurns map[string]*trackedTurn
	if state.ActiveTurns != nil {
		activeTurns = *state.ActiveTurns
	}
	turnMu := state.Mu
	watchdogTimeout := TrackerDurationOrDefault(state.TurnWatchdogTimeout, DefaultTurnWatchdogTimeout)
	stallThreshold := TrackerDurationOrDefault(state.StallThreshold, DefaultStallThreshold)
	return activeTurns, turnMu, watchdogTimeout, stallThreshold
}
func stopTrackedTurnTimers(turn *trackedTurn) {
	if turn == nil {
		return
	}
	if turn.Timer != nil {
		turn.Timer.Stop()
	}
	if turn.StallTimer != nil {
		turn.StallTimer.Stop()
	}
}
func trySignalTrackedTurnDone(turn *trackedTurn, status string) bool {
	if turn == nil || turn.Done == nil {
		return false
	}
	select {
	case turn.Done <- status:
		return true
	default:
		return false
	}
}
func fillTrackedTurnTransitionResult(result *TrackedTurnTransitionResult, threadID string, turn *trackedTurn) {
	if result == nil || turn == nil {
		return
	}
	result.Found = true
	result.ThreadID = threadID
	result.TurnID = strings.TrimSpace(turn.ID)
	result.StartedAt = turn.StartedAt
	result.LastEventAt = turn.LastEventAt
	result.InterruptRequested = turn.InterruptRequested
	result.StallHintLogged = turn.StallHintLogged
}
func finalizeTrackedTurnTransition(activeTurns map[string]*trackedTurn, threadID string, turn *trackedTurn, req TrackedTurnFinalizeRequest, result *TrackedTurnTransitionResult) {
	if turn == nil || result == nil {
		return
	}
	result.ExpectedTurnID = strings.TrimSpace(req.TurnID)
	if result.ExpectedTurnID != "" && !strings.EqualFold(result.TurnID, result.ExpectedTurnID) {
		result.TurnIDMismatch = true
	}
	delete(activeTurns, threadID)
	stopTrackedTurnTimers(turn)
	finalStatus := NormalizeTrackedTurnStatus(req.Status)
	if turn.InterruptRequested && finalStatus == "completed" {
		finalStatus = "interrupted"
	}
	trySignalTrackedTurnDone(turn, finalStatus)
	result.Finalized = true
	result.FinalStatus = finalStatus
	result.FinalReason = strings.TrimSpace(req.Reason)
	result.Completion = trackedTurnCompletionPayload(threadID, result.TurnID, finalStatus, result.FinalReason)
}
func ApplyTrackedTurnTransitionCore(state TurnTrackerState, threadID string, req TrackedTurnTransitionRequest) TrackedTurnTransitionResult {
	result := TrackedTurnTransitionResult{}
	WithActiveTurnCore(state, threadID, func(id string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool {
		fillTrackedTurnTransitionResult(&result, id, turn)
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
			if wantTurnID := strings.TrimSpace(req.MarkStallHintForTurnID); wantTurnID != "" && !strings.EqualFold(result.TurnID, wantTurnID) {
				return false
			}
			if !turn.StallHintLogged {
				turn.StallHintLogged = true
				result.StallHintLogged = true
				result.StallHintApplied = true
			}
		}
		if req.Finalize != nil {
			finalizeTrackedTurnTransition(activeTurns, id, turn, *req.Finalize, &result)
		}
		return true
	})
	return result
}
func WithActiveTurnCore(state TurnTrackerState, threadID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	id := strings.TrimSpace(threadID)
	if id == "" || state.Mu == nil || state.ActiveTurns == nil || fn == nil {
		return false
	}
	state.Mu.Lock()
	defer state.Mu.Unlock()
	EnsureTurnTrackerStateLocked(state)
	activeTurns := *state.ActiveTurns
	turn, ok := activeTurns[id]
	return ok && turn != nil && fn(id, turn, activeTurns)
}
func WithActiveTurnByIDCore(state TurnTrackerState, threadID, turnID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	expectedTurnID := strings.TrimSpace(turnID)
	if expectedTurnID == "" || fn == nil {
		return false
	}
	return WithActiveTurnCore(state, threadID, func(id string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool {
		if !strings.EqualFold(strings.TrimSpace(turn.ID), expectedTurnID) {
			return false
		}
		return fn(id, turn, activeTurns)
	})
}
func SupersedeActiveTurn(activeTurns map[string]*trackedTurn, threadID, nextTurnID string) (map[string]any, string, bool) {
	if activeTurns == nil {
		return nil, "", false
	}
	prev, ok := activeTurns[threadID]
	if !ok || prev == nil {
		return nil, "", false
	}
	delete(activeTurns, threadID)
	stopTrackedTurnTimers(prev)
	trySignalTrackedTurnDone(prev, "failed")
	return trackedTurnCompletionPayload(threadID, prev.ID, "failed", "superseded_by_new_turn"), prev.ID, true
}
func BeginTrackedTurnCore(
	state TurnTrackerState,
	threadID string,
	turnID string,
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
	checkTurnStall func(string, string),
) string {
	activeTurns, turnMu, watchdogTimeout, stallThreshold := TrackerStateCore(state)
	id := strings.TrimSpace(threadID)
	if id == "" {
		return ""
	}
	tid := util.FirstNonEmpty(strings.TrimSpace(turnID), fmt.Sprintf("turn-%d", time.Now().UnixMilli()))
	if turnMu == nil || activeTurns == nil {
		return tid
	}
	var superseded map[string]any
	var hadPrevTurn bool
	now := time.Now()
	turnMu.Lock()
	EnsureTurnTrackerStateLocked(state)
	superseded, _, hadPrevTurn = SupersedeActiveTurn(activeTurns, id, tid)
	turn := &trackedTurn{
		ID:          tid,
		ThreadID:    id,
		StartedAt:   now,
		LastEventAt: now,
		Done:        make(chan string, 1),
	}
	effectiveWatchdog := watchdogTimeout
	if !hadPrevTurn {
		effectiveWatchdog += watchdogTimeout / 2
	}
	turn.Timer = time.AfterFunc(effectiveWatchdog, func() {
		if notify == nil || completeTrackedTurnByID == nil {
			return
		}
		if completion, ok := completeTrackedTurnByID(id, tid, "failed", "watchdog_timeout"); ok {
			notify("turn/completed", completion)
		}
	})
	activeTurns[id] = turn
	if checkTurnStall != nil {
		scheduleTrackedTurnStallCheck(turn, max(stallThreshold/3, 10*time.Second), id, tid, checkTurnStall)
	}
	turnMu.Unlock()
	if superseded != nil && notify != nil {
		notify("turn/completed", superseded)
	}
	return tid
}
func WaitTrackedTurnTerminalCore(state TurnTrackerState, threadID string, timeout time.Duration) (string, bool) {
	if timeout <= 0 {
		return "", false
	}
	var done chan string
	if !WithActiveTurnCore(state, threadID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		done = turn.Done
		return done != nil
	}) || done == nil {
		return "", false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case status := <-done:
		return NormalizeTrackedTurnStatus(status), true
	case <-timer.C:
		return "", false
	}
}
func CompleteTrackedTurnByIDCore(state TurnTrackerState, threadID, turnID, status, reason string) (map[string]any, bool) {
	transition := ApplyTrackedTurnTransitionCore(state, threadID, TrackedTurnTransitionRequest{
		Finalize: &TrackedTurnFinalizeRequest{
			TurnID: turnID,
			Status: status,
			Reason: reason,
		},
	})
	if !transition.Finalized || transition.Completion == nil {
		return nil, false
	}
	return transition.Completion, true
}
func PeekTrackedTurnMetaCore(state TurnTrackerState, threadID string) (string, time.Time, bool, bool) {
	transition := ApplyTrackedTurnTransitionCore(state, threadID, TrackedTurnTransitionRequest{})
	if !transition.Found {
		return "", time.Time{}, false, false
	}
	return transition.TurnID, transition.StartedAt, transition.InterruptRequested, true
}
func MarkTrackedTurnStallHintCore(state TurnTrackerState, threadID, turnID string) bool {
	transition := ApplyTrackedTurnTransitionCore(state, threadID, TrackedTurnTransitionRequest{
		MarkStallHint:          true,
		MarkStallHintForTurnID: strings.TrimSpace(turnID),
	})
	return transition.StallHintApplied
}
func TouchTrackedTurnLastEventCore(state TurnTrackerState, threadID string) {
	ApplyTrackedTurnTransitionCore(state, threadID, TrackedTurnTransitionRequest{TouchHeartbeat: true})
}
func NextTrackedTurnStallDecisionCore(
	state TurnTrackerState,
	threadID string,
	turnID string,
	stallThreshold time.Duration,
	checkTurnStall func(string, string),
) TrackedTurnStallDecision {
	id, tid := strings.TrimSpace(threadID), strings.TrimSpace(turnID)
	threshold := stallThreshold
	if threshold <= 0 {
		threshold = DefaultStallThreshold
	}
	decision := TrackedTurnStallDecision{Action: TrackedTurnStallNoop, ThreadID: id, TurnID: tid, Threshold: threshold}
	if id == "" || tid == "" {
		return decision
	}
	WithActiveTurnByIDCore(state, id, tid, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		decision.TurnID = strings.TrimSpace(turn.ID)
		decision.Silent = time.Since(turn.LastEventAt)
		switch {
		case decision.Silent < threshold:
			rescheduleStallCheck(turn, id, decision.TurnID, decision.Silent, threshold, checkTurnStall)
			decision.Action = TrackedTurnStallRescheduled
		case turn.StallAutoInterrupted:
		case !turn.StallGraceStarted:
			turn.StallGraceStarted = true
			decision.Action = TrackedTurnStallEnterGrace
		default:
			turn.StallAutoInterrupted = true
			decision.Action = TrackedTurnStallAutoInterrupt
		}
		return true
	})
	return decision
}
func CheckTurnStallCore(
	state TurnTrackerState,
	threadID string,
	turnID string,
	handleStallGracePeriod func(threadID, turnID string, silent, threshold time.Duration),
	executeStallAutoInterrupt func(threadID, turnID string, silent, threshold time.Duration),
	checkTurnStall func(string, string),
) {
	_, _, _, stallThreshold := TrackerStateCore(state)
	decision := NextTrackedTurnStallDecisionCore(state, threadID, turnID, stallThreshold, checkTurnStall)
	if decision.Action != TrackedTurnStallEnterGrace && decision.Action != TrackedTurnStallAutoInterrupt {
		return
	}
	if decision.Action == TrackedTurnStallEnterGrace && handleStallGracePeriod != nil {
		handleStallGracePeriod(decision.ThreadID, decision.TurnID, decision.Silent, decision.Threshold)
		return
	}
	if executeStallAutoInterrupt != nil {
		executeStallAutoInterrupt(decision.ThreadID, decision.TurnID, decision.Silent, decision.Threshold)
	}
}
func HandleStallGracePeriodCore(
	state TurnTrackerState,
	threadID string,
	turnID string,
	_ time.Duration,
	_ time.Duration,
	pushAlert func(threadID, category, message string),
	checkTurnStall func(string, string),
) {
	if pushAlert != nil {
		pushAlert(threadID, "stall_warning", "长时间无事件，若持续将自动中断")
	}
	WithActiveTurnByIDCore(state, threadID, turnID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		scheduleTrackedTurnStallCheck(turn, 30*time.Second, threadID, turnID, checkTurnStall)
		return true
	})
}

type TrackerAlertRuntime interface {
	PushAlert(threadID, category, message string)
}

func TrackerRuntimePushAlert(runtime TrackerAlertRuntime) func(threadID, category, message string) {
	if runtime == nil {
		return nil
	}
	return runtime.PushAlert
}
func ExecuteStallAutoInterruptCore(
	threadID string,
	turnID string,
	silent time.Duration,
	_ time.Duration,
	pushAlert func(threadID, category, message string),
	markTrackedTurnInterruptRequested func(string) bool,
	cancelCodeRuns func(string) int,
	sendInterrupt func(string) (bool, error),
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
) {
	if pushAlert != nil {
		pushAlert(threadID, "stall", fmt.Sprintf("思考超时 %ds 未响应，自动中断", int(silent.Seconds())))
	}
	util.SafeGo(func() {
		if markTrackedTurnInterruptRequested != nil {
			markTrackedTurnInterruptRequested(threadID)
		}
		if cancelCodeRuns != nil {
			cancelCodeRuns(threadID)
		}
		interrupted := false
		if sendInterrupt != nil {
			attempted, err := sendInterrupt(threadID)
			interrupted = attempted && err == nil
		}
		if interrupted || notify == nil || completeTrackedTurnByID == nil {
			return
		}
		if completion, ok := completeTrackedTurnByID(threadID, turnID, "failed", "thinking_stall_timeout"); ok {
			notify("turn/completed", completion)
		}
	})
}
func CaptureAndInjectTurnSummaryCore(state TurnTrackerState, threadID, eventType, method string, payload map[string]any) {
	if payload == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	turnID := strings.TrimSpace(ExtractTrackedTurnID(payload))
	if turnID == "" {
		if activeTurnID, _, _, ok := PeekTrackedTurnMetaCore(state, id); ok {
			turnID = strings.TrimSpace(activeTurnID)
		}
	}
	methodKey := normalizeTrackedEventKey(method)
	eventKey := normalizeTrackedEventKey(eventType)
	summary := TrackedTurnSummaryFromPayload(payload)
	if summary != "" {
		_, _, _, terminal, _ := TrackedTurnTerminalFromEvent(eventType, method, payload)
		if terminal || methodKey == "codex/event/task_complete" || eventKey == "codex/event/task_complete" {
			RememberTrackedTurnSummary(state, state.Mu, id, turnID, summary)
		}
	}
	if methodKey != "turn/completed" {
		return
	}
	summary = util.FirstNonEmpty(summary, LookupTrackedTurnSummary(state, state.Mu, id, turnID))
	if summary == "" {
		return
	}
	InjectTrackedTurnSummary(payload, summary)
	RememberTrackedTurnSummary(state, state.Mu, id, turnID, summary)
}
func MaybeFinalizeTrackedTurnCore(
	state TurnTrackerState,
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
	turnID, startedAt, _, ok := PeekTrackedTurnMetaCore(state, id)
	if !ok {
		return
	}
	eventTurnID, status, reason, terminal, synthetic := TrackedTurnTerminalFromEvent(eventType, method, payload)
	if !terminal {
		if !IsTerminalEventType(eventType, method) && !startedAt.IsZero() && time.Since(startedAt) >= 30*time.Second {
			MarkTrackedTurnStallHintCore(state, id, turnID)
		}
		return
	}
	eventTurnID = util.FirstNonEmpty(eventTurnID, turnID)
	completion, completed := CompleteTrackedTurnByIDCore(state, id, eventTurnID, status, reason)
	if !completed {
		return
	}
	summary := util.FirstNonEmpty(
		TrackedTurnSummaryFromPayload(payload),
		LookupTrackedTurnSummary(state, state.Mu, id, util.FirstNonEmpty(eventTurnID, ExtractTrackedTurnID(payload), turnID)),
	)
	if summary != "" {
		InjectTrackedTurnSummary(completion, summary)
		RememberTrackedTurnSummary(state, state.Mu, id, util.FirstNonEmpty(ExtractTrackedTurnID(completion), eventTurnID, ExtractTrackedTurnID(payload)), summary)
	}
	if synthetic {
		if notify != nil {
			notify("turn/completed", completion)
		}
		return
	}
	MergeTrackedTurnCompletionPayload(payload, completion)
}
func FinalizeTrackedTurnEventCore(state TurnTrackerState, threadID, eventType, method string, payload map[string]any, notify func(string, any)) {
	TouchTrackedTurnLastEventCore(state, threadID)
	MaybeFinalizeTrackedTurnCore(state, threadID, eventType, method, payload, notify)
}

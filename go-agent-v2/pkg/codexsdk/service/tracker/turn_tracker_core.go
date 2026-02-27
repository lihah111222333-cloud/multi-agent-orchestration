package tracker

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	DefaultTurnWatchdogTimeout        = 10 * time.Minute
	DefaultTrackedTurnSummaryTTL      = 30 * time.Minute
	TrackedTurnSummaryCacheMaxEntries = 512
	DefaultStallThreshold             = 480 * time.Second
	DefaultStallHeartbeat             = 300 * time.Second
	trackedTurnGracePeriod            = 30 * time.Second
	// earlySilenceFirstTurn 首轮 turn 早期静默检测超时（含 MCP 启动开销）。
	earlySilenceFirstTurn = 120 * time.Second
	// earlySilenceSubsequent 后续 turn 早期静默检测超时。
	earlySilenceSubsequent = 60 * time.Second
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
	EarlySilenceTimer    *time.Timer
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
type TurnTrackerState struct {
	Mu                  *sync.Mutex
	ActiveTurns         *map[string]*trackedTurn
	TurnWatchdogTimeout *time.Duration
	TurnSummaryCache    *map[string]TrackedTurnSummaryCacheEntry
	TurnSummaryTTL      *time.Duration
	StallThreshold      *time.Duration
	StallHeartbeat      *time.Duration
}

type trackedTurnTerminalKind int

const (
	trackedTurnTerminalNone trackedTurnTerminalKind = iota
	trackedTurnTerminalAborted
	trackedTurnTerminalCompleted
	trackedTurnTerminalConnectionDead
	trackedTurnTerminalShutdownComplete
	trackedTurnTerminalStreamError
	trackedTurnTerminalThreadStatusChanged
)

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
	"error":                     trackedTurnTerminalStreamError,
	"codex/event/stream_error":  trackedTurnTerminalStreamError,
	"thread/status/changed":     trackedTurnTerminalThreadStatusChanged,
}

var trackedTurnSummaryKeys = []string{"lastAgentMessage", "last_agent_message", "summary", "result", "message"}

func EnsureTurnTrackerStateLocked(state TurnTrackerState) {
	if state.ActiveTurns != nil && *state.ActiveTurns == nil {
		*state.ActiveTurns = make(map[string]*trackedTurn)
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

func threadLogFields(threadID string) []any {
	id := strings.TrimSpace(threadID)
	return []any{
		logger.FieldAgentID, id,
		logger.FieldThreadID, id,
	}
}
func shouldLogTrackedTurnStallHint(eventType, method string, startedAt time.Time) bool {
	if IsTerminalEventType(eventType, method) {
		return false
	}
	if startedAt.IsZero() {
		return false
	}
	return time.Since(startedAt) >= trackedTurnGracePeriod
}
func scheduleTrackedTurnStallCheck(turn *trackedTurn, delay time.Duration, threadID, turnID string, check func(string, string)) {
	if turn == nil || check == nil {
		return
	}
	if delay <= 0 {
		delay = 10 * time.Second
	}
	turn.StallTimer = time.AfterFunc(delay, func() { check(threadID, turnID) })
}
func rescheduleStallCheck(turn *trackedTurn, threadID, turnID string, silent, threshold time.Duration, check func(string, string)) {
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
		if s == "" {
			return "completed"
		}
		return s
	}
}
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
func trackedTurnEventAndMethodKeys(eventType, method string) (string, string) {
	return strings.ToLower(strings.TrimSpace(eventType)), strings.ToLower(strings.TrimSpace(method))
}
func trackedTurnTerminalKindFor(eventKey, methodKey string) trackedTurnTerminalKind {
	if kind, ok := trackedTurnTerminalByEvent[eventKey]; ok {
		return kind
	}
	if kind, ok := trackedTurnTerminalByMethod[methodKey]; ok {
		return kind
	}
	return trackedTurnTerminalNone
}
func trackedTurnReasonOr(payload map[string]any, fallback string) string {
	reason := ExtractTrackedTurnReason(payload)
	if reason != "" {
		return reason
	}
	return fallback
}

var threadStatusTerminalMap = map[string]struct {
	status string
	reason string
}{
	"idle":         {status: "completed", reason: "thread_status_idle"},
	"systemerror":  {status: "failed", reason: "thread_status_system_error"},
	"system_error": {status: "failed", reason: "thread_status_system_error"},
	"error":        {status: "failed", reason: "thread_status_system_error"},
	"notloaded":    {status: "failed", reason: "thread_status_not_loaded"},
	"not_loaded":   {status: "failed", reason: "thread_status_not_loaded"},
}

func extractThreadStatusType(payload map[string]any) string {
	switch raw := payload["status"].(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(raw))
	case map[string]any:
		return strings.ToLower(strings.TrimSpace(ExtractTrackedString(raw, "type")))
	default:
		return ""
	}
}
func ThreadStatusTerminalFromPayload(payload map[string]any) (status string, reason string, terminal bool) {
	if payload == nil {
		return "", "", false
	}
	statusType := extractThreadStatusType(payload)
	if statusType == "" {
		return "", "", false
	}
	if terminal, ok := threadStatusTerminalMap[statusType]; ok {
		return terminal.status, terminal.reason, true
	}
	return "", "", false
}
func TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	eventKey, methodKey := trackedTurnEventAndMethodKeys(eventType, method)
	switch trackedTurnTerminalKindFor(eventKey, methodKey) {
	case trackedTurnTerminalAborted:
		return ExtractTrackedTurnID(payload), "interrupted", trackedTurnReasonOr(payload, "turn_aborted"), true, false
	case trackedTurnTerminalCompleted:
		status := ExtractTrackedTurnStatus(payload)
		if status == "" {
			status = "completed"
		}
		return ExtractTrackedTurnID(payload), status, trackedTurnReasonOr(payload, "turn_complete"), true, false
	case trackedTurnTerminalConnectionDead:
		return ExtractTrackedTurnID(payload), "failed", trackedTurnReasonOr(payload, "connection_dead"), true, true
	case trackedTurnTerminalShutdownComplete:
		return ExtractTrackedTurnID(payload), "completed", trackedTurnReasonOr(payload, "shutdown_complete"), true, true
	case trackedTurnTerminalStreamError:
		retryable, known := extractTrackedRetryable(payload)
		if !known || retryable {
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
	case trackedTurnTerminalThreadStatusChanged:
		status, reason, ok := ThreadStatusTerminalFromPayload(payload)
		if !ok {
			return "", "", "", false, false
		}
		return ExtractTrackedTurnID(payload), status, reason, true, true
	default:
		return "", "", "", false, false
	}
}
func IsTerminalEventType(eventType, method string) bool {
	eventKey, methodKey := trackedTurnEventAndMethodKeys(eventType, method)
	return trackedTurnTerminalKindFor(eventKey, methodKey) != trackedTurnTerminalNone
}
func TrackedTurnSummaryCacheKey(threadID, turnID string) string {
	return strings.TrimSpace(threadID) + "\x00" + strings.TrimSpace(turnID)
}
func pruneTrackedTurnSummaryCacheLocked(cache map[string]TrackedTurnSummaryCacheEntry, now time.Time, ttl time.Duration, maxEntries int) {
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
func RememberTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID, summary string) {
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
	EnsureTurnTrackerStateLocked(state)
	if state.TurnSummaryCache == nil {
		return
	}
	cache := *state.TurnSummaryCache
	if cache == nil {
		cache = make(map[string]TrackedTurnSummaryCacheEntry)
		*state.TurnSummaryCache = cache
	}
	cache[TrackedTurnSummaryCacheKey(id, tid)] = TrackedTurnSummaryCacheEntry{TurnID: tid, Summary: text, UpdatedAt: time.Now()}
	ttl := DefaultTrackedTurnSummaryTTL
	if state.TurnSummaryTTL != nil && *state.TurnSummaryTTL > 0 {
		ttl = *state.TurnSummaryTTL
	}
	pruneTrackedTurnSummaryCacheLocked(cache, time.Now(), ttl, TrackedTurnSummaryCacheMaxEntries)
}
func LookupTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID string) string {
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
	entry, ok := cache[TrackedTurnSummaryCacheKey(id, tid)]
	if !ok {
		return ""
	}
	return strings.TrimSpace(entry.Summary)
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
	if strings.TrimSpace(ExtractTrackedString(payload, "lastAgentMessage")) == "" {
		payload["lastAgentMessage"] = text
	}
	if strings.TrimSpace(ExtractTrackedString(payload, "summary")) == "" {
		payload["summary"] = text
	}
	turn, ok := payload["turn"].(map[string]any)
	if !ok || turn == nil {
		turn = map[string]any{}
		payload["turn"] = turn
	}
	if strings.TrimSpace(ExtractTrackedString(turn, "lastAgentMessage")) == "" {
		turn["lastAgentMessage"] = text
	}
	if strings.TrimSpace(ExtractTrackedString(turn, "summary")) == "" {
		turn["summary"] = text
	}
}
func buildTrackedTurnCompletionPayload(threadID, turnID, status, reason string) map[string]any {
	return map[string]any{
		"threadId": threadID,
		"turn": map[string]any{
			"id":     turnID,
			"status": status,
		},
		"status": status,
		"reason": reason,
	}
}
func MergeTrackedTurnCompletionPayload(target map[string]any, completion map[string]any) {
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
func ApplyTrackedTurnTransitionCore(state TurnTrackerState, threadID string, req TrackedTurnTransitionRequest) TrackedTurnTransitionResult {
	result := TrackedTurnTransitionResult{}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return result
	}
	activeTurns, turnMu, _, _ := TrackerStateCore(state)
	if turnMu == nil || activeTurns == nil {
		return result
	}
	turnMu.Lock()
	defer turnMu.Unlock()
	EnsureTurnTrackerStateLocked(state)
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
		if turn.EarlySilenceTimer != nil {
			turn.EarlySilenceTimer.Stop()
		}
		finalStatus := NormalizeTrackedTurnStatus(req.Finalize.Status)
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
		result.Completion = buildTrackedTurnCompletionPayload(id, result.TurnID, finalStatus, reasonText)
	}
	return result
}
func WithActiveTurnCore(state TurnTrackerState, threadID string, fn func(threadID string, turn *trackedTurn, activeTurns map[string]*trackedTurn) bool) bool {
	activeTurns, turnMu, _, _ := TrackerStateCore(state)
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
	if prev.Timer != nil {
		prev.Timer.Stop()
	}
	if prev.StallTimer != nil {
		prev.StallTimer.Stop()
	}
	if prev.EarlySilenceTimer != nil {
		prev.EarlySilenceTimer.Stop()
	}
	doneSent := false
	if prev.Done != nil {
		select {
		case prev.Done <- "failed":
			doneSent = true
		default:
		}
	}
	prevAge := time.Since(prev.StartedAt)
	prevLastEventAge := time.Since(prev.LastEventAt)
	logFn := logger.Warn
	if prevAge < 5*time.Second && !prev.InterruptRequested {
		logFn = logger.Info
	}
	fields := append(threadLogFields(threadID),
		"prev_turn_id", prev.ID,
		"next_turn_id", nextTurnID,
		"prev_age_ms", prevAge.Milliseconds(),
		"prev_last_event_age_ms", prevLastEventAge.Milliseconds(),
		"prev_interrupt_requested", prev.InterruptRequested,
		"prev_done_sent", doneSent,
		"prev_stall_hint_logged", prev.StallHintLogged,
		"prev_stall_grace_started", prev.StallGraceStarted,
		"prev_stall_auto_interrupted", prev.StallAutoInterrupted,
	)
	logFn("turn tracker: superseding active turn", fields...)
	payload := buildTrackedTurnCompletionPayload(threadID, prev.ID, "failed", "superseded_by_new_turn")
	return payload, prev.ID, true
}
func BeginTrackedTurnCore(
	state TurnTrackerState,
	threadID string,
	turnID string,
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
	checkTurnStall func(string, string),
	recoverProcess func(threadID, reason string),
) string {
	activeTurns, turnMu, watchdogTimeout, stallThreshold := TrackerStateCore(state)
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
	EnsureTurnTrackerStateLocked(state)
	superseded, prevTurnID, hadPrevTurn = SupersedeActiveTurn(activeTurns, id, tid)
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
	// 首轮 turn 给予额外 grace period（初始化开销：进程启动、WS 建连、MCP 加载等）
	effectiveWatchdog := watchdogTimeout
	if !hadPrevTurn {
		effectiveWatchdog = watchdogTimeout + watchdogTimeout/2 // 1.5x for first turn
	}
	turn.Timer = time.AfterFunc(effectiveWatchdog, func() {
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
	// 早期静默检测: submit 后如果一段时间内没有收到任何事件,
	// 说明 Codex 进程可能已死, 触发恢复。
	earlySilenceTimeout := earlySilenceSubsequent
	if !hadPrevTurn {
		earlySilenceTimeout = earlySilenceFirstTurn
	}
	earlySilenceThreadID := id
	earlySilenceTurnID := tid
	turn.EarlySilenceTimer = time.AfterFunc(earlySilenceTimeout, func() {
		if turnMu == nil {
			return
		}
		turnMu.Lock()
		current, ok := activeTurns[earlySilenceThreadID]
		if !ok || current == nil || current.ID != earlySilenceTurnID {
			turnMu.Unlock()
			return
		}
		silent := time.Since(current.LastEventAt)
		// 只有当真的没收到任何事件时才触发 (给 5s 容差)
		if silent < earlySilenceTimeout-5*time.Second {
			turnMu.Unlock()
			return
		}
		turnMu.Unlock()

		logger.Warn("turn tracker: early silence detected — no events after submit", append(threadLogFields(earlySilenceThreadID),
			logger.FieldTurnID, earlySilenceTurnID,
			"silent_ms", silent.Milliseconds(),
			"timeout_ms", earlySilenceTimeout.Milliseconds(),
		)...)
		if recoverProcess != nil {
			recoverProcess(earlySilenceThreadID, "early_silence_after_submit")
		}
	})
	activeTurns[id] = turn
	if checkTurnStall != nil {
		stallInterval := max(stallThreshold/3, 10*time.Second)
		scheduleTrackedTurnStallCheck(turn, stallInterval, id, tid, checkTurnStall)
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
func WaitTrackedTurnTerminalCore(state TurnTrackerState, threadID string, timeout time.Duration) (string, bool) {
	if timeout <= 0 {
		return "", false
	}
	var done chan string
	ok := WithActiveTurnCore(state, threadID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
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
	decision := TrackedTurnStallDecision{Action: TrackedTurnStallNoop}
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	if id == "" || tid == "" {
		return decision
	}
	threshold := stallThreshold
	if threshold <= 0 {
		threshold = DefaultStallThreshold
	}
	WithActiveTurnByIDCore(state, id, tid, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		currentTurnID := strings.TrimSpace(turn.ID)
		silent := time.Since(turn.LastEventAt)
		decision.ThreadID = id
		decision.TurnID = currentTurnID
		decision.Silent = silent
		decision.Threshold = threshold
		if silent < threshold {
			rescheduleStallCheck(turn, id, currentTurnID, silent, threshold, checkTurnStall)
			decision.Action = TrackedTurnStallRescheduled
			return true
		}
		if turn.StallAutoInterrupted {
			return true
		}
		if !turn.StallGraceStarted {
			turn.StallGraceStarted = true
			decision.Action = TrackedTurnStallEnterGrace
			return true
		}
		turn.StallAutoInterrupted = true
		decision.Action = TrackedTurnStallAutoInterrupt
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
	switch decision.Action {
	case TrackedTurnStallRescheduled, TrackedTurnStallNoop:
		return
	case TrackedTurnStallEnterGrace:
		if handleStallGracePeriod != nil {
			handleStallGracePeriod(decision.ThreadID, decision.TurnID, decision.Silent, decision.Threshold)
		}
	case TrackedTurnStallAutoInterrupt:
		if executeStallAutoInterrupt != nil {
			executeStallAutoInterrupt(decision.ThreadID, decision.TurnID, decision.Silent, decision.Threshold)
		}
	}
}
func HandleStallGracePeriodCore(
	state TurnTrackerState,
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
		"grace_ms", trackedTurnGracePeriod.Milliseconds(),
	)...)
	if pushAlert != nil {
		pushAlert(threadID, "stall_warning", "长时间无事件，若持续将自动中断")
	}
	WithActiveTurnByIDCore(state, threadID, turnID, func(_ string, turn *trackedTurn, _ map[string]*trackedTurn) bool {
		scheduleTrackedTurnStallCheck(turn, trackedTurnGracePeriod, threadID, turnID, checkTurnStall)
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
	threshold time.Duration,
	pushAlert func(threadID, category, message string),
	markTrackedTurnInterruptRequested func(string) bool,
	cancelCodeRuns func(string) int,
	sendInterrupt func(string) (bool, error),
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
	recoverProcess func(threadID, reason string),
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
		// interrupt 失败/进程无响应时，触发进程恢复
		if !interrupted && recoverProcess != nil {
			logger.Warn("turn tracker: triggering process recovery after failed stall interrupt", append(threadLogFields(threadID),
				logger.FieldTurnID, turnID,
			)...)
			recoverProcess(threadID, "stall_interrupt_failed")
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
	summary := TrackedTurnSummaryFromPayload(payload)
	if summary != "" {
		_, _, _, terminal, _ := TrackedTurnTerminalFromEvent(eventType, method, payload)
		eventKey, methodKey := trackedTurnEventAndMethodKeys(eventType, method)
		if terminal || eventKey == "codex/event/task_complete" || methodKey == "codex/event/task_complete" {
			RememberTrackedTurnSummary(state, state.Mu, id, turnID, summary)
		}
	}
	if !strings.EqualFold(strings.TrimSpace(method), "turn/completed") {
		return
	}
	if summary == "" && turnID != "" {
		summary = LookupTrackedTurnSummary(state, state.Mu, id, turnID)
	}
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
	turnID, startedAt, interruptRequested, ok := PeekTrackedTurnMetaCore(state, id)
	if !ok {
		return
	}
	eventTurnID, status, reason, terminal, synthetic := TrackedTurnTerminalFromEvent(eventType, method, payload)
	diagFields := append(threadLogFields(id),
		"tracked_turn_id", turnID,
		"event_turn_id", strings.TrimSpace(eventTurnID),
		logger.FieldStatus, strings.TrimSpace(status),
		"reason", strings.TrimSpace(reason),
		logger.FieldEventType, strings.TrimSpace(eventType),
		logger.FieldMethod, strings.TrimSpace(method),
	)
	if !terminal {
		if shouldLogTrackedTurnStallHint(eventType, method, startedAt) && MarkTrackedTurnStallHintCore(state, id, turnID) {
			logger.Warn("turn tracker: active turn not terminal yet at tail event", append(diagFields,
				"turn_age_ms", time.Since(startedAt).Milliseconds(),
				"interrupt_requested", interruptRequested,
			)...)
		}
		return
	}
	if strings.TrimSpace(eventTurnID) == "" {
		logger.Warn("turn tracker: terminal event missing turn_id", diagFields...)
		// Fallback: use tracker's known turn_id to avoid mismatched cleanup.
		eventTurnID = turnID
	}
	completion, completed := CompleteTrackedTurnByIDCore(state, id, eventTurnID, status, reason)
	if !completed {
		logger.Warn("turn tracker: terminal event failed to close tracked turn", diagFields...)
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
	summary := TrackedTurnSummaryFromPayload(payload)
	if summary == "" {
		summary = LookupTrackedTurnSummary(state, state.Mu, id, util.FirstNonEmpty(eventTurnID, ExtractTrackedTurnID(payload), turnID))
	}
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

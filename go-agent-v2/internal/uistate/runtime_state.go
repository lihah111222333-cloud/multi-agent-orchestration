package uistate

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ThreadSnapshot is UI-ready thread info.
type ThreadSnapshot struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// TimelineAttachment is a lightweight attachment reference.
type TimelineAttachment struct {
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Path       string `json:"path,omitempty"`
	PreviewURL string `json:"previewUrl,omitempty"`
}

// TimelineItem is the unified render item for chat timeline.
type TimelineItem struct {
	ID          string               `json:"id"`
	Ts          string               `json:"ts"`
	Kind        string               `json:"kind"`
	Text        string               `json:"text,omitempty"`
	Attachments []TimelineAttachment `json:"attachments,omitempty"`
	Done        bool                 `json:"done,omitempty"`
	Command     string               `json:"command,omitempty"`
	Output      string               `json:"output,omitempty"`
	Status      string               `json:"status,omitempty"`
	ExitCode    *int                 `json:"exitCode,omitempty"`
	File        string               `json:"file,omitempty"`
	Tool        string               `json:"tool,omitempty"`
	Preview     string               `json:"preview,omitempty"`
	ElapsedMS   *int                 `json:"elapsedMs,omitempty"`
}

// AgentMeta tracks runtime meta for thread cards.
type AgentMeta struct {
	Alias        string `json:"alias,omitempty"`
	LastActiveAt string `json:"lastActiveAt,omitempty"`
	IsMain       bool   `json:"isMain,omitempty"`
}

// TokenUsageSnapshot stores context-window token usage for UI.
type TokenUsageSnapshot struct {
	UsedTokens          int     `json:"usedTokens"`
	ContextWindowTokens int     `json:"contextWindowTokens,omitempty"`
	UsedPercent         float64 `json:"usedPercent,omitempty"`
	LeftPercent         float64 `json:"leftPercent,omitempty"`
	UpdatedAt           string  `json:"updatedAt,omitempty"`
}

// RuntimeSnapshot is a full UI runtime state snapshot.
type RuntimeSnapshot struct {
	Threads                 []ThreadSnapshot              `json:"threads"`
	Statuses                map[string]string             `json:"statuses"`
	InterruptibleByThread   map[string]bool               `json:"interruptibleByThread"`
	StatusHeadersByThread   map[string]string             `json:"statusHeadersByThread"`
	StatusDetailsByThread   map[string]string             `json:"statusDetailsByThread"`
	TimelinesByThread       map[string][]TimelineItem     `json:"timelinesByThread"`
	DiffTextByThread        map[string]string             `json:"diffTextByThread"`
	TokenUsageByThread      map[string]TokenUsageSnapshot `json:"tokenUsageByThread"`
	WorkspaceRunsByKey      map[string]map[string]any     `json:"workspaceRunsByKey"`
	WorkspaceFeatureEnabled *bool                         `json:"workspaceFeatureEnabled"`
	WorkspaceLastError      string                        `json:"workspaceLastError"`
	AgentMetaByID           map[string]AgentMeta          `json:"agentMetaById"`
}

// HistoryRecord is a compact history message for timeline hydration.
type HistoryRecord struct {
	ID        int64
	Role      string
	EventType string
	Method    string
	Content   string
	Metadata  json.RawMessage
	CreatedAt time.Time
}

type threadRuntime struct {
	thinkingIndex  int
	assistantIndex int
	commandIndex   int
	planIndex      int
	editingFiles   map[string]struct{}

	turnDepth     int
	approvalDepth int
	commandDepth  int
	fileEditDepth int
	toolCallDepth int
	collabDepth   int

	terminalWaitOverlay bool
	terminalWaitLabel   string
	mcpStartupOverlay   bool
	mcpStartupLabel     string
	backgroundOverlay   bool
	backgroundLabel     string
	backgroundDetails   string
	streamErrorText     string
	streamErrorDetails  string
	statusHeader        string
	reasoningHeaderBuf  string
	hasDerivedState     bool
}

func newThreadRuntime() *threadRuntime {
	return &threadRuntime{
		thinkingIndex:  -1,
		assistantIndex: -1,
		commandIndex:   -1,
		planIndex:      -1,
		editingFiles:   map[string]struct{}{},
	}
}

// RuntimeManager stores UI business runtime state in Go.
type RuntimeManager struct {
	mu sync.RWMutex // 保护 snapshot/runtime/seq

	snapshot RuntimeSnapshot
	runtime  map[string]*threadRuntime
	seq      uint64
}

// NewRuntimeManager creates an empty runtime manager.
func NewRuntimeManager() *RuntimeManager {
	return &RuntimeManager{
		snapshot: RuntimeSnapshot{
			Threads:               []ThreadSnapshot{},
			Statuses:              map[string]string{},
			InterruptibleByThread: map[string]bool{},
			StatusHeadersByThread: map[string]string{},
			StatusDetailsByThread: map[string]string{},
			TimelinesByThread:     map[string][]TimelineItem{},
			DiffTextByThread:      map[string]string{},
			TokenUsageByThread:    map[string]TokenUsageSnapshot{},
			WorkspaceRunsByKey:    map[string]map[string]any{},
			AgentMetaByID:         map[string]AgentMeta{},
		},
		runtime: map[string]*threadRuntime{},
	}
}

// Snapshot returns a deep-copied runtime snapshot for JSON-RPC responses.
func (m *RuntimeManager) Snapshot() RuntimeSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneSnapshot(m.snapshot)
}

// SnapshotLight returns a snapshot without timelines and diffs (the heaviest fields).
// Use ThreadTimeline / ThreadDiff to fetch specific thread data separately.
func (m *RuntimeManager) SnapshotLight() RuntimeSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneSnapshotLight(m.snapshot)
}

// ThreadTimeline returns a single thread's timeline items (read-only reference).
// Callers must NOT mutate the returned slice.
func (m *RuntimeManager) ThreadTimeline(threadID string) []TimelineItem {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.snapshot.TimelinesByThread[id]
	if len(src) == 0 {
		return []TimelineItem{}
	}
	return src
}

// ThreadDiff returns a single thread's diff text.
func (m *RuntimeManager) ThreadDiff(threadID string) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot.DiffTextByThread[id]
}

// ReplaceThreads upserts thread list and status snapshot.
func (m *RuntimeManager) ReplaceThreads(threads []ThreadSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	next := make([]ThreadSnapshot, 0, len(threads))
	for _, item := range threads {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		state := normalizeThreadState(item.State)
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		m.ensureThreadLocked(id)
		rt := m.runtime[id]
		if state != "" && !rt.hasDerivedState {
			m.snapshot.Statuses[id] = state
			m.snapshot.StatusHeadersByThread[id] = defaultStatusHeaderForState(state)
		}
		resolvedState := m.snapshot.Statuses[id]
		if resolvedState == "" {
			resolvedState = "idle"
			m.snapshot.Statuses[id] = resolvedState
		}
		if strings.TrimSpace(m.snapshot.StatusHeadersByThread[id]) == "" {
			m.snapshot.StatusHeadersByThread[id] = defaultStatusHeaderForState(resolvedState)
		}
		next = append(next, ThreadSnapshot{
			ID:    id,
			Name:  name,
			State: resolvedState,
		})
	}
	m.snapshot.Threads = next
}

// SetThreadName updates thread visible name and alias meta.
func (m *RuntimeManager) SetThreadName(threadID, name string) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	alias := strings.TrimSpace(name)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.ensureThreadLocked(id)
	for i := range m.snapshot.Threads {
		if m.snapshot.Threads[i].ID != id {
			continue
		}
		if alias != "" {
			m.snapshot.Threads[i].Name = alias
		} else {
			m.snapshot.Threads[i].Name = id
		}
		break
	}
	meta := m.snapshot.AgentMetaByID[id]
	meta.Alias = alias
	m.snapshot.AgentMetaByID[id] = meta
}

// SetMainAgent marks the selected main agent.
func (m *RuntimeManager) SetMainAgent(threadID string) {
	id := strings.TrimSpace(threadID)

	m.mu.Lock()
	defer m.mu.Unlock()

	for key, meta := range m.snapshot.AgentMetaByID {
		meta.IsMain = id != "" && key == id
		m.snapshot.AgentMetaByID[key] = meta
	}
	if id != "" {
		m.ensureThreadLocked(id)
		meta := m.snapshot.AgentMetaByID[id]
		meta.IsMain = true
		m.snapshot.AgentMetaByID[id] = meta
	}
}

// AppendUserMessage appends a user message into timeline.
func (m *RuntimeManager) AppendUserMessage(threadID, text string, attachments []TimelineAttachment) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureThreadLocked(id)
	m.appendUserLocked(id, text, attachments, time.Now())
}

// ClearThreadTimeline clears a single thread timeline and diff.
func (m *RuntimeManager) ClearThreadTimeline(threadID string) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureThreadLocked(id)
	m.snapshot.TimelinesByThread[id] = []TimelineItem{}
	m.snapshot.DiffTextByThread[id] = ""
	m.runtime[id] = newThreadRuntime()
}

// ApplyAgentEvent mutates runtime state by normalized backend events.
func (m *RuntimeManager) ApplyAgentEvent(threadID string, normalized NormalizedEvent, payload map[string]any) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.ensureThreadLocked(id)
	m.applyAgentEventLocked(id, normalized, payload, time.Now())
}

// TimelineStats returns per-thread timeline item counts for diagnostics.
func (m *RuntimeManager) TimelineStats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	perThread := map[string]int{}
	totalItems := 0
	for tid, items := range m.snapshot.TimelinesByThread {
		perThread[tid] = len(items)
		totalItems += len(items)
	}

	diffBytes := 0
	for _, d := range m.snapshot.DiffTextByThread {
		diffBytes += len(d)
	}

	return map[string]any{
		"threadCount":   len(m.snapshot.TimelinesByThread),
		"totalItems":    totalItems,
		"diffByteTotal": diffBytes,
		"perThread":     perThread,
	}
}

// hasAccumulatedText returns true if the timeline item at the given index
// exists and has non-empty Text (i.e. streaming deltas have been accumulated).
func hasAccumulatedText(timeline []TimelineItem, index int) bool {
	if index < 0 || index >= len(timeline) {
		return false
	}
	return timeline[index].Text != ""
}

// HydrateHistory rebuilds thread timeline from stored messages.
// Returns false if skipped (e.g. thread is actively streaming).
func (m *RuntimeManager) HydrateHistory(threadID string, records []HistoryRecord) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 若 thread 正在积累流式文本 (assistant/thinking delta 未完成),
	// 跳过 hydration 以免清空已累积的 delta。
	// 仅在 index 处的 item 已有非空 Text 时才视为"正在流式"。
	// turn_started 会设置 thinkingIndex 但 item.Text 仍为空 — 此时 hydration 仍应执行。
	if rt, ok := m.runtime[id]; ok {
		timeline := m.snapshot.TimelinesByThread[id]
		if hasAccumulatedText(timeline, rt.assistantIndex) ||
			hasAccumulatedText(timeline, rt.thinkingIndex) {
			return false
		}
	}

	m.ensureThreadLocked(id)
	m.snapshot.TimelinesByThread[id] = []TimelineItem{}
	m.snapshot.DiffTextByThread[id] = ""
	m.runtime[id] = newThreadRuntime()

	ordered := make([]HistoryRecord, 0, len(records))
	ordered = append(ordered, records...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	for _, rec := range ordered {
		ts := rec.CreatedAt
		if ts.IsZero() {
			ts = time.Now()
		}
		role := strings.ToLower(strings.TrimSpace(rec.Role))
		if role == "user" {
			var payload map[string]any
			if len(rec.Metadata) > 0 {
				_ = json.Unmarshal(rec.Metadata, &payload)
			}
			m.appendUserLocked(id, rec.Content, extractUserAttachmentsFromPayload(payload), ts)
			continue
		}

		var payload map[string]any
		if len(rec.Metadata) > 0 {
			_ = json.Unmarshal(rec.Metadata, &payload)
		}
		if payload == nil {
			payload = map[string]any{}
		}
		hydrateContentPayload(rec, payload)

		normalized := NormalizeEvent(rec.EventType, rec.Method, rec.Metadata)
		if normalized.Text == "" {
			normalized.Text = rec.Content
		}
		if normalized.UIType == UITypeSystem && role == "assistant" && strings.TrimSpace(rec.Content) != "" {
			normalized.UIType = UITypeAssistantDone
		}

		m.applyAgentEventLocked(id, normalized, payload, ts)
	}

	// 清理瞬态 overlay 状态: MCP/terminal/background overlay 依赖实时事件,
	// 不会被持久化, 因此 hydration 重放可能错误地重新启用 overlay。
	if rt := m.runtime[id]; rt != nil {
		rt.mcpStartupOverlay = false
		rt.mcpStartupLabel = ""
		rt.terminalWaitOverlay = false
		rt.terminalWaitLabel = ""
		rt.backgroundOverlay = false
		rt.backgroundLabel = ""
		rt.backgroundDetails = ""
	}
	return true
}

// AppendHistory appends additional history records without resetting the timeline.
// Used by streaming pages after the initial HydrateHistory call.
func (m *RuntimeManager) AppendHistory(threadID string, records []HistoryRecord) {
	id := strings.TrimSpace(threadID)
	if id == "" || len(records) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.ensureThreadLocked(id)

	ordered := make([]HistoryRecord, 0, len(records))
	ordered = append(ordered, records...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	for _, rec := range ordered {
		ts := rec.CreatedAt
		if ts.IsZero() {
			ts = time.Now()
		}
		role := strings.ToLower(strings.TrimSpace(rec.Role))
		if role == "user" {
			var payload map[string]any
			if len(rec.Metadata) > 0 {
				_ = json.Unmarshal(rec.Metadata, &payload)
			}
			m.appendUserLocked(id, rec.Content, extractUserAttachmentsFromPayload(payload), ts)
			continue
		}

		var payload map[string]any
		if len(rec.Metadata) > 0 {
			_ = json.Unmarshal(rec.Metadata, &payload)
		}
		if payload == nil {
			payload = map[string]any{}
		}
		hydrateContentPayload(rec, payload)

		normalized := NormalizeEvent(rec.EventType, rec.Method, rec.Metadata)
		if normalized.Text == "" {
			normalized.Text = rec.Content
		}
		if normalized.UIType == UITypeSystem && role == "assistant" && strings.TrimSpace(rec.Content) != "" {
			normalized.UIType = UITypeAssistantDone
		}

		m.applyAgentEventLocked(id, normalized, payload, ts)
	}
}

func hydrateContentPayload(rec HistoryRecord, payload map[string]any) {
	if rec.Content == "" {
		return
	}
	if _, ok := payload["delta"]; !ok {
		payload["delta"] = rec.Content
	}
	if _, ok := payload["text"]; !ok {
		payload["text"] = rec.Content
	}
	if _, ok := payload["content"]; !ok {
		payload["content"] = rec.Content
	}
	if _, ok := payload["output"]; !ok {
		payload["output"] = rec.Content
	}
}

func extractUserAttachmentsFromPayload(payload map[string]any) []TimelineAttachment {
	if payload == nil {
		return nil
	}
	rawInput, ok := payload["input"]
	if !ok {
		return nil
	}
	list, ok := rawInput.([]any)
	if !ok {
		return nil
	}
	attachments := make([]TimelineAttachment, 0, len(list))
	for _, raw := range list {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typeValue, _ := item["type"].(string)
		kind := strings.ToLower(strings.TrimSpace(typeValue))
		switch kind {
		case "image":
			path := extractFirstString(item, "url")
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			attachments = append(attachments, TimelineAttachment{
				Kind:       "image",
				Name:       attachmentName(path),
				Path:       path,
				PreviewURL: attachmentPreview(path),
			})
		case "localimage":
			path := strings.TrimSpace(extractFirstString(item, "path"))
			preview := strings.TrimSpace(extractFirstString(item, "url"))
			if preview == "" {
				preview = path
			}
			if preview == "" {
				continue
			}
			nameSource := path
			if nameSource == "" {
				nameSource = preview
			}
			attachments = append(attachments, TimelineAttachment{
				Kind:       "image",
				Name:       attachmentName(nameSource),
				Path:       path,
				PreviewURL: attachmentPreview(preview),
			})
		case "mention", "filecontent":
			path := extractFirstString(item, "path")
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			attachments = append(attachments, TimelineAttachment{
				Kind: "file",
				Name: attachmentName(path),
				Path: path,
			})
		}
	}
	return attachments
}

func attachmentName(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:image/") {
		ext := strings.TrimSpace(strings.TrimPrefix(lower, "data:image/"))
		if idx := strings.Index(ext, ";"); idx >= 0 {
			ext = ext[:idx]
		}
		ext = strings.TrimSpace(ext)
		if ext == "" {
			return "image"
		}
		return "image." + ext
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if parsed, err := url.Parse(value); err == nil {
			base := strings.TrimSpace(filepath.Base(parsed.Path))
			if base != "" && base != "." && base != string(filepath.Separator) {
				return base
			}
		}
		return value
	}
	base := strings.TrimSpace(filepath.Base(value))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return value
	}
	return base
}

func attachmentPreview(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:image/") ||
		strings.HasPrefix(lower, "file://") {
		return value
	}
	return (&url.URL{Scheme: "file", Path: value}).String()
}

// ReplaceWorkspaceRuns replaces workspace run cache.
func (m *RuntimeManager) ReplaceWorkspaceRuns(runs []map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := map[string]map[string]any{}
	for _, raw := range runs {
		runKey := extractRunKey(raw)
		if runKey == "" {
			continue
		}
		next[runKey] = copyMap(raw)
	}
	m.snapshot.WorkspaceRunsByKey = next
	flag := true
	m.snapshot.WorkspaceFeatureEnabled = &flag
	m.snapshot.WorkspaceLastError = ""
}

// UpsertWorkspaceRun upserts a workspace run item.
func (m *RuntimeManager) UpsertWorkspaceRun(raw map[string]any) {
	runKey := extractRunKey(raw)
	if runKey == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	prev := m.snapshot.WorkspaceRunsByKey[runKey]
	next := copyMap(prev)
	for k, v := range raw {
		next[k] = v
	}
	m.snapshot.WorkspaceRunsByKey[runKey] = next
	flag := true
	m.snapshot.WorkspaceFeatureEnabled = &flag
	m.snapshot.WorkspaceLastError = ""
}

// ApplyWorkspaceMergeResult merges merge-result metrics into a run.
func (m *RuntimeManager) ApplyWorkspaceMergeResult(runKey string, result map[string]any) {
	key := strings.TrimSpace(runKey)
	if key == "" {
		key = extractRunKey(result)
	}
	if key == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	prev := m.snapshot.WorkspaceRunsByKey[key]
	next := copyMap(prev)
	next["runKey"] = key
	if value, ok := result["status"]; ok {
		next["status"] = value
	}
	if value, ok := result["workspace"]; ok {
		next["workspacePath"] = value
	}
	for _, k := range []string{"merged", "conflicts", "unchanged", "errors", "finishedAt", "dryRun"} {
		if value, ok := result[k]; ok {
			next[k] = value
		}
	}
	m.snapshot.WorkspaceRunsByKey[key] = next
	flag := true
	m.snapshot.WorkspaceFeatureEnabled = &flag
	m.snapshot.WorkspaceLastError = ""
}

// SetWorkspaceUnavailable marks workspace feature unavailable.
func (m *RuntimeManager) SetWorkspaceUnavailable(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	flag := false
	m.snapshot.WorkspaceFeatureEnabled = &flag
	m.snapshot.WorkspaceLastError = strings.TrimSpace(message)
}

type resolvedFields struct {
	text     string
	command  string
	file     string
	files    []string
	exitCode *int
	planDone *bool
	planSet  bool
}

type runtimeEventHandler func(*RuntimeManager, string, resolvedFields, map[string]any, time.Time)

var runtimeEventHandlers = map[UIType]runtimeEventHandler{
	UITypeTurnStarted:     handleTurnStartedEvent,
	UITypeTurnComplete:    handleTurnCompleteEvent,
	UITypeAssistantDelta:  handleAssistantDeltaEvent,
	UITypeAssistantDone:   handleAssistantDoneEvent,
	UITypeReasoningDelta:  handleReasoningDeltaEvent,
	UITypeCommandStart:    handleCommandStartEvent,
	UITypeCommandOutput:   handleCommandOutputEvent,
	UITypeCommandDone:     handleCommandDoneEvent,
	UITypeFileEditStart:   handleFileEditStartEvent,
	UITypeFileEditDone:    handleFileEditDoneEvent,
	UITypeToolCall:        handleToolCallEvent,
	UITypeApprovalRequest: handleApprovalRequestEvent,
	UITypePlanDelta:       handlePlanDeltaEvent,
	UITypeDiffUpdate:      handleDiffUpdateEvent,
	UITypeUserMessage:     handleUserMessageEvent,
	UITypeError:           handleErrorEvent,
}

func resolveEventFields(normalized NormalizedEvent, payload map[string]any) resolvedFields {
	fields := resolvedFields{
		text:    strings.TrimSpace(normalized.Text),
		command: strings.TrimSpace(normalized.Command),
		file:    strings.TrimSpace(normalized.File),
		files:   nil,
	}
	if fields.text == "" {
		fields.text = extractFirstString(payload, "uiText", "delta", "text", "content", "output", "message")
	}
	if fields.command == "" {
		fields.command = extractFirstString(payload, "uiCommand", "command")
	}
	if fields.file == "" {
		fields.file = extractFirstString(payload, "uiFile", "file")
	}
	fields.files = normalizeFilesAny(payload["uiFiles"])
	if len(fields.files) == 0 {
		fields.files = normalizeFilesAny(payload["files"])
	}
	if len(fields.files) == 0 && len(normalized.Files) > 0 {
		fields.files = append(fields.files, normalized.Files...)
	}
	if fields.file != "" && len(fields.files) == 0 {
		fields.files = []string{fields.file}
	}
	fields.exitCode = normalized.ExitCode
	if fields.exitCode != nil {
		return fields
	}
	if code, ok := extractExitCode(payload["uiExitCode"]); ok {
		fields.exitCode = &code
		return fields
	}
	if code, ok := extractExitCode(payload["exit_code"]); ok {
		fields.exitCode = &code
	}
	if planText, planDone, ok := extractPlanSnapshot(payload); ok {
		fields.text = planText
		fields.planSet = true
		fields.planDone = &planDone
	}
	return fields
}

func (m *RuntimeManager) applyAgentEventLocked(threadID string, normalized NormalizedEvent, payload map[string]any, ts time.Time) {
	m.markAgentActiveLocked(threadID, ts)
	rt := m.runtime[threadID]
	rt.hasDerivedState = true
	fields := resolveEventFields(normalized, payload)
	m.applyLifecycleStateLocked(threadID, normalized, payload, fields, ts)
	if handler, ok := runtimeEventHandlers[normalized.UIType]; ok {
		handler(m, threadID, fields, payload, ts)
	}
	nextState := m.deriveThreadStateLocked(threadID)
	m.setThreadStateLocked(threadID, nextState)
	m.snapshot.StatusHeadersByThread[threadID] = m.deriveThreadStatusHeaderLocked(threadID, nextState)
	m.snapshot.StatusDetailsByThread[threadID] = m.deriveThreadStatusDetailsLocked(threadID, nextState)
}

func (m *RuntimeManager) applyLifecycleStateLocked(threadID string, normalized NormalizedEvent, payload map[string]any, fields resolvedFields, ts time.Time) {
	rt := m.runtime[threadID]
	eventType := strings.ToLower(strings.TrimSpace(normalized.RawType))
	method := strings.TrimSpace(normalized.Method)

	if normalized.UIType == UITypeError {
		text := strings.TrimSpace(fields.text)
		if text == "" {
			text = "发生错误"
		}
		rt.streamErrorText = text
		if eventType == "stream_error" {
			rt.streamErrorDetails = deriveStreamErrorDetails(payload)
		} else {
			rt.streamErrorDetails = ""
		}
	} else if eventType != "stream_error" {
		rt.streamErrorText = ""
		rt.streamErrorDetails = ""
	}

	if isTerminalInteractionEvent(eventType, method) {
		if isTerminalWaitPayload(payload) {
			rt.terminalWaitOverlay = true
			rt.terminalWaitLabel = deriveTerminalWaitLabel(payload)
		} else {
			rt.terminalWaitOverlay = false
			rt.terminalWaitLabel = ""
		}
	}
	if isMCPStartupUpdateEvent(eventType, method) {
		rt.mcpStartupOverlay = true
		rt.mcpStartupLabel = deriveMCPStartupLabel(payload)
	}
	if isMCPStartupCompleteEvent(eventType, method) {
		rt.mcpStartupOverlay = false
		rt.mcpStartupLabel = ""
	}
	if isBackgroundEvent(eventType, method) {
		if shouldClearBackgroundOverlay(payload) {
			rt.backgroundOverlay = false
			rt.backgroundLabel = ""
			rt.backgroundDetails = ""
		} else {
			rt.backgroundOverlay = true
			rt.backgroundLabel = deriveBackgroundLabel(payload)
			rt.backgroundDetails = deriveBackgroundDetails(payload)
		}
	}

	if eventType == "collab_agent_spawn_begin" || eventType == "collab_agent_interaction_begin" || eventType == "collab_waiting_begin" {
		rt.collabDepth += 1
	} else if eventType == "collab_agent_spawn_end" || eventType == "collab_agent_interaction_end" || eventType == "collab_waiting_end" {
		rt.collabDepth = max(0, rt.collabDepth-1)
	}

	if eventType == "token_count" || eventType == "context_compacted" || method == "thread/tokenUsage/updated" || method == "thread/compacted" {
		m.updateTokenUsageLocked(threadID, payload, ts)
	}

	switch normalized.UIType {
	case UITypeTurnStarted:
		m.clearTurnLifecycleLocked(threadID)
		rt.turnDepth = 1
		rt.approvalDepth = 0
		rt.terminalWaitOverlay = false
		rt.terminalWaitLabel = ""
		rt.statusHeader = "工作中"
	case UITypeTurnComplete:
		m.clearTurnLifecycleLocked(threadID)
		// mcp_startup_complete 为主清理信号，但历史/重连链路可能丢失 complete，
		// turn 收敛时兜底清理，避免 “MCP 启动中” 状态残留。
		rt.mcpStartupOverlay = false
		rt.mcpStartupLabel = ""
	case UITypeReasoningDelta:
		if rt.turnDepth == 0 {
			rt.turnDepth = 1
		}
		if isReasoningSectionBreakEvent(eventType, method) {
			rt.reasoningHeaderBuf = ""
		}
		m.captureReasoningHeaderLocked(threadID, fields.text)
	case UITypeCommandStart:
		rt.commandDepth += 1
		rt.approvalDepth = 0
		rt.terminalWaitOverlay = false
		rt.terminalWaitLabel = ""
	case UITypeCommandOutput:
		if rt.commandDepth == 0 {
			rt.commandDepth = 1
		}
		rt.terminalWaitOverlay = false
		rt.terminalWaitLabel = ""
	case UITypeCommandDone:
		rt.commandDepth = max(0, rt.commandDepth-1)
		rt.terminalWaitOverlay = false
		rt.terminalWaitLabel = ""
	case UITypeFileEditStart:
		rt.fileEditDepth += 1
		rt.approvalDepth = 0
	case UITypeFileEditDone:
		rt.fileEditDepth = max(0, rt.fileEditDepth-1)
	case UITypeApprovalRequest:
		rt.approvalDepth += 1
	case UITypeToolCall:
		if eventType == "mcp_tool_call_begin" {
			rt.toolCallDepth += 1
		}
		if eventType == "mcp_tool_call_end" {
			rt.toolCallDepth = max(0, rt.toolCallDepth-1)
		}
	}
}

func (m *RuntimeManager) clearTurnLifecycleLocked(threadID string) {
	rt := m.runtime[threadID]
	rt.turnDepth = 0
	rt.approvalDepth = 0
	rt.commandDepth = 0
	rt.fileEditDepth = 0
	rt.toolCallDepth = 0
	rt.collabDepth = 0
	rt.terminalWaitOverlay = false
	rt.terminalWaitLabel = ""
	rt.backgroundOverlay = false
	rt.backgroundLabel = ""
	rt.backgroundDetails = ""
	rt.streamErrorText = ""
	rt.streamErrorDetails = ""
	rt.statusHeader = ""
	rt.reasoningHeaderBuf = ""
}

func (m *RuntimeManager) deriveThreadStateLocked(threadID string) string {
	rt := m.runtime[threadID]
	switch {
	case strings.TrimSpace(rt.streamErrorText) != "":
		return "error"
	case rt.terminalWaitOverlay:
		return "waiting"
	case rt.approvalDepth > 0:
		return "waiting"
	case rt.fileEditDepth > 0:
		return "editing"
	case rt.commandDepth > 0 || rt.toolCallDepth > 0 || rt.collabDepth > 0:
		return "running"
	case rt.turnDepth > 0:
		return "thinking"
	case rt.mcpStartupOverlay:
		return "syncing"
	default:
		return "idle"
	}
}

func (m *RuntimeManager) deriveThreadStatusHeaderLocked(threadID, state string) string {
	rt := m.runtime[threadID]
	switch {
	case strings.TrimSpace(rt.streamErrorText) != "":
		return rt.streamErrorText
	case rt.terminalWaitOverlay:
		if strings.TrimSpace(rt.terminalWaitLabel) != "" {
			return rt.terminalWaitLabel
		}
		return "等待后台终端"
	case shouldShowMCPStartupOverlay(rt):
		if strings.TrimSpace(rt.mcpStartupLabel) != "" {
			return rt.mcpStartupLabel
		}
		return "MCP 启动中"
	case rt.backgroundOverlay:
		if strings.TrimSpace(rt.backgroundLabel) != "" {
			return rt.backgroundLabel
		}
		return "后台处理中"
	case rt.approvalDepth > 0:
		return "等待确认"
	case shouldUseReasoningHeader(rt):
		return rt.statusHeader
	}
	switch state {
	case "running":
		return "工作中"
	case "editing":
		return "工作中"
	case "thinking":
		return "工作中"
	case "responding":
		return "工作中"
	case "starting":
		return "启动中"
	case "waiting":
		return "等待确认"
	case "syncing":
		return "同步中"
	case "error":
		return "异常"
	default:
		return "等待指示"
	}
}

func defaultStatusHeaderForState(state string) string {
	switch normalizeThreadState(state) {
	case "starting":
		return "启动中"
	case "waiting":
		return "等待确认"
	case "syncing":
		return "同步中"
	case "error":
		return "异常"
	case "running", "editing", "thinking", "responding":
		return "工作中"
	default:
		return "等待指示"
	}
}

func (m *RuntimeManager) deriveThreadStatusDetailsLocked(threadID, state string) string {
	rt := m.runtime[threadID]
	switch {
	case strings.TrimSpace(rt.streamErrorText) != "":
		return strings.TrimSpace(rt.streamErrorDetails)
	case rt.terminalWaitOverlay:
		return "命令正在等待终端输入"
	case shouldShowMCPStartupOverlay(rt):
		return "正在初始化 MCP 服务"
	case rt.backgroundOverlay:
		if strings.TrimSpace(rt.backgroundDetails) != "" {
			return rt.backgroundDetails
		}
		return "后台事件处理中"
	case rt.approvalDepth > 0:
		return "等待用户审批后继续"
	case shouldUseReasoningHeader(rt):
		return "根据推理标题展示当前阶段"
	}
	switch state {
	case "running":
		return "命令或工具正在执行"
	case "editing":
		return "文件修改进行中"
	case "thinking":
		return "模型推理中"
	case "syncing":
		return "后台同步中"
	case "error":
		return "运行出现异常"
	default:
		return ""
	}
}

func shouldShowMCPStartupOverlay(rt *threadRuntime) bool {
	if rt == nil || !rt.mcpStartupOverlay {
		return false
	}
	return rt.turnDepth == 0 &&
		rt.approvalDepth == 0 &&
		rt.commandDepth == 0 &&
		rt.fileEditDepth == 0 &&
		rt.toolCallDepth == 0 &&
		rt.collabDepth == 0
}

func isTerminalInteractionEvent(eventType, method string) bool {
	return eventType == "exec_terminal_interaction" || eventType == "item/commandexecution/terminalinteraction" || strings.EqualFold(method, "item/commandExecution/terminalInteraction")
}

func isMCPStartupUpdateEvent(eventType, method string) bool {
	return eventType == "mcp_startup_update" ||
		eventType == "codex/event/mcp_startup_update" ||
		eventType == "agent/event/mcp_startup_update" ||
		strings.EqualFold(method, "codex/event/mcp_startup_update") ||
		strings.EqualFold(method, "agent/event/mcp_startup_update")
}

func isMCPStartupCompleteEvent(eventType, method string) bool {
	return eventType == "mcp_startup_complete" ||
		eventType == "codex/event/mcp_startup_complete" ||
		eventType == "agent/event/mcp_startup_complete" ||
		strings.EqualFold(method, "codex/event/mcp_startup_complete") ||
		strings.EqualFold(method, "agent/event/mcp_startup_complete")
}

func isTerminalWaitPayload(payload map[string]any) bool {
	if payload == nil {
		return true
	}
	if value, ok := payload["stdin"]; ok {
		switch v := value.(type) {
		case nil:
			return true
		case string:
			return strings.TrimSpace(v) == ""
		case []any:
			return len(v) == 0
		case []string:
			return len(v) == 0
		default:
			return false
		}
	}
	return true
}

func deriveTerminalWaitLabel(payload map[string]any) string {
	command := strings.TrimSpace(extractFirstString(payload, "command", "command_display", "displayCommand"))
	if command == "" {
		command = strings.TrimSpace(extractNestedFirstString(payload, []string{"process", "command_display"}, []string{"process", "command"}))
	}
	if command == "" {
		return "等待后台终端"
	}
	return "等待后台终端 · " + command
}

func deriveMCPStartupLabel(payload map[string]any) string {
	server := strings.TrimSpace(extractFirstString(payload, "server", "name"))
	if server == "" {
		server = strings.TrimSpace(extractNestedFirstString(payload, []string{"status", "server"}, []string{"msg", "server"}))
	}
	if server == "" {
		return "MCP 启动中"
	}
	return "MCP 启动中 · " + server
}

func isReasoningSectionBreakEvent(eventType, method string) bool {
	return eventType == "agent_reasoning_section_break" ||
		strings.EqualFold(method, "agent/reasoningSectionBreak")
}

func isBackgroundEvent(eventType, method string) bool {
	return eventType == "background_event" ||
		eventType == "codex/event/background_event" ||
		strings.EqualFold(method, "codex/event/background_event")
}

func shouldClearBackgroundOverlay(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if done, ok := payload["done"].(bool); ok {
		return done
	}
	if active, ok := payload["active"].(bool); ok {
		return !active
	}
	status := strings.ToLower(strings.TrimSpace(extractFirstString(payload, "status", "state", "phase")))
	switch status {
	case "done", "completed", "complete", "finished", "success", "succeeded", "idle", "stopped", "closed", "ended":
		return true
	default:
		return false
	}
}

func deriveBackgroundLabel(payload map[string]any) string {
	text := extractFirstString(payload, "uiHeader", "statusHeader", "title", "event", "name")
	if strings.TrimSpace(text) == "" {
		text = extractFirstString(payload, "message", "text", "content")
	}
	if strings.TrimSpace(text) == "" {
		text = extractNestedFirstString(
			payload,
			[]string{"msg", "title"},
			[]string{"msg", "text"},
			[]string{"data", "title"},
			[]string{"data", "text"},
		)
	}
	text = compactOneLine(text, 48)
	if text == "" {
		return "后台处理中"
	}
	if strings.HasPrefix(text, "后台") {
		return text
	}
	return "后台处理中 · " + text
}

func deriveBackgroundDetails(payload map[string]any) string {
	text := extractFirstString(payload, "details", "detail", "description", "message", "text", "content")
	if strings.TrimSpace(text) == "" {
		text = extractNestedFirstString(
			payload,
			[]string{"msg", "details"},
			[]string{"msg", "text"},
			[]string{"data", "details"},
			[]string{"data", "text"},
		)
	}
	text = compactOneLine(text, 120)
	if text == "" {
		return "后台事件处理中"
	}
	return text
}

func deriveStreamErrorDetails(payload map[string]any) string {
	text := extractFirstString(payload, "additional_details", "additionalDetails", "details")
	if strings.TrimSpace(text) == "" {
		text = extractNestedFirstString(
			payload,
			[]string{"msg", "additional_details"},
			[]string{"msg", "details"},
			[]string{"data", "additional_details"},
			[]string{"data", "details"},
		)
	}
	return compactOneLine(text, 180)
}

func shouldUseReasoningHeader(rt *threadRuntime) bool {
	if rt == nil {
		return false
	}
	if strings.TrimSpace(rt.statusHeader) == "" {
		return false
	}
	if rt.turnDepth <= 0 {
		return false
	}
	if rt.approvalDepth > 0 || rt.commandDepth > 0 || rt.fileEditDepth > 0 || rt.toolCallDepth > 0 || rt.collabDepth > 0 {
		return false
	}
	if rt.terminalWaitOverlay || rt.mcpStartupOverlay || rt.backgroundOverlay {
		return false
	}
	return strings.TrimSpace(rt.streamErrorText) == ""
}

func (m *RuntimeManager) captureReasoningHeaderLocked(threadID, delta string) {
	rt := m.runtime[threadID]
	if rt == nil {
		return
	}
	header, buf := extractReasoningHeader(rt.reasoningHeaderBuf, delta)
	rt.reasoningHeaderBuf = buf
	if strings.TrimSpace(header) == "" {
		return
	}
	rt.statusHeader = header
}

func extractReasoningHeader(buffer, delta string) (string, string) {
	merged := buffer + delta
	merged = compactOneLine(merged, 512)
	if merged == "" {
		return "", ""
	}
	start := strings.Index(merged, "**")
	if start < 0 {
		return "", merged
	}
	rest := merged[start+2:]
	end := strings.Index(rest, "**")
	if end < 0 {
		return "", merged[start:]
	}
	header := compactOneLine(rest[:end], 80)
	if header == "" {
		return "", compactOneLine(rest[end+2:], 512)
	}
	return header, ""
}

func compactOneLine(text string, limit int) string {
	cleaned := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if cleaned == "" {
		return ""
	}
	if limit <= 0 {
		return cleaned
	}
	runes := []rune(cleaned)
	if len(runes) <= limit {
		return cleaned
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func handleTurnStartedEvent(m *RuntimeManager, threadID string, _ resolvedFields, _ map[string]any, ts time.Time) {
	m.completeTurnLocked(threadID, ts)
	m.startThinkingLocked(threadID, ts)
}

func handleTurnCompleteEvent(m *RuntimeManager, threadID string, _ resolvedFields, _ map[string]any, ts time.Time) {
	m.completeTurnLocked(threadID, ts)
}

func handleAssistantDeltaEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.appendAssistantLocked(threadID, fields.text, ts)
}

func handleAssistantDoneEvent(m *RuntimeManager, threadID string, _ resolvedFields, _ map[string]any, _ time.Time) {
	m.finishAssistantLocked(threadID)
}

func handleReasoningDeltaEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.appendThinkingLocked(threadID, fields.text, ts)
}

func handleCommandStartEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.startCommandLocked(threadID, fields.command, ts)
}

func handleCommandOutputEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.appendCommandOutputLocked(threadID, fields.text, ts)
}

func handleCommandDoneEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, _ time.Time) {
	m.finishCommandLocked(threadID, fields.exitCode)
}

func handleFileEditStartEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	for _, file := range fields.files {
		m.fileEditingLocked(threadID, file, ts)
	}
	m.rememberEditingFilesLocked(threadID, fields.files)
}

func handleFileEditDoneEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	saved := fields.files
	if len(saved) == 0 {
		saved = m.consumeEditingFilesLocked(threadID)
	}
	for _, file := range saved {
		m.fileSavedLocked(threadID, file, ts)
	}
}

func handleToolCallEvent(m *RuntimeManager, threadID string, _ resolvedFields, payload map[string]any, ts time.Time) {
	m.appendToolCallLocked(threadID, payload, ts)
}

func handleApprovalRequestEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.showApprovalLocked(threadID, fields.command, ts)
}

func handlePlanDeltaEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	if fields.planSet {
		planDone := false
		if fields.planDone != nil {
			planDone = *fields.planDone
		}
		m.setPlanLocked(threadID, fields.text, planDone, ts)
		return
	}
	m.appendPlanLocked(threadID, fields.text, ts)
}

func handleDiffUpdateEvent(m *RuntimeManager, threadID string, _ resolvedFields, payload map[string]any, _ time.Time) {
	diff := extractFirstString(payload, "diff", "uiText", "text", "content")
	m.snapshot.DiffTextByThread[threadID] = diff
}

func handleUserMessageEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	m.appendUserLocked(threadID, fields.text, nil, ts)
}

func handleErrorEvent(m *RuntimeManager, threadID string, fields resolvedFields, _ map[string]any, ts time.Time) {
	text := fields.text
	if text == "" {
		text = "发生错误"
	}
	m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind: "error",
		Text: text,
	}, ts)
}

func (m *RuntimeManager) ensureThreadLocked(threadID string) {
	if m.snapshot.StatusHeadersByThread == nil {
		m.snapshot.StatusHeadersByThread = map[string]string{}
	}
	if m.snapshot.StatusDetailsByThread == nil {
		m.snapshot.StatusDetailsByThread = map[string]string{}
	}
	if m.snapshot.TokenUsageByThread == nil {
		m.snapshot.TokenUsageByThread = map[string]TokenUsageSnapshot{}
	}
	if _, ok := m.snapshot.TimelinesByThread[threadID]; !ok {
		m.snapshot.TimelinesByThread[threadID] = []TimelineItem{}
	}
	if _, ok := m.snapshot.DiffTextByThread[threadID]; !ok {
		m.snapshot.DiffTextByThread[threadID] = ""
	}
	if _, ok := m.snapshot.Statuses[threadID]; !ok {
		m.snapshot.Statuses[threadID] = "idle"
	}
	if _, ok := m.snapshot.StatusHeadersByThread[threadID]; !ok {
		m.snapshot.StatusHeadersByThread[threadID] = "等待指示"
	}
	if _, ok := m.snapshot.StatusDetailsByThread[threadID]; !ok {
		m.snapshot.StatusDetailsByThread[threadID] = ""
	}
	if _, ok := m.snapshot.AgentMetaByID[threadID]; !ok {
		m.snapshot.AgentMetaByID[threadID] = AgentMeta{}
	}
	if _, ok := m.runtime[threadID]; !ok {
		m.runtime[threadID] = newThreadRuntime()
	}
}

func (m *RuntimeManager) setThreadStateLocked(threadID, state string) {
	normalized := normalizeThreadState(state)
	if normalized == "" {
		return
	}
	m.snapshot.Statuses[threadID] = normalized
	for i := range m.snapshot.Threads {
		if m.snapshot.Threads[i].ID == threadID {
			m.snapshot.Threads[i].State = normalized
			break
		}
	}
}

func (m *RuntimeManager) markAgentActiveLocked(threadID string, ts time.Time) {
	meta := m.snapshot.AgentMetaByID[threadID]
	if ts.IsZero() {
		ts = time.Now()
	}
	meta.LastActiveAt = ts.UTC().Format(time.RFC3339)
	m.snapshot.AgentMetaByID[threadID] = meta
}

func (m *RuntimeManager) nextItemIDLocked(kind string) string {
	m.seq += 1
	if kind == "" {
		kind = "item"
	}
	return fmt.Sprintf("%s-%d-%d", kind, time.Now().UnixMilli(), m.seq)
}

func (m *RuntimeManager) pushTimelineItemLocked(threadID string, item TimelineItem, ts time.Time) int {
	list := append([]TimelineItem{}, m.snapshot.TimelinesByThread[threadID]...)
	item.ID = m.nextItemIDLocked(item.Kind)
	if ts.IsZero() {
		ts = time.Now()
	}
	item.Ts = ts.UTC().Format(time.RFC3339)
	list = append(list, item)
	m.snapshot.TimelinesByThread[threadID] = list
	return len(list) - 1
}

func (m *RuntimeManager) patchTimelineItemLocked(threadID string, index int, patch func(*TimelineItem)) {
	list := append([]TimelineItem{}, m.snapshot.TimelinesByThread[threadID]...)
	if index < 0 || index >= len(list) {
		return
	}
	item := list[index]
	patch(&item)
	list[index] = item
	m.snapshot.TimelinesByThread[threadID] = list
}

func (m *RuntimeManager) timelineLocked(threadID string) []TimelineItem {
	return m.snapshot.TimelinesByThread[threadID]
}

func (m *RuntimeManager) appendUserLocked(threadID, text string, attachments []TimelineAttachment, ts time.Time) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" && len(attachments) == 0 {
		return
	}
	m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind:        "user",
		Text:        text,
		Attachments: attachments,
	}, ts)
}

func (m *RuntimeManager) startThinkingLocked(threadID string, ts time.Time) {
	rt := m.runtime[threadID]
	if rt.thinkingIndex >= 0 {
		return
	}
	rt.thinkingIndex = m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind: "thinking",
		Text: "",
		Done: false,
	}, ts)
}

func (m *RuntimeManager) appendThinkingLocked(threadID, delta string, ts time.Time) {
	if delta == "" {
		return
	}
	rt := m.runtime[threadID]
	if rt.thinkingIndex < 0 {
		m.startThinkingLocked(threadID, ts)
	}
	index := rt.thinkingIndex
	list := m.timelineLocked(threadID)
	if index < 0 || index >= len(list) {
		return
	}
	m.patchTimelineItemLocked(threadID, index, func(item *TimelineItem) {
		item.Text = item.Text + delta
	})
}

func (m *RuntimeManager) finishThinkingLocked(threadID string) {
	rt := m.runtime[threadID]
	index := rt.thinkingIndex
	if index < 0 {
		return
	}

	list := append([]TimelineItem{}, m.timelineLocked(threadID)...)
	if index >= len(list) {
		rt.thinkingIndex = -1
		return
	}
	item := list[index]
	if strings.TrimSpace(item.Text) == "" {
		list = append(list[:index], list[index+1:]...)
		m.snapshot.TimelinesByThread[threadID] = list
		m.shiftRuntimeIndicesAfterRemoveLocked(rt, index)
		rt.thinkingIndex = -1
		return
	}

	m.patchTimelineItemLocked(threadID, index, func(item *TimelineItem) {
		item.Done = true
	})
	rt.thinkingIndex = -1
}

func (m *RuntimeManager) shiftRuntimeIndicesAfterRemoveLocked(rt *threadRuntime, removedIndex int) {
	indices := []*int{&rt.thinkingIndex, &rt.assistantIndex, &rt.commandIndex, &rt.planIndex}
	for _, idx := range indices {
		if *idx > removedIndex {
			*idx = *idx - 1
		} else if *idx == removedIndex {
			*idx = -1
		}
	}
}

func (m *RuntimeManager) startAssistantLocked(threadID string, ts time.Time) {
	m.finishThinkingLocked(threadID)
	rt := m.runtime[threadID]
	if rt.assistantIndex >= 0 {
		return
	}
	rt.assistantIndex = m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind: "assistant",
		Text: "",
	}, ts)
}

func (m *RuntimeManager) appendAssistantLocked(threadID, delta string, ts time.Time) {
	if delta == "" {
		return
	}
	rt := m.runtime[threadID]
	if rt.assistantIndex < 0 {
		m.startAssistantLocked(threadID, ts)
	}
	index := rt.assistantIndex
	list := m.timelineLocked(threadID)
	if index < 0 || index >= len(list) {
		return
	}
	m.patchTimelineItemLocked(threadID, index, func(item *TimelineItem) {
		item.Text = item.Text + delta
	})
}

func (m *RuntimeManager) finishAssistantLocked(threadID string) {
	m.runtime[threadID].assistantIndex = -1
}

func (m *RuntimeManager) startCommandLocked(threadID, command string, ts time.Time) {
	m.finishThinkingLocked(threadID)
	rt := m.runtime[threadID]
	rt.commandIndex = m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind:    "command",
		Command: command,
		Output:  "",
		Status:  "running",
	}, ts)
}

func (m *RuntimeManager) appendCommandOutputLocked(threadID, output string, ts time.Time) {
	if output == "" {
		return
	}
	rt := m.runtime[threadID]
	if rt.commandIndex < 0 {
		m.startCommandLocked(threadID, "", ts)
	}
	index := rt.commandIndex
	list := m.timelineLocked(threadID)
	if index < 0 || index >= len(list) {
		return
	}
	m.patchTimelineItemLocked(threadID, index, func(item *TimelineItem) {
		item.Output = item.Output + output
	})
}

func (m *RuntimeManager) finishCommandLocked(threadID string, exitCode *int) {
	rt := m.runtime[threadID]
	index := rt.commandIndex
	if index < 0 {
		return
	}
	code := 0
	if exitCode != nil {
		code = *exitCode
	}
	m.patchTimelineItemLocked(threadID, index, func(item *TimelineItem) {
		if code == 0 {
			item.Status = "completed"
		} else {
			item.Status = "failed"
		}
		local := code
		item.ExitCode = &local
	})
	rt.commandIndex = -1
}

func (m *RuntimeManager) fileEditingLocked(threadID, file string, ts time.Time) {
	m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind:   "file",
		File:   file,
		Status: "editing",
	}, ts)
}

func (m *RuntimeManager) fileSavedLocked(threadID, file string, ts time.Time) {
	list := append([]TimelineItem{}, m.timelineLocked(threadID)...)
	for i := len(list) - 1; i >= 0; i-- {
		item := list[i]
		if item.Kind == "file" && item.Status == "editing" && (item.File == file || file == "") {
			m.patchTimelineItemLocked(threadID, i, func(v *TimelineItem) {
				v.Status = "saved"
			})
			return
		}
	}
	m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind:   "file",
		File:   file,
		Status: "saved",
	}, ts)
}

func (m *RuntimeManager) rememberEditingFilesLocked(threadID string, files []string) {
	rt := m.runtime[threadID]
	for _, file := range files {
		value := strings.TrimSpace(file)
		if value == "" {
			continue
		}
		rt.editingFiles[value] = struct{}{}
	}
}

func (m *RuntimeManager) consumeEditingFilesLocked(threadID string) []string {
	rt := m.runtime[threadID]
	files := make([]string, 0, len(rt.editingFiles))
	for file := range rt.editingFiles {
		files = append(files, file)
	}
	sort.Strings(files)
	rt.editingFiles = map[string]struct{}{}
	return files
}

func (m *RuntimeManager) flushEditingFilesAsSavedLocked(threadID string, ts time.Time) {
	files := m.consumeEditingFilesLocked(threadID)
	for _, file := range files {
		m.fileSavedLocked(threadID, file, ts)
	}
}

func (m *RuntimeManager) appendToolCallLocked(threadID string, payload map[string]any, ts time.Time) {
	tool := extractFirstString(payload, "tool", "tool_name")
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return
	}

	file := strings.TrimSpace(extractFirstString(payload, "file", "file_path"))
	preview := strings.TrimSpace(extractFirstString(payload, "resultPreview", "preview", "text", "content"))
	status := "ok"
	if value, ok := payload["success"].(bool); ok && !value {
		status = "failed"
	}
	var elapsedMS *int
	if v, ok := extractExitCode(payload["elapsedMs"]); ok {
		local := v
		elapsedMS = &local
	}

	list := m.timelineLocked(threadID)
	lastIndex := len(list) - 1
	if lastIndex >= 0 {
		last := list[lastIndex]
		if canMergeToolCall(last, tool, file, preview, elapsedMS) {
			m.patchTimelineItemLocked(threadID, lastIndex, func(item *TimelineItem) {
				if item.File == "" {
					item.File = file
				}
				if item.Preview == "" {
					item.Preview = preview
				}
				if item.ElapsedMS == nil {
					item.ElapsedMS = elapsedMS
				}
				if status == "failed" {
					item.Status = "failed"
				}
			})
			return
		}
	}

	m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind:      "tool",
		Tool:      tool,
		File:      file,
		Status:    status,
		ElapsedMS: elapsedMS,
		Preview:   preview,
	}, ts)
}

func canMergeToolCall(last TimelineItem, tool, file, preview string, elapsedMS *int) bool {
	if last.Kind != "tool" || last.Tool != tool {
		return false
	}
	return (last.File == "" && file != "") ||
		(last.Preview == "" && preview != "") ||
		(last.ElapsedMS == nil && elapsedMS != nil)
}

func (m *RuntimeManager) showApprovalLocked(threadID, command string, ts time.Time) {
	m.pushTimelineItemLocked(threadID, TimelineItem{
		Kind:    "approval",
		Command: command,
		Status:  "pending",
	}, ts)
}

func (m *RuntimeManager) appendPlanLocked(threadID, delta string, ts time.Time) {
	if delta == "" {
		return
	}
	rt := m.runtime[threadID]
	if rt.planIndex < 0 {
		rt.planIndex = m.pushTimelineItemLocked(threadID, TimelineItem{
			Kind: "plan",
			Text: "",
			Done: false,
		}, ts)
	}
	index := rt.planIndex
	list := m.timelineLocked(threadID)
	if index < 0 || index >= len(list) {
		return
	}
	m.patchTimelineItemLocked(threadID, index, func(item *TimelineItem) {
		item.Text = item.Text + delta
	})
}

func (m *RuntimeManager) setPlanLocked(threadID, text string, done bool, ts time.Time) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	rt := m.runtime[threadID]
	if rt.planIndex < 0 {
		rt.planIndex = m.pushTimelineItemLocked(threadID, TimelineItem{
			Kind: "plan",
			Text: trimmed,
			Done: done,
		}, ts)
		return
	}
	index := rt.planIndex
	list := m.timelineLocked(threadID)
	if index < 0 || index >= len(list) {
		return
	}
	m.patchTimelineItemLocked(threadID, index, func(item *TimelineItem) {
		item.Text = trimmed
		item.Done = done
	})
}

type planEntry struct {
	step   string
	status string
}

func extractPlanSnapshot(payload map[string]any) (string, bool, bool) {
	entries, explanation := extractPlanEntries(payload)
	if len(entries) == 0 {
		return "", false, false
	}
	text, done := formatPlanSnapshot(entries, explanation)
	if strings.TrimSpace(text) == "" {
		return "", false, false
	}
	return text, done, true
}

func extractPlanEntries(payload map[string]any) ([]planEntry, string) {
	if payload == nil {
		return nil, ""
	}
	entries := parsePlanEntriesAny(payload["plan"])
	explanation := extractPlanExplanation(payload["explanation"])
	if len(entries) > 0 {
		return entries, explanation
	}
	for _, key := range []string{"msg", "data", "payload"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		entries = parsePlanEntriesAny(nested["plan"])
		if explanation == "" {
			explanation = extractPlanExplanation(nested["explanation"])
		}
		if len(entries) > 0 {
			return entries, explanation
		}
	}
	return nil, explanation
}

func parsePlanEntriesAny(raw any) []planEntry {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]planEntry, 0, len(items))
	for _, item := range items {
		entryMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		step := strings.TrimSpace(extractFirstString(entryMap, "step", "title", "text", "content"))
		if step == "" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(extractFirstString(entryMap, "status", "state")))
		if status == "" {
			status = "pending"
		}
		out = append(out, planEntry{step: step, status: status})
	}
	return out
}

func extractPlanExplanation(raw any) string {
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func formatPlanSnapshot(entries []planEntry, explanation string) (string, bool) {
	total := len(entries)
	if total == 0 {
		return "", false
	}
	completed := 0
	lines := make([]string, 0, total+2)
	for index, entry := range entries {
		if planStatusDone(entry.status) {
			completed += 1
		}
		lines = append(lines, fmt.Sprintf("%d. %s %s", index+1, planStatusSymbol(entry.status), entry.step))
	}
	header := fmt.Sprintf("✓ 已完成 %d/%d 项任务", completed, total)
	if explanation != "" {
		lines = append([]string{header, explanation}, lines...)
	} else {
		lines = append([]string{header}, lines...)
	}
	return strings.Join(lines, "\n"), completed == total
}

func planStatusSymbol(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "success", "done", "finished":
		return "☑"
	case "in_progress", "running", "doing", "active":
		return "◐"
	case "failed", "error", "blocked":
		return "⚠"
	default:
		return "○"
	}
}

func planStatusDone(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "success", "done", "finished":
		return true
	default:
		return false
	}
}

func (m *RuntimeManager) completeTurnLocked(threadID string, ts time.Time) {
	m.finishThinkingLocked(threadID)
	m.finishAssistantLocked(threadID)

	rt := m.runtime[threadID]
	if rt.commandIndex >= 0 {
		zero := 0
		m.finishCommandLocked(threadID, &zero)
	}
	if rt.planIndex >= 0 {
		index := rt.planIndex
		m.patchTimelineItemLocked(threadID, index, func(item *TimelineItem) {
			item.Done = true
		})
		rt.planIndex = -1
	}
	m.flushEditingFilesAsSavedLocked(threadID, ts)
}

func normalizeThreadState(state string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	if s == "" {
		return "idle"
	}
	switch s {
	case "booting", "starting_up":
		return "starting"
	case "streaming":
		return "responding"
	case "executing", "working", "in_progress":
		return "running"
	case "awaiting_approval", "pending":
		return "waiting"
	case "completed", "success", "done", "resumed", "ready", "stopped":
		return "idle"
	case "failed":
		return "error"
	}
	switch s {
	case "idle", "starting", "thinking", "responding", "running", "editing", "waiting", "syncing", "error":
		return s
	default:
		if strings.Contains(s, "error") || strings.Contains(s, "fail") {
			return "error"
		}
		return "idle"
	}
}

func isInterruptibleThreadState(state string) bool {
	switch normalizeThreadState(state) {
	case "starting", "thinking", "responding", "running", "editing", "waiting", "syncing":
		return true
	default:
		return false
	}
}

func extractFirstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			return text
		}
	}
	return ""
}

func extractNestedFirstString(payload map[string]any, paths ...[]string) string {
	for _, path := range paths {
		if len(path) == 0 {
			continue
		}
		current := any(payload)
		matched := true
		for _, key := range path {
			nextMap, ok := current.(map[string]any)
			if !ok {
				matched = false
				break
			}
			next, ok := nextMap[key]
			if !ok {
				matched = false
				break
			}
			current = next
		}
		if !matched {
			continue
		}
		if text, ok := current.(string); ok {
			trimmed := strings.TrimSpace(text)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func extractFirstInt(payload map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if number, ok := extractExitCode(value); ok {
			return number, true
		}
		if text, ok := value.(string); ok {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			if number, err := json.Number(text).Int64(); err == nil {
				return int(number), true
			}
		}
	}
	return 0, false
}

func extractIntValue(value any) (int, bool) {
	if number, ok := extractExitCode(value); ok {
		return number, true
	}
	text, ok := value.(string)
	if !ok {
		return 0, false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	if number, err := json.Number(text).Int64(); err == nil {
		return int(number), true
	}
	return 0, false
}

func extractNestedValue(payload map[string]any, path ...string) (any, bool) {
	if payload == nil || len(path) == 0 {
		return nil, false
	}
	current := any(payload)
	for _, key := range path {
		nextMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := nextMap[key]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func extractFirstIntByPaths(payload map[string]any, paths ...[]string) (int, bool) {
	for _, path := range paths {
		if len(path) == 0 {
			continue
		}
		value, ok := extractNestedValue(payload, path...)
		if !ok {
			continue
		}
		if number, ok := extractIntValue(value); ok {
			return number, true
		}
	}
	return 0, false
}

func extractFirstIntDeep(payload map[string]any, keys ...string) (int, bool) {
	if payload == nil {
		return 0, false
	}
	if number, ok := extractFirstInt(payload, keys...); ok {
		return number, true
	}
	for _, key := range []string{"msg", "data", "payload"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if number, ok := extractFirstInt(nested, keys...); ok {
			return number, true
		}
	}
	return 0, false
}

func (m *RuntimeManager) updateTokenUsageLocked(threadID string, payload map[string]any, ts time.Time) {
	prev := m.snapshot.TokenUsageByThread[threadID]
	next := prev
	usedFromInfoTotal := false

	if limit, ok := extractFirstIntByPaths(payload,
		[]string{"tokenUsage", "modelContextWindow"},
		[]string{"usage", "modelContextWindow"},
		[]string{"info", "model_context_window"},
		[]string{"info", "modelContextWindow"},
	); ok && limit > 0 {
		next.ContextWindowTokens = limit
	} else if limit, ok := extractFirstIntDeep(payload, "context_window_tokens", "contextWindowTokens", "context_window", "model_context_window", "modelContextWindow", "max_input_tokens", "maxTokens", "limit_tokens", "token_limit"); ok && limit > 0 {
		next.ContextWindowTokens = limit
	}
	limit := next.ContextWindowTokens

	if total, ok := extractFirstIntByPaths(payload,
		[]string{"tokenUsage", "total", "totalTokens"},
		[]string{"tokenUsage", "total", "total_tokens"},
		[]string{"usage", "total", "totalTokens"},
		[]string{"usage", "total", "total_tokens"},
	); ok {
		next.UsedTokens = max(0, total)
	} else if total, ok := extractFirstIntByPaths(payload,
		[]string{"info", "last_token_usage", "total_tokens"},
		[]string{"info", "lastTokenUsage", "totalTokens"},
	); ok {
		next.UsedTokens = max(0, total)
	} else if total, ok := extractFirstIntByPaths(payload,
		[]string{"info", "total_token_usage", "total_tokens"},
		[]string{"info", "totalTokenUsage", "totalTokens"},
	); ok {
		next.UsedTokens = max(0, total)
		usedFromInfoTotal = true
		if limit > 0 && next.UsedTokens > limit {
			fallbackUsed := prev.UsedTokens
			if fallbackUsed < 0 || fallbackUsed > limit {
				fallbackUsed = 0
			}
			if prev.UsedTokens > 0 && prev.UsedTokens <= limit {
				next.UsedTokens = prev.UsedTokens
			} else if lastTotal, ok := extractFirstIntByPaths(payload,
				[]string{"info", "last_token_usage", "total_tokens"},
				[]string{"info", "lastTokenUsage", "totalTokens"},
			); ok && lastTotal > 0 && lastTotal <= limit {
				next.UsedTokens = lastTotal
			} else {
				next.UsedTokens = fallbackUsed
			}
		}
	} else if total, ok := extractFirstIntDeep(payload, "total_tokens", "totalTokens", "used_tokens", "usedTokens"); ok {
		next.UsedTokens = max(0, total)
	} else {
		input, hasInput := extractFirstIntByPaths(payload,
			[]string{"tokenUsage", "total", "inputTokens"},
			[]string{"tokenUsage", "total", "input_tokens"},
			[]string{"usage", "total", "inputTokens"},
			[]string{"usage", "total", "input_tokens"},
		)
		if !hasInput {
			input, hasInput = extractFirstIntByPaths(payload,
				[]string{"info", "last_token_usage", "input_tokens"},
				[]string{"info", "lastTokenUsage", "inputTokens"},
			)
		}
		if !hasInput {
			input, hasInput = extractFirstIntByPaths(payload,
				[]string{"info", "total_token_usage", "input_tokens"},
				[]string{"info", "totalTokenUsage", "inputTokens"},
			)
			if hasInput {
				usedFromInfoTotal = true
			}
		}
		if !hasInput {
			input, hasInput = extractFirstIntDeep(payload, "input", "input_tokens", "inputTokens", "prompt_tokens")
		}
		output, hasOutput := extractFirstIntByPaths(payload,
			[]string{"tokenUsage", "total", "outputTokens"},
			[]string{"tokenUsage", "total", "output_tokens"},
			[]string{"usage", "total", "outputTokens"},
			[]string{"usage", "total", "output_tokens"},
		)
		if !hasOutput {
			output, hasOutput = extractFirstIntByPaths(payload,
				[]string{"info", "last_token_usage", "output_tokens"},
				[]string{"info", "lastTokenUsage", "outputTokens"},
			)
		}
		if !hasOutput {
			output, hasOutput = extractFirstIntByPaths(payload,
				[]string{"info", "total_token_usage", "output_tokens"},
				[]string{"info", "totalTokenUsage", "outputTokens"},
			)
			if hasOutput {
				usedFromInfoTotal = true
			}
		}
		if !hasOutput {
			output, hasOutput = extractFirstIntDeep(payload, "output", "output_tokens", "outputTokens", "completion_tokens")
		}
		if hasInput || hasOutput {
			next.UsedTokens = max(0, input+output)
		}
	}

	if usedFromInfoTotal && limit > 0 && next.UsedTokens > limit {
		fallbackUsed := prev.UsedTokens
		if fallbackUsed < 0 || fallbackUsed > limit {
			fallbackUsed = 0
		}
		if lastTotal, ok := extractFirstIntByPaths(payload,
			[]string{"info", "last_token_usage", "total_tokens"},
			[]string{"info", "lastTokenUsage", "totalTokens"},
		); ok && lastTotal > 0 && lastTotal <= limit {
			next.UsedTokens = lastTotal
		} else {
			next.UsedTokens = fallbackUsed
		}
	}

	if next.ContextWindowTokens > 0 && next.UsedTokens > next.ContextWindowTokens && prev.UsedTokens > 0 && prev.UsedTokens <= next.ContextWindowTokens {
		next.UsedTokens = prev.UsedTokens
	}

	if next.ContextWindowTokens > 0 {
		next.UsedPercent = (float64(next.UsedTokens) / float64(next.ContextWindowTokens)) * 100
		if next.UsedPercent < 0 {
			next.UsedPercent = 0
		}
		if next.UsedPercent > 100 {
			next.UsedPercent = 100
		}
		next.LeftPercent = 100 - next.UsedPercent
	} else {
		next.UsedPercent = 0
		next.LeftPercent = 0
	}

	if ts.IsZero() {
		ts = time.Now()
	}
	next.UpdatedAt = ts.UTC().Format(time.RFC3339)
	m.snapshot.TokenUsageByThread[threadID] = next
}

func normalizeFilesAny(value any) []string {
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		seen := map[string]struct{}{}
		for _, item := range v {
			text := strings.TrimSpace(item)
			if text == "" {
				continue
			}
			if _, ok := seen[text]; ok {
				continue
			}
			seen[text] = struct{}{}
			out = append(out, text)
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		seen := map[string]struct{}{}
		for _, item := range v {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			if _, ok := seen[text]; ok {
				continue
			}
			seen[text] = struct{}{}
			out = append(out, text)
		}
		return out
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		return []string{text}
	default:
		return nil
	}
}

func extractExitCode(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

func extractRunKey(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	for _, key := range []string{"run_key", "runKey"} {
		if value, ok := raw[key].(string); ok {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func cloneSnapshot(src RuntimeSnapshot) RuntimeSnapshot {
	out := RuntimeSnapshot{
		Threads:                 make([]ThreadSnapshot, 0, len(src.Threads)),
		Statuses:                make(map[string]string, len(src.Statuses)),
		InterruptibleByThread:   make(map[string]bool, len(src.Statuses)),
		StatusHeadersByThread:   make(map[string]string, len(src.StatusHeadersByThread)),
		StatusDetailsByThread:   make(map[string]string, len(src.StatusDetailsByThread)),
		TimelinesByThread:       make(map[string][]TimelineItem, len(src.TimelinesByThread)),
		DiffTextByThread:        make(map[string]string, len(src.DiffTextByThread)),
		TokenUsageByThread:      make(map[string]TokenUsageSnapshot, len(src.TokenUsageByThread)),
		WorkspaceRunsByKey:      make(map[string]map[string]any, len(src.WorkspaceRunsByKey)),
		WorkspaceFeatureEnabled: nil,
		WorkspaceLastError:      src.WorkspaceLastError,
		AgentMetaByID:           make(map[string]AgentMeta, len(src.AgentMetaByID)),
	}

	out.Threads = append(out.Threads, src.Threads...)
	for key, value := range src.Statuses {
		out.Statuses[key] = value
		out.InterruptibleByThread[key] = isInterruptibleThreadState(value)
	}
	for _, thread := range out.Threads {
		threadID := strings.TrimSpace(thread.ID)
		if threadID == "" {
			continue
		}
		if _, ok := out.InterruptibleByThread[threadID]; ok {
			continue
		}
		status := normalizeThreadState(out.Statuses[threadID])
		out.InterruptibleByThread[threadID] = isInterruptibleThreadState(status)
	}
	for key, value := range src.StatusHeadersByThread {
		out.StatusHeadersByThread[key] = value
	}
	for key, value := range src.StatusDetailsByThread {
		out.StatusDetailsByThread[key] = value
	}
	for key, list := range src.TimelinesByThread {
		copied := make([]TimelineItem, len(list))
		copy(copied, list)
		for i := range copied {
			if len(copied[i].Attachments) > 0 {
				attachments := make([]TimelineAttachment, len(copied[i].Attachments))
				copy(attachments, copied[i].Attachments)
				copied[i].Attachments = attachments
			}
			if copied[i].ExitCode != nil {
				v := *copied[i].ExitCode
				copied[i].ExitCode = &v
			}
			if copied[i].ElapsedMS != nil {
				v := *copied[i].ElapsedMS
				copied[i].ElapsedMS = &v
			}
		}
		out.TimelinesByThread[key] = copied
	}
	for key, value := range src.DiffTextByThread {
		out.DiffTextByThread[key] = value
	}
	for key, value := range src.TokenUsageByThread {
		out.TokenUsageByThread[key] = value
	}
	for key, value := range src.WorkspaceRunsByKey {
		out.WorkspaceRunsByKey[key] = copyMap(value)
	}
	if src.WorkspaceFeatureEnabled != nil {
		v := *src.WorkspaceFeatureEnabled
		out.WorkspaceFeatureEnabled = &v
	}
	for key, value := range src.AgentMetaByID {
		out.AgentMetaByID[key] = value
	}
	return out
}

// cloneSnapshotLight is like cloneSnapshot but skips timelines and diffs (the heaviest fields).
func cloneSnapshotLight(src RuntimeSnapshot) RuntimeSnapshot {
	out := RuntimeSnapshot{
		Threads:                 make([]ThreadSnapshot, 0, len(src.Threads)),
		Statuses:                make(map[string]string, len(src.Statuses)),
		InterruptibleByThread:   make(map[string]bool, len(src.Statuses)),
		StatusHeadersByThread:   make(map[string]string, len(src.StatusHeadersByThread)),
		StatusDetailsByThread:   make(map[string]string, len(src.StatusDetailsByThread)),
		TimelinesByThread:       map[string][]TimelineItem{}, // 跳过: 由调用者按需获取
		DiffTextByThread:        map[string]string{},         // 跳过: 由调用者按需获取
		TokenUsageByThread:      make(map[string]TokenUsageSnapshot, len(src.TokenUsageByThread)),
		WorkspaceRunsByKey:      make(map[string]map[string]any, len(src.WorkspaceRunsByKey)),
		WorkspaceFeatureEnabled: nil,
		WorkspaceLastError:      src.WorkspaceLastError,
		AgentMetaByID:           make(map[string]AgentMeta, len(src.AgentMetaByID)),
	}

	out.Threads = append(out.Threads, src.Threads...)
	for key, value := range src.Statuses {
		out.Statuses[key] = value
		out.InterruptibleByThread[key] = isInterruptibleThreadState(value)
	}
	for _, thread := range out.Threads {
		threadID := strings.TrimSpace(thread.ID)
		if threadID == "" {
			continue
		}
		if _, ok := out.InterruptibleByThread[threadID]; ok {
			continue
		}
		status := normalizeThreadState(out.Statuses[threadID])
		out.InterruptibleByThread[threadID] = isInterruptibleThreadState(status)
	}
	for key, value := range src.StatusHeadersByThread {
		out.StatusHeadersByThread[key] = value
	}
	for key, value := range src.StatusDetailsByThread {
		out.StatusDetailsByThread[key] = value
	}
	for key, value := range src.TokenUsageByThread {
		out.TokenUsageByThread[key] = value
	}
	for key, value := range src.WorkspaceRunsByKey {
		out.WorkspaceRunsByKey[key] = copyMap(value)
	}
	if src.WorkspaceFeatureEnabled != nil {
		v := *src.WorkspaceFeatureEnabled
		out.WorkspaceFeatureEnabled = &v
	}
	for key, value := range src.AgentMetaByID {
		out.AgentMetaByID[key] = value
	}
	return out
}

func copyMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(value))
	for k, v := range value {
		out[k] = v
	}
	return out
}

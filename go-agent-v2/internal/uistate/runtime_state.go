package uistate

import (
	"encoding/json"
	"fmt"
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

// RuntimeSnapshot is a full UI runtime state snapshot.
type RuntimeSnapshot struct {
	Threads                 []ThreadSnapshot          `json:"threads"`
	Statuses                map[string]string         `json:"statuses"`
	TimelinesByThread       map[string][]TimelineItem `json:"timelinesByThread"`
	DiffTextByThread        map[string]string         `json:"diffTextByThread"`
	WorkspaceRunsByKey      map[string]map[string]any `json:"workspaceRunsByKey"`
	WorkspaceFeatureEnabled *bool                     `json:"workspaceFeatureEnabled"`
	WorkspaceLastError      string                    `json:"workspaceLastError"`
	AgentMetaByID           map[string]AgentMeta      `json:"agentMetaById"`
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
			Threads:            []ThreadSnapshot{},
			Statuses:           map[string]string{},
			TimelinesByThread:  map[string][]TimelineItem{},
			DiffTextByThread:   map[string]string{},
			WorkspaceRunsByKey: map[string]map[string]any{},
			AgentMetaByID:      map[string]AgentMeta{},
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
		thread := ThreadSnapshot{
			ID:    id,
			Name:  name,
			State: state,
		}
		next = append(next, thread)
		m.ensureThreadLocked(id)
		if state != "" {
			m.snapshot.Statuses[id] = state
		}
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

// HydrateHistory rebuilds thread timeline from stored messages.
func (m *RuntimeManager) HydrateHistory(threadID string, records []HistoryRecord) {
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
			if normalized.UIStatus == "" {
				normalized.UIStatus = UIStatusThinking
			}
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
			path := extractFirstString(item, "path")
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
	return "file://" + value
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
	if normalized.UIStatus != "" {
		m.setThreadStateLocked(threadID, string(normalized.UIStatus))
	}
	m.markAgentActiveLocked(threadID, ts)
	fields := resolveEventFields(normalized, payload)
	if handler, ok := runtimeEventHandlers[normalized.UIType]; ok {
		handler(m, threadID, fields, payload, ts)
	}
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
	if _, ok := m.snapshot.TimelinesByThread[threadID]; !ok {
		m.snapshot.TimelinesByThread[threadID] = []TimelineItem{}
	}
	if _, ok := m.snapshot.DiffTextByThread[threadID]; !ok {
		m.snapshot.DiffTextByThread[threadID] = ""
	}
	if _, ok := m.snapshot.Statuses[threadID]; !ok {
		m.snapshot.Statuses[threadID] = "idle"
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
		TimelinesByThread:       make(map[string][]TimelineItem, len(src.TimelinesByThread)),
		DiffTextByThread:        make(map[string]string, len(src.DiffTextByThread)),
		WorkspaceRunsByKey:      make(map[string]map[string]any, len(src.WorkspaceRunsByKey)),
		WorkspaceFeatureEnabled: nil,
		WorkspaceLastError:      src.WorkspaceLastError,
		AgentMetaByID:           make(map[string]AgentMeta, len(src.AgentMetaByID)),
	}

	out.Threads = append(out.Threads, src.Threads...)
	for key, value := range src.Statuses {
		out.Statuses[key] = value
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

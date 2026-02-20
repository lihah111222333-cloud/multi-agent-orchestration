package uistate

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

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
			ActivityStatsByThread: map[string]ActivityStats{},
			AlertsByThread:        map[string][]AlertEntry{},
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
			if path != "" {
				attachments = append(attachments, TimelineAttachment{
					Kind: "file",
					Name: attachmentName(path),
					Path: path,
				})
				continue
			}
			if kind != "filecontent" {
				continue
			}
			content := strings.TrimSpace(extractFirstString(item, "content"))
			if content == "" {
				continue
			}
			name := strings.TrimSpace(extractFirstString(item, "name"))
			if name == "" {
				name = "inline-file"
			}
			attachments = append(attachments, TimelineAttachment{
				Kind: "file",
				Name: name,
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


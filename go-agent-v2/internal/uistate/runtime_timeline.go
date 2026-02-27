// runtime_timeline.go — timeline 操作、token 用量、plan 提取 & 通用工具函数。
package uistate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/util"
)

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
	m.seq++
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
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
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
	if list[index].Kind != "assistant" || index != len(list)-1 {
		rt.assistantIndex = m.pushTimelineItemLocked(threadID, TimelineItem{
			Kind: "assistant",
			Text: "",
		}, ts)
		index = rt.assistantIndex
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
	status := "failed"
	if code == 0 {
		status = "completed"
	}
	m.patchTimelineItemLocked(threadID, index, func(item *TimelineItem) {
		item.Status = status
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
	tool := strings.TrimSpace(util.ExtractFirstString(payload, "tool", "tool_name"))
	if tool == "" {
		return
	}

	file := strings.TrimSpace(util.ExtractFirstString(payload, "file", "file_path"))
	preview := strings.TrimSpace(util.ExtractFirstString(payload, "resultPreview", "preview", "text", "content"))
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
	if lastIndex := len(list) - 1; lastIndex >= 0 {
		last := list[lastIndex]
		if last.Kind == "tool" && last.Tool == tool &&
			((last.File == "" && file != "") ||
				(last.Preview == "" && preview != "") ||
				(last.ElapsedMS == nil && elapsedMS != nil)) {
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

func (m *RuntimeManager) showApprovalLocked(threadID, command string, requestID int64, ts time.Time) {
	command = strings.TrimSpace(command)
	list := m.timelineLocked(threadID)
	lastIndex := len(list) - 1
	matchIndex := -1
	if requestID > 0 {
		for i := lastIndex; i >= 0; i-- {
			item := list[i]
			if item.Kind == "approval" && item.Status == "pending" && item.RequestID == requestID {
				matchIndex = i
				break
			}
		}
	}
	if matchIndex < 0 && lastIndex >= 0 {
		last := list[lastIndex]
		if last.Kind == "approval" && last.Status == "pending" && last.RequestID == 0 {
			matchIndex = lastIndex
		}
	}
	if matchIndex >= 0 {
		matched := list[matchIndex]
		if (requestID > 0 && matched.RequestID == 0) || (command != "" && matched.Command == "") {
			m.patchTimelineItemLocked(threadID, matchIndex, func(item *TimelineItem) {
				if requestID > 0 && item.RequestID == 0 {
					item.RequestID = requestID
				}
				if item.Command == "" && command != "" {
					item.Command = command
				}
			})
		}
		return
	}

	item := TimelineItem{
		Kind:    "approval",
		Command: command,
		Status:  "pending",
	}
	if requestID > 0 {
		item.RequestID = requestID
	}
	m.pushTimelineItemLocked(threadID, item, ts)
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
	if text, done := formatPlanSnapshot(entries, explanation); strings.TrimSpace(text) != "" {
		return text, done, true
	}
	return "", false, false
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
		step := strings.TrimSpace(util.ExtractFirstString(entryMap, "step", "title", "text", "content"))
		if step == "" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(util.ExtractFirstString(entryMap, "status", "state")))
		if status == "" {
			status = "pending"
		}
		out = append(out, planEntry{step: step, status: status})
	}
	return out
}

func extractPlanExplanation(raw any) string {
	text, _ := raw.(string)
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
		status := strings.ToLower(strings.TrimSpace(entry.status))
		if status == "completed" || status == "success" || status == "done" || status == "finished" {
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

func (m *RuntimeManager) completeTurnLocked(threadID string, ts time.Time) {
	m.finishThinkingLocked(threadID)
	m.finishAssistantLocked(threadID)

	rt := m.runtime[threadID]
	if rt.commandIndex >= 0 {
		zero := 0
		m.finishCommandLocked(threadID, &zero)
	}
	if rt.planIndex >= 0 {
		m.patchTimelineItemLocked(threadID, rt.planIndex, func(item *TimelineItem) {
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
	s := normalizeThreadState(state)
	return s == "starting" || s == "thinking" || s == "responding" || s == "running" || s == "editing" || s == "waiting" || s == "syncing"
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

func normalizeFilesAny(value any) []string {
	if v, ok := value.(string); ok {
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		return []string{text}
	}
	items := util.AsStringSlice(value)
	if items == nil {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, text := range items {
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	return out
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
	if value := strings.TrimSpace(util.ExtractFirstString(raw, "run_key")); value != "" {
		return value
	}
	if value := strings.TrimSpace(util.ExtractFirstString(raw, "runKey")); value != "" {
		return value
	}
	return ""
}

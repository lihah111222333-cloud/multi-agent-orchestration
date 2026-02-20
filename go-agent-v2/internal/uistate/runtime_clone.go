// runtime_clone.go — RuntimeSnapshot 深拷贝工具函数。
package uistate

import "strings"

func cloneSnapshot(src RuntimeSnapshot) RuntimeSnapshot {
	return cloneBaseSnapshot(src, true)
}

// cloneSnapshotLight is like cloneSnapshot but skips timelines and diffs (the heaviest fields).
func cloneSnapshotLight(src RuntimeSnapshot) RuntimeSnapshot {
	return cloneBaseSnapshot(src, false)
}

// cloneBaseSnapshot creates a deep copy of RuntimeSnapshot. When includeTimeline is false,
// Timeline and DiffText maps are left empty (the caller uses separate accessors).
func cloneBaseSnapshot(src RuntimeSnapshot, includeTimeline bool) RuntimeSnapshot {
	out := RuntimeSnapshot{
		Threads:               make([]ThreadSnapshot, 0, len(src.Threads)),
		Statuses:              make(map[string]string, len(src.Statuses)),
		InterruptibleByThread: make(map[string]bool, len(src.Statuses)),
		StatusHeadersByThread: make(map[string]string, len(src.StatusHeadersByThread)),
		StatusDetailsByThread: make(map[string]string, len(src.StatusDetailsByThread)),
		TokenUsageByThread:    make(map[string]TokenUsageSnapshot, len(src.TokenUsageByThread)),
		WorkspaceRunsByKey:    make(map[string]map[string]any, len(src.WorkspaceRunsByKey)),
		WorkspaceLastError:    src.WorkspaceLastError,
		AgentMetaByID:         make(map[string]AgentMeta, len(src.AgentMetaByID)),
	}

	if includeTimeline {
		out.TimelinesByThread = make(map[string][]TimelineItem, len(src.TimelinesByThread))
		out.DiffTextByThread = make(map[string]string, len(src.DiffTextByThread))
	} else {
		out.TimelinesByThread = map[string][]TimelineItem{}
		out.DiffTextByThread = map[string]string{}
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

	if includeTimeline {
		cloneTimelineItems(src.TimelinesByThread, out.TimelinesByThread)
		for key, value := range src.DiffTextByThread {
			out.DiffTextByThread[key] = value
		}
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

	out.ActivityStatsByThread = cloneActivityStatsMap(src.ActivityStatsByThread)
	out.AlertsByThread = cloneAlerts(src.AlertsByThread)

	return out
}

// cloneTimelineItems deep-copies timeline items including pointer fields.
func cloneTimelineItems(src, dst map[string][]TimelineItem) {
	for key, list := range src {
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
		dst[key] = copied
	}
}

// cloneActivityStatsMap deep-copies activity stats including ToolCalls map.
func cloneActivityStatsMap(src map[string]ActivityStats) map[string]ActivityStats {
	out := make(map[string]ActivityStats, len(src))
	for key, value := range src {
		cloned := ActivityStats{
			LSPCalls:  value.LSPCalls,
			Commands:  value.Commands,
			FileEdits: value.FileEdits,
			ToolCalls: make(map[string]int64, len(value.ToolCalls)),
		}
		for k, v := range value.ToolCalls {
			cloned.ToolCalls[k] = v
		}
		out[key] = cloned
	}
	return out
}

// cloneAlerts deep-copies alert entries per thread.
func cloneAlerts(src map[string][]AlertEntry) map[string][]AlertEntry {
	out := make(map[string][]AlertEntry, len(src))
	for key, value := range src {
		if len(value) == 0 {
			continue
		}
		entries := make([]AlertEntry, len(value))
		copy(entries, value)
		out[key] = entries
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

// thread_list_helpers.go — 线程列表去重 + 快照构建工具 (DRY: E2 + E3)。
//
// 消除 3 个 appendXxxThreads + 2 个 buildThreadSnapshots 的重复逻辑。
package codexadapter

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func normalizeThreadListItem(id, name string) (string, string, bool) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return "", "", false
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		trimmedName = trimmedID
	}
	return trimmedID, trimmedName, true
}

func appendThreadListItem(threads []ThreadListItem, seen map[string]struct{}, id, name, state string) []ThreadListItem {
	trimmedID, trimmedName, ok := normalizeThreadListItem(id, name)
	if !ok {
		return threads
	}
	if _, exists := seen[trimmedID]; exists {
		return threads
	}
	seen[trimmedID] = struct{}{}
	return append(threads, ThreadListItem{
		ID:    trimmedID,
		Name:  trimmedName,
		State: state,
	})
}

func appendThreadSnapshot(snapshots []uistate.ThreadSnapshot, id, name, state string) []uistate.ThreadSnapshot {
	trimmedID, trimmedName, ok := normalizeThreadListItem(id, name)
	if !ok {
		return snapshots
	}
	return append(snapshots, uistate.ThreadSnapshot{
		ID:    trimmedID,
		Name:  trimmedName,
		State: state,
	})
}

func appendBindingThreadItems(threads []ThreadListItem, seen map[string]struct{}, items []store.AgentCodexBinding) []ThreadListItem {
	for _, item := range items {
		threads = appendThreadListItem(threads, seen, item.AgentID, item.AgentID, "idle")
	}
	return threads
}

func appendAgentStatusThreadItems(threads []ThreadListItem, seen map[string]struct{}, items []store.AgentStatus) []ThreadListItem {
	for _, item := range items {
		threads = appendThreadListItem(threads, seen, item.AgentID, item.AgentName, "idle")
	}
	return threads
}

func appendRunnerThreadItems(threads []ThreadListItem, seen map[string]struct{}, items []runner.AgentInfo) []ThreadListItem {
	for _, item := range items {
		threads = appendThreadListItem(threads, seen, item.ID, item.Name, string(item.State))
	}
	return threads
}

func toRunnerThreadSnapshots(items []runner.AgentInfo) []uistate.ThreadSnapshot {
	snapshots := make([]uistate.ThreadSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = appendThreadSnapshot(snapshots, item.ID, item.Name, string(item.State))
	}
	return snapshots
}

func toListItemSnapshots(items []ThreadListItem) []uistate.ThreadSnapshot {
	snapshots := make([]uistate.ThreadSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = appendThreadSnapshot(snapshots, item.ID, item.Name, item.State)
	}
	return snapshots
}

// appendThreadItems 消除 appendBindingThreads/appendAgentStatusThreads/appendRunnerThreads 重复。
func appendThreadItems[T any](threads []ThreadListItem, seen map[string]struct{}, items []T) []ThreadListItem {
	switch src := any(items).(type) {
	case []store.AgentCodexBinding:
		return appendBindingThreadItems(threads, seen, src)
	case []store.AgentStatus:
		return appendAgentStatusThreadItems(threads, seen, src)
	case []runner.AgentInfo:
		return appendRunnerThreadItems(threads, seen, src)
	default:
		return threads
	}
}

// toThreadSnapshots 消除 buildThreadSnapshots/buildThreadSnapshotsFromListItems 重复。
func toThreadSnapshots[T any](items []T) []uistate.ThreadSnapshot {
	switch src := any(items).(type) {
	case []runner.AgentInfo:
		return toRunnerThreadSnapshots(src)
	case []ThreadListItem:
		return toListItemSnapshots(src)
	default:
		return nil
	}
}

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

// threadItemExtractor 将索引 i 处的元素转为 (id, name, state)。
type threadItemExtractor func(i int) (id, name, state string)

// appendThreadItemsFunc 泛型去重合并: trim → dedup → append。
func appendThreadItemsFunc(
	threads []ThreadListItem, seen map[string]struct{},
	count int, extract threadItemExtractor,
) []ThreadListItem {
	for i := 0; i < count; i++ {
		id, name, state := extract(i)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if strings.TrimSpace(name) == "" {
			name = id
		}
		threads = append(threads, ThreadListItem{ID: id, Name: strings.TrimSpace(name), State: state})
		seen[id] = struct{}{}
	}
	return threads
}

// toThreadSnapshotsFunc 泛型快照构建。
func toThreadSnapshotsFunc(count int, extract threadItemExtractor) []uistate.ThreadSnapshot {
	snapshots := make([]uistate.ThreadSnapshot, 0, count)
	for i := 0; i < count; i++ {
		id, name, state := extract(i)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if strings.TrimSpace(name) == "" {
			name = id
		}
		snapshots = append(snapshots, uistate.ThreadSnapshot{
			ID:    id,
			Name:  strings.TrimSpace(name),
			State: state,
		})
	}
	return snapshots
}

// ---- 类型适配便利函数 ----

// appendThreadItems 消除 appendBindingThreads/appendAgentStatusThreads/appendRunnerThreads 重复。
func appendThreadItems[T any](threads []ThreadListItem, seen map[string]struct{}, items []T) []ThreadListItem {
	var extract threadItemExtractor
	// Type switch on first element's type (via intermediate any conversion).
	switch any(items).(type) {
	case []store.AgentCodexBinding:
		src := any(items).([]store.AgentCodexBinding)
		extract = func(i int) (string, string, string) {
			return src[i].AgentID, src[i].AgentID, "idle"
		}
	case []store.AgentStatus:
		src := any(items).([]store.AgentStatus)
		extract = func(i int) (string, string, string) {
			return src[i].AgentID, src[i].AgentName, "idle"
		}
	case []runner.AgentInfo:
		src := any(items).([]runner.AgentInfo)
		extract = func(i int) (string, string, string) {
			return src[i].ID, src[i].Name, string(src[i].State)
		}
	default:
		return threads
	}
	return appendThreadItemsFunc(threads, seen, len(items), extract)
}

// toThreadSnapshots 消除 buildThreadSnapshots/buildThreadSnapshotsFromListItems 重复。
func toThreadSnapshots[T any](items []T) []uistate.ThreadSnapshot {
	var extract threadItemExtractor
	switch any(items).(type) {
	case []runner.AgentInfo:
		src := any(items).([]runner.AgentInfo)
		extract = func(i int) (string, string, string) {
			return src[i].ID, src[i].Name, string(src[i].State)
		}
	case []ThreadListItem:
		src := any(items).([]ThreadListItem)
		extract = func(i int) (string, string, string) {
			return src[i].ID, src[i].Name, src[i].State
		}
	default:
		return nil
	}
	return toThreadSnapshotsFunc(len(items), extract)
}

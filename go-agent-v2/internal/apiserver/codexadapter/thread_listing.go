package codexadapter

import (
	"context"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"sort"
	"strings"
	"time"
)

const prefThreadAliases = "threads.aliases"

// threadListItem models thread list entry payload.
type threadListItem = contracts.ThreadListItem

// ThreadList returns thread/list payload and syncs runtime snapshots.
func (a *Adapter) ThreadList(ctx context.Context) ([]threadListItem, error) {
	return a.threadList(ctx, "thread/list", true)
}

// ThreadLoadedList returns paginated thread IDs for sessions currently loaded in memory.
func (a *Adapter) ThreadLoadedList(_ context.Context, cursor *string, limit *uint32) ([]string, *string, error) {
	ids := loadedThreadIDsFromAgents(a.runningAgents())
	data, nextCursor := paginateLoadedThreadIDs(ids, cursor, limit)
	return data, nextCursor, nil
}

func (a *Adapter) threadList(ctx context.Context, methodName string, syncRuntime bool) ([]threadListItem, error) {
	agents := a.runningAgents()
	threads := make([]threadListItem, 0, len(agents)+32)
	seen := make(map[string]struct{}, len(agents)+32)
	for _, item := range agents {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		threads = append(threads, threadListItem{
			ID:    id,
			Name:  name,
			State: string(item.State),
		})
		seen[id] = struct{}{}
	}

	threads = a.appendThreadHistoryFromStores(ctx, threads, seen, methodName)
	applyThreadAliases(threads, a.loadThreadAliases(ctx))
	if syncRuntime {
		if runtime := a.uiRuntime(); runtime != nil {
			runtime.ReplaceThreads(toThreadSnapshots(threads))
		}
	}
	return threads, nil
}

func (a *Adapter) runningAgents() []runner.AgentInfo {
	manager := a.manager()
	if manager == nil {
		return nil
	}
	return manager.List()
}

func loadedThreadIDsFromAgents(agents []runner.AgentInfo) []string {
	if len(agents) == 0 {
		return []string{}
	}
	ids := make([]string, 0, len(agents))
	seen := make(map[string]struct{}, len(agents))
	for _, item := range agents {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (a *Adapter) appendThreadHistoryFromStores(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
) []threadListItem {
	idMethod := strings.TrimSpace(methodName)
	if idMethod == "" {
		idMethod = "thread/list"
	}
	if a == nil {
		return threads
	}
	threads = a.appendHistoryFromBindingStore(ctx, threads, seen, idMethod)
	threads = a.appendHistoryFromStatusStore(ctx, threads, seen, idMethod)
	threads = a.appendHistoryFromArchiveState(ctx, threads, seen, idMethod)
	return threads
}

func (a *Adapter) appendHistoryFromBindingStore(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
) []threadListItem {
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	bindings, err := bindingStore.ListAll(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from agent_codex_binding failed", logger.FieldError, err)
		return threads
	}
	return appendThreadItems(threads, seen, bindings)
}

func (a *Adapter) appendHistoryFromStatusStore(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
) []threadListItem {
	statusStore := a.statusStore()
	if statusStore == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	items, err := statusStore.List(dbCtx, "")
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from agent_status failed", logger.FieldError, err)
		return threads
	}
	return appendThreadItems(threads, seen, items)
}

func (a *Adapter) appendHistoryFromArchiveState(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
) []threadListItem {
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	archivedMap, err := a.loadThreadArchiveMap(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from threadArchives.chat failed", logger.FieldError, err)
		return threads
	}
	return appendArchivedThreads(threads, seen, archivedMap)
}

func appendThreadSnapshot(snapshots []uistate.ThreadSnapshot, id, name, state string) []uistate.ThreadSnapshot {
	trimmedID, trimmedName, ok := normalizethreadListItem(id, name)
	if !ok {
		return snapshots
	}
	return append(snapshots, uistate.ThreadSnapshot{
		ID:    trimmedID,
		Name:  trimmedName,
		State: state,
	})
}

func appendBindingThreadItems(threads []threadListItem, seen map[string]struct{}, items []store.AgentCodexBinding) []threadListItem {
	for _, item := range items {
		threads = appendthreadListItem(threads, seen, item.AgentID, item.AgentID, "idle")
	}
	return threads
}

func appendAgentStatusThreadItems(threads []threadListItem, seen map[string]struct{}, items []store.AgentStatus) []threadListItem {
	for _, item := range items {
		threads = appendthreadListItem(threads, seen, item.AgentID, item.AgentName, "idle")
	}
	return threads
}

func appendRunnerThreadItems(threads []threadListItem, seen map[string]struct{}, items []runner.AgentInfo) []threadListItem {
	for _, item := range items {
		threads = appendthreadListItem(threads, seen, item.ID, item.Name, string(item.State))
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

func toListItemSnapshots(items []threadListItem) []uistate.ThreadSnapshot {
	snapshots := make([]uistate.ThreadSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = appendThreadSnapshot(snapshots, item.ID, item.Name, item.State)
	}
	return snapshots
}

func appendThreadItems[T any](threads []threadListItem, seen map[string]struct{}, items []T) []threadListItem {
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

func toThreadSnapshots[T any](items []T) []uistate.ThreadSnapshot {
	switch src := any(items).(type) {
	case []runner.AgentInfo:
		return toRunnerThreadSnapshots(src)
	case []threadListItem:
		return toListItemSnapshots(src)
	default:
		return nil
	}
}

func (a *Adapter) loadThreadAliases(ctx context.Context) map[string]string {
	store := a.store()
	if store == nil {
		return map[string]string{}
	}
	value, err := store.Get(ctx, prefThreadAliases)
	if err != nil {
		logger.Warn("thread aliases: load preference failed", logger.FieldError, err)
		return map[string]string{}
	}
	return normalizeThreadAliases(value)
}

func (a *Adapter) persistThreadAlias(ctx context.Context, threadID, alias string) error {
	store := a.store()
	if store == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	value, err := store.Get(ctx, prefThreadAliases)
	if err != nil {
		return err
	}
	aliases := normalizeThreadAliases(value)
	nextAlias := strings.TrimSpace(alias)
	if nextAlias == "" || nextAlias == id {
		delete(aliases, id)
	} else {
		aliases[id] = nextAlias
	}
	return store.Set(ctx, prefThreadAliases, aliases)
}

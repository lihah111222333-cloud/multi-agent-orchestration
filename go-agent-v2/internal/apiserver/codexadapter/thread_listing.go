package codexadapter

import (
	"context"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	listingsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/listing"
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
	ids := listingsvc.LoadedThreadIDsFromAgents(toListingAgentInfos(a.runningAgents()))
	data, nextCursor := listingsvc.PaginateLoadedThreadIDs(ids, cursor, limit)
	return data, nextCursor, nil
}

func (a *Adapter) threadList(ctx context.Context, methodName string, syncRuntime bool) ([]threadListItem, error) {
	items, err := listingsvc.BuildThreadList(ctx, methodName, syncRuntime, a.listingAgentInfos, a.appendThreadHistoryFromStoresService, a.loadThreadAliases, a.syncThreadListRuntimeService)
	if err != nil {
		return nil, err
	}
	return toThreadListItemsFromService(items), nil
}

func (a *Adapter) listingAgentInfos() []listingsvc.AgentInfo {
	return toListingAgentInfos(a.runningAgents())
}

func (a *Adapter) appendThreadHistoryFromStoresService(ctx context.Context, threads []listingsvc.ThreadListItem, seen map[string]struct{}, methodName string) []listingsvc.ThreadListItem {
	return toServiceThreadListItems(a.appendThreadHistoryFromStores(ctx, toThreadListItemsFromService(threads), seen, methodName))
}

func (a *Adapter) syncThreadListRuntimeService(threads []listingsvc.ThreadListItem) {
	a.syncThreadListRuntime(toThreadListItemsFromService(threads))
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

func (a *Adapter) appendThreadHistoryFromStores(ctx context.Context, threads []threadListItem, seen map[string]struct{}, methodName string) []threadListItem {
	serviceItems := listingsvc.AppendThreadHistoryFromStores(ctx, toServiceThreadListItems(threads), seen, methodName, a.appendHistoryFromBindingStoreService, a.appendHistoryFromStatusStoreService, a.appendHistoryFromArchiveStateService)
	return toThreadListItemsFromService(serviceItems)
}

func (a *Adapter) appendHistoryFromBindingStoreService(ctx context.Context, threads []listingsvc.ThreadListItem, seen map[string]struct{}, methodName string) []listingsvc.ThreadListItem {
	return toServiceThreadListItems(a.appendHistoryFromBindingStore(ctx, toThreadListItemsFromService(threads), seen, methodName))
}

func (a *Adapter) appendHistoryFromStatusStoreService(ctx context.Context, threads []listingsvc.ThreadListItem, seen map[string]struct{}, methodName string) []listingsvc.ThreadListItem {
	return toServiceThreadListItems(a.appendHistoryFromStatusStore(ctx, toThreadListItemsFromService(threads), seen, methodName))
}

func (a *Adapter) appendHistoryFromArchiveStateService(ctx context.Context, threads []listingsvc.ThreadListItem, seen map[string]struct{}, methodName string) []listingsvc.ThreadListItem {
	return toServiceThreadListItems(a.appendHistoryFromArchiveState(ctx, toThreadListItemsFromService(threads), seen, methodName))
}

func (a *Adapter) appendHistoryFromBindingStore(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
) []threadListItem {
	return toThreadListItemsFromService(
		listingsvc.AppendHistoryFromBindingStore(
			ctx,
			toServiceThreadListItems(threads),
			seen,
			methodName,
			func(ctx context.Context) ([]listingsvc.AgentCodexBinding, error) {
				bindingStore := a.bindingStore()
				if bindingStore == nil {
					return nil, nil
				}
				items, err := bindingStore.ListAll(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]listingsvc.AgentCodexBinding, 0, len(items))
				for _, item := range items {
					out = append(out, listingsvc.AgentCodexBinding{AgentID: item.AgentID})
				}
				return out, nil
			},
		),
	)
}

func (a *Adapter) appendHistoryFromStatusStore(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
) []threadListItem {
	return toThreadListItemsFromService(
		listingsvc.AppendHistoryFromStatusStore(
			ctx,
			toServiceThreadListItems(threads),
			seen,
			methodName,
			func(ctx context.Context) ([]listingsvc.AgentStatus, error) {
				statusStore := a.statusStore()
				if statusStore == nil {
					return nil, nil
				}
				items, err := statusStore.List(ctx, "")
				if err != nil {
					return nil, err
				}
				out := make([]listingsvc.AgentStatus, 0, len(items))
				for _, item := range items {
					out = append(out, listingsvc.AgentStatus{AgentID: item.AgentID, AgentName: item.AgentName})
				}
				return out, nil
			},
		),
	)
}

func (a *Adapter) appendHistoryFromArchiveState(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
) []threadListItem {
	return toThreadListItemsFromService(
		listingsvc.AppendHistoryFromArchiveState(ctx, toServiceThreadListItems(threads), seen, methodName, a.loadThreadArchiveMap),
	)
}

func (a *Adapter) syncThreadListRuntime(threads []threadListItem) {
	runtime := a.uiRuntime()
	if runtime != nil {
		runtime.ReplaceThreads(toThreadSnapshots(threads))
	}
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

func normalizethreadListItem(id, name string) (string, string, bool) {
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

func appendthreadListItem(threads []threadListItem, seen map[string]struct{}, id, name, state string) []threadListItem {
	trimmedID, trimmedName, ok := normalizethreadListItem(id, name)
	if !ok {
		return threads
	}
	if _, exists := seen[trimmedID]; exists {
		return threads
	}
	seen[trimmedID] = struct{}{}
	return append(threads, threadListItem{ID: trimmedID, Name: trimmedName, State: state})
}

func (a *Adapter) loadThreadAliases(ctx context.Context) map[string]string {
	store := a.store()
	if store == nil {
		return map[string]string{}
	}
	return listingsvc.LoadThreadAliases(ctx, store.Get)
}

func (a *Adapter) persistThreadAlias(ctx context.Context, threadID, alias string) error {
	store := a.store()
	if store == nil {
		return nil
	}
	return listingsvc.PersistThreadAlias(ctx, threadID, alias, store.Get, store.Set)
}

func toListingAgentInfos(items []runner.AgentInfo) []listingsvc.AgentInfo {
	if len(items) == 0 {
		return nil
	}
	out := make([]listingsvc.AgentInfo, 0, len(items))
	for _, item := range items {
		out = append(out, listingsvc.AgentInfo{
			ID:    item.ID,
			Name:  item.Name,
			State: string(item.State),
		})
	}
	return out
}

func toServiceThreadListItems(items []threadListItem) []listingsvc.ThreadListItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]listingsvc.ThreadListItem, 0, len(items))
	for _, item := range items {
		out = append(out, listingsvc.ThreadListItem{
			ID:    item.ID,
			Name:  item.Name,
			State: item.State,
		})
	}
	return out
}

func toThreadListItemsFromService(items []listingsvc.ThreadListItem) []threadListItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]threadListItem, 0, len(items))
	for _, item := range items {
		out = append(out, threadListItem{
			ID:    item.ID,
			Name:  item.Name,
			State: item.State,
		})
	}
	return out
}

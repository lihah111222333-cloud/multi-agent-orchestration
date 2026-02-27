package listing

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	listingsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/listing"
)

const PrefThreadAliases = listingsvc.PrefThreadAliases

type ThreadListItem = listingsvc.ThreadListItem
type AgentInfo = listingsvc.AgentInfo
type AgentCodexBinding = listingsvc.AgentCodexBinding
type AgentStatus = listingsvc.AgentStatus
type ThreadSnapshot = listingsvc.ThreadSnapshot

func mapSlice[S any, D any](in []S, mapFn func(S) D) []D {
	if len(in) == 0 {
		return nil
	}
	out := make([]D, len(in))
	for i, item := range in {
		out[i] = mapFn(item)
	}
	return out
}

type appendStoreFunc[T any] func(
	context.Context,
	[]ThreadListItem,
	map[string]struct{},
	string,
	func(context.Context) ([]T, error),
) []ThreadListItem

func listStoreItems[S any, D any](
	listFn func(context.Context) ([]S, error),
	mapFn func(S) D,
) func(context.Context) ([]D, error) {
	return func(ctx context.Context) ([]D, error) {
		if listFn == nil {
			return nil, nil
		}
		items, err := listFn(ctx)
		if err != nil {
			return nil, err
		}
		return mapSlice(items, mapFn), nil
	}
}

func appendStoreHistory[T any](appendFn appendStoreFunc[T], listFn func(context.Context) ([]T, error)) func(
	context.Context,
	[]ThreadListItem,
	map[string]struct{},
	string,
) []ThreadListItem {
	return func(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string) []ThreadListItem {
		return appendFn(ctx, threads, seen, methodName, listFn)
	}
}

var (
	PaginateLoadedThreadIDs   = listingsvc.PaginateLoadedThreadIDs
	PersistThreadAlias        = listingsvc.PersistThreadAlias
	LoadedThreadIDsFromAgents = listingsvc.LoadedThreadIDsFromAgents
)

func ToAgentInfos(items []codexsdk.AgentInfo) []AgentInfo {
	return mapSlice(items, func(item codexsdk.AgentInfo) AgentInfo {
		return AgentInfo{ID: item.ID, Name: item.Name, State: string(item.State)}
	})
}

func ToThreadListItems(items []ThreadListItem) []contracts.ThreadListItem {
	return items
}

func BuildThreadListFromDeps(
	ctx context.Context,
	runningAgents []AgentInfo,
	bindingStore *store.AgentCodexBindingStore,
	statusStore *store.AgentStatusStore,
	prefStore *uistate.PreferenceManager,
	uiRuntime *uistate.RuntimeManager,
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) ([]ThreadListItem, error) {
	appendBinding := appendStoreHistory(
		listingsvc.AppendHistoryFromBindingStore,
		listStoreItems(
			func(ctx context.Context) ([]store.AgentCodexBinding, error) {
				if bindingStore == nil {
					return nil, nil
				}
				return bindingStore.ListAll(ctx)
			},
			func(item store.AgentCodexBinding) AgentCodexBinding {
				return AgentCodexBinding{AgentID: item.AgentID}
			},
		),
	)
	appendStatus := appendStoreHistory(
		listingsvc.AppendHistoryFromStatusStore,
		listStoreItems(
			func(ctx context.Context) ([]store.AgentStatus, error) {
				if statusStore == nil {
					return nil, nil
				}
				return statusStore.List(ctx, "")
			},
			func(item store.AgentStatus) AgentStatus {
				return AgentStatus{AgentID: item.AgentID, AgentName: item.AgentName}
			},
		),
	)
	appendArchive := func(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string) []ThreadListItem {
		return listingsvc.AppendHistoryFromArchiveState(ctx, threads, seen, methodName, loadThreadArchiveMap)
	}
	loadAliases := func(ctx context.Context) map[string]string {
		if prefStore == nil {
			return map[string]string{}
		}
		return listingsvc.LoadThreadAliases(ctx, prefStore.Get)
	}
	syncRuntimeThreads := func(threads []ThreadListItem) {
		if uiRuntime == nil {
			return
		}
		uiRuntime.ReplaceThreads(mapSlice(threads, func(item ThreadListItem) uistate.ThreadSnapshot {
			return uistate.ThreadSnapshot{ID: item.ID, Name: item.Name, State: item.State}
		}))
	}
	return listingsvc.BuildThreadList(
		ctx,
		"thread/list",
		true,
		func() []AgentInfo { return runningAgents },
		func(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string) []ThreadListItem {
			return listingsvc.AppendThreadHistoryFromStores(ctx, threads, seen, methodName, appendBinding, appendStatus, appendArchive)
		},
		loadAliases,
		syncRuntimeThreads,
	)
}

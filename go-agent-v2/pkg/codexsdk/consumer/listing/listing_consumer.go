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

var (
	PaginateLoadedThreadIDs       = listingsvc.PaginateLoadedThreadIDs
	BuildThreadList               = listingsvc.BuildThreadList
	AppendThreadHistoryFromStores = listingsvc.AppendThreadHistoryFromStores
	AppendHistoryFromBindingStore = listingsvc.AppendHistoryFromBindingStore
	AppendHistoryFromStatusStore  = listingsvc.AppendHistoryFromStatusStore
	AppendHistoryFromArchiveState = listingsvc.AppendHistoryFromArchiveState
	AppendArchivedThreads         = listingsvc.AppendArchivedThreads
	NormalizeThreadListItem       = listingsvc.NormalizeThreadListItem
	AppendThreadListItem          = listingsvc.AppendThreadListItem
	LoadThreadAliases             = listingsvc.LoadThreadAliases
	PersistThreadAlias            = listingsvc.PersistThreadAlias
	LoadedThreadIDsFromAgents     = listingsvc.LoadedThreadIDsFromAgents
)

func AppendThreadItems[T any](threads []ThreadListItem, seen map[string]struct{}, items []T) []ThreadListItem {
	return listingsvc.AppendThreadItems(threads, seen, items)
}

func ToThreadSnapshots[T any](items []T) []ThreadSnapshot {
	return listingsvc.ToThreadSnapshots(items)
}

func ToAgentInfos(items []codexsdk.AgentInfo) []AgentInfo {
	return mapSlice(items, func(item codexsdk.AgentInfo) AgentInfo {
		return AgentInfo{ID: item.ID, Name: item.Name, State: string(item.State)}
	})
}

// ToThreadListItems is now identity — contracts.ThreadListItem == listing.ThreadListItem == agentcore.ThreadListItem.
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
	appendBinding := func(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string) []ThreadListItem {
		return AppendHistoryFromBindingStore(ctx, threads, seen, methodName, func(ctx context.Context) ([]AgentCodexBinding, error) {
			if bindingStore == nil {
				return nil, nil
			}
			items, err := bindingStore.ListAll(ctx)
			if err != nil {
				return nil, err
			}
			return mapSlice(items, func(item store.AgentCodexBinding) AgentCodexBinding {
				return AgentCodexBinding{AgentID: item.AgentID}
			}), nil
		})
	}
	appendStatus := func(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string) []ThreadListItem {
		return AppendHistoryFromStatusStore(ctx, threads, seen, methodName, func(ctx context.Context) ([]AgentStatus, error) {
			if statusStore == nil {
				return nil, nil
			}
			items, err := statusStore.List(ctx, "")
			if err != nil {
				return nil, err
			}
			return mapSlice(items, func(item store.AgentStatus) AgentStatus {
				return AgentStatus{AgentID: item.AgentID, AgentName: item.AgentName}
			}), nil
		})
	}
	appendArchive := func(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string) []ThreadListItem {
		return AppendHistoryFromArchiveState(ctx, threads, seen, methodName, loadThreadArchiveMap)
	}
	loadAliases := func(ctx context.Context) map[string]string {
		if prefStore == nil {
			return map[string]string{}
		}
		return LoadThreadAliases(ctx, prefStore.Get)
	}
	syncRuntimeThreads := func(threads []ThreadListItem) {
		if uiRuntime == nil {
			return
		}
		uiRuntime.ReplaceThreads(mapSlice(threads, func(item ThreadListItem) uistate.ThreadSnapshot {
			return uistate.ThreadSnapshot{ID: item.ID, Name: item.Name, State: item.State}
		}))
	}
	return BuildThreadList(
		ctx,
		"thread/list",
		true,
		func() []AgentInfo { return runningAgents },
		func(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string) []ThreadListItem {
			return AppendThreadHistoryFromStores(ctx, threads, seen, methodName, appendBinding, appendStatus, appendArchive)
		},
		loadAliases,
		syncRuntimeThreads,
	)
}

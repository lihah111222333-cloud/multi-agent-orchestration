package listing

import (
	"context"

	listingsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/listing"
)

const PrefThreadAliases = listingsvc.PrefThreadAliases

type ThreadListItem = listingsvc.ThreadListItem
type AgentInfo = listingsvc.AgentInfo
type AgentCodexBinding = listingsvc.AgentCodexBinding
type AgentStatus = listingsvc.AgentStatus
type ThreadSnapshot = listingsvc.ThreadSnapshot

func PaginateLoadedThreadIDs(ids []string, cursor *string, limit *uint32) ([]string, *string) {
	return listingsvc.PaginateLoadedThreadIDs(ids, cursor, limit)
}

func BuildThreadList(
	ctx context.Context,
	methodName string,
	syncRuntime bool,
	runningAgents func() []AgentInfo,
	appendHistoryFromStores func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem,
	loadThreadAliases func(context.Context) map[string]string,
	syncRuntimeThreads func([]ThreadListItem),
) ([]ThreadListItem, error) {
	return listingsvc.BuildThreadList(
		ctx,
		methodName,
		syncRuntime,
		runningAgents,
		appendHistoryFromStores,
		loadThreadAliases,
		syncRuntimeThreads,
	)
}

func AppendThreadHistoryFromStores(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
	appendHistoryFromBindingStore func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem,
	appendHistoryFromStatusStore func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem,
	appendHistoryFromArchiveState func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem,
) []ThreadListItem {
	return listingsvc.AppendThreadHistoryFromStores(
		ctx,
		threads,
		seen,
		methodName,
		appendHistoryFromBindingStore,
		appendHistoryFromStatusStore,
		appendHistoryFromArchiveState,
	)
}

func AppendHistoryFromBindingStore(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
	listBindings func(context.Context) ([]AgentCodexBinding, error),
) []ThreadListItem {
	return listingsvc.AppendHistoryFromBindingStore(ctx, threads, seen, methodName, listBindings)
}

func AppendHistoryFromStatusStore(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
	listStatus func(context.Context) ([]AgentStatus, error),
) []ThreadListItem {
	return listingsvc.AppendHistoryFromStatusStore(ctx, threads, seen, methodName, listStatus)
}

func AppendHistoryFromArchiveState(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) []ThreadListItem {
	return listingsvc.AppendHistoryFromArchiveState(ctx, threads, seen, methodName, loadThreadArchiveMap)
}

func AppendArchivedThreads(threads []ThreadListItem, seen map[string]struct{}, archived map[string]int64) []ThreadListItem {
	return listingsvc.AppendArchivedThreads(threads, seen, archived)
}

func NormalizeThreadListItem(id, name string) (string, string, bool) {
	return listingsvc.NormalizeThreadListItem(id, name)
}

func AppendThreadListItem(threads []ThreadListItem, seen map[string]struct{}, id, name, state string) []ThreadListItem {
	return listingsvc.AppendThreadListItem(threads, seen, id, name, state)
}

func LoadThreadAliases(ctx context.Context, getPref func(context.Context, string) (any, error)) map[string]string {
	return listingsvc.LoadThreadAliases(ctx, getPref)
}

func PersistThreadAlias(
	ctx context.Context,
	threadID string,
	alias string,
	getPref func(context.Context, string) (any, error),
	setPref func(context.Context, string, any) error,
) error {
	return listingsvc.PersistThreadAlias(ctx, threadID, alias, getPref, setPref)
}

func LoadedThreadIDsFromAgents(agents []AgentInfo) []string {
	return listingsvc.LoadedThreadIDsFromAgents(agents)
}

func AppendThreadItems[T any](threads []ThreadListItem, seen map[string]struct{}, items []T) []ThreadListItem {
	return listingsvc.AppendThreadItems(threads, seen, items)
}

func ToThreadSnapshots[T any](items []T) []ThreadSnapshot {
	return listingsvc.ToThreadSnapshots(items)
}

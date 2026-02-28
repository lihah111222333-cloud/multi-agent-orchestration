package listing

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/history"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const PrefThreadAliases = "threads.aliases"

type ThreadListItem = agentcore.ThreadListItem

type AgentInfo struct{ ID, Name, State string }

type AgentCodexBinding struct{ AgentID string }

type AgentStatus struct{ AgentID, AgentName string }

type ThreadSnapshot struct{ ID, Name, State string }

func PaginateLoadedThreadIDs(ids []string, cursor *string, limit *uint32) ([]string, *string) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	start := 0
	if cursor != nil {
		cursorID := strings.TrimSpace(*cursor)
		if cursorID != "" {
			start = sort.SearchStrings(ids, cursorID)
			if start < len(ids) && ids[start] == cursorID {
				start++
			}
		}
	}
	if start >= len(ids) {
		return []string{}, nil
	}
	pageSize := len(ids)
	if limit != nil {
		pageSize = int(*limit)
		if pageSize < 1 {
			pageSize = 1
		}
	}
	end := start + pageSize
	if end > len(ids) {
		end = len(ids)
	}
	page := append([]string(nil), ids[start:end]...)
	if end >= len(ids) {
		return page, nil
	}
	nextCursor := page[len(page)-1]
	return page, &nextCursor
}

func BuildThreadList(ctx context.Context, methodName string, syncRuntime bool, runningAgents func() []AgentInfo, appendHistoryFromStores func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem, loadThreadAliases func(context.Context) map[string]string, syncRuntimeThreads func([]ThreadListItem)) ([]ThreadListItem, error) {
	agents := []AgentInfo(nil)
	if runningAgents != nil {
		agents = runningAgents()
	}
	threads := make([]ThreadListItem, 0, len(agents)+32)
	seen := make(map[string]struct{}, len(agents)+32)
	threads = AppendThreadItems(threads, seen, agents)
	if appendHistoryFromStores != nil {
		threads = appendHistoryFromStores(ctx, threads, seen, methodName)
	}
	if loadThreadAliases != nil {
		ApplyThreadAliases(threads, loadThreadAliases(ctx))
	}
	if syncRuntime && syncRuntimeThreads != nil {
		syncRuntimeThreads(threads)
	}
	return threads, nil
}

func AppendThreadHistoryFromStores(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string, appendHistoryFromBindingStore func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem, appendHistoryFromStatusStore func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem, appendHistoryFromArchiveState func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem) []ThreadListItem {
	idMethod := strings.TrimSpace(methodName)
	if idMethod == "" {
		idMethod = "thread/list"
	}
	for _, appendStore := range []func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem{appendHistoryFromBindingStore, appendHistoryFromStatusStore, appendHistoryFromArchiveState} {
		if appendStore != nil {
			threads = appendStore(ctx, threads, seen, idMethod)
		}
	}
	return threads
}

func AppendHistoryFromBindingStore(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string, listBindings func(context.Context) ([]AgentCodexBinding, error)) []ThreadListItem {
	return appendHistoryFromStore(ctx, threads, seen, methodName, "agent_codex_binding", 5*time.Second, listBindings)
}

func AppendHistoryFromStatusStore(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string, listStatus func(context.Context) ([]AgentStatus, error)) []ThreadListItem {
	return appendHistoryFromStore(ctx, threads, seen, methodName, "agent_status", 5*time.Second, listStatus)
}

func AppendHistoryFromArchiveState(ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName string, loadThreadArchiveMap func(context.Context) (map[string]int64, error)) []ThreadListItem {
	if loadThreadArchiveMap == nil {
		return threads
	}
	archivedMap, err := loadWithTimeout(ctx, 3*time.Second, loadThreadArchiveMap)
	if err != nil {
		logger.Warn(methodName+": load history threads from threadArchives.chat failed", logger.FieldError, err)
		return threads
	}
	return AppendArchivedThreads(threads, seen, archivedMap)
}

func appendHistoryFromStore[T any](ctx context.Context, threads []ThreadListItem, seen map[string]struct{}, methodName, source string, timeout time.Duration, load func(context.Context) ([]T, error)) []ThreadListItem {
	if load == nil {
		return threads
	}
	items, err := loadWithTimeout(ctx, timeout, load)
	if err != nil {
		logger.Warn(methodName+": load history threads from "+source+" failed", logger.FieldError, err)
		return threads
	}
	return AppendThreadItems(threads, seen, items)
}

func loadWithTimeout[T any](ctx context.Context, timeout time.Duration, load func(context.Context) (T, error)) (T, error) {
	var zero T
	if load == nil {
		return zero, nil
	}
	dbCtx, cancel := context.WithTimeout(history.EnsureContext(ctx), timeout)
	defer cancel()
	return load(dbCtx)
}

func AppendArchivedThreads(threads []ThreadListItem, seen map[string]struct{}, archived map[string]int64) []ThreadListItem {
	type archivedEntry struct {
		ID string
		At int64
	}
	entries := make([]archivedEntry, 0, len(archived))
	for rawID, rawAt := range archived {
		id := strings.TrimSpace(rawID)
		if id == "" || rawAt <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		entries = append(entries, archivedEntry{ID: id, At: rawAt})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].At != entries[j].At {
			return entries[i].At > entries[j].At
		}
		return entries[i].ID < entries[j].ID
	})
	for _, item := range entries {
		threads = append(threads, ThreadListItem{ID: item.ID, Name: item.ID, State: "idle", Archived: true})
		seen[item.ID] = struct{}{}
	}
	return threads
}

func NormalizeThreadListItem(id, name string) (string, string, bool) {
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

func AppendThreadListItem(threads []ThreadListItem, seen map[string]struct{}, id, name, state string) []ThreadListItem {
	trimmedID, trimmedName, ok := NormalizeThreadListItem(id, name)
	if !ok {
		return threads
	}
	if _, exists := seen[trimmedID]; exists {
		return threads
	}
	seen[trimmedID] = struct{}{}
	return append(threads, ThreadListItem{ID: trimmedID, Name: trimmedName, State: state})
}

func NormalizeThreadAliases(value any) map[string]string {
	aliases := map[string]string{}
	switch typed := value.(type) {
	case map[string]string:
		addNormalizedThreadAliases(aliases, typed)
	case map[string]any:
		addNormalizedThreadAliases(aliases, typed)
	case string:
		addNormalizedThreadAliases(aliases, decodeAliasMap([]byte(strings.TrimSpace(typed))))
	case json.RawMessage:
		addNormalizedThreadAliases(aliases, decodeAliasMap(typed))
	}
	return aliases
}

func addNormalizedThreadAliases[T any](aliases map[string]string, src map[string]T) {
	for threadID, alias := range src {
		addNormalizedThreadAlias(aliases, threadID, alias)
	}
}

func decodeAliasMap(raw []byte) map[string]any {
	decoded := map[string]any{}
	if len(raw) == 0 || json.Unmarshal(raw, &decoded) != nil {
		return nil
	}
	return decoded
}

func addNormalizedThreadAlias(aliases map[string]string, threadID string, alias any) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	name := strings.TrimSpace(StringValue(alias))
	if name == "" || name == id {
		return
	}
	aliases[id] = name
}

func ApplyThreadAliases(threads []ThreadListItem, aliases map[string]string) {
	if len(threads) == 0 || len(aliases) == 0 {
		return
	}
	for i := range threads {
		id := strings.TrimSpace(threads[i].ID)
		if id == "" {
			continue
		}
		if alias := strings.TrimSpace(aliases[id]); alias != "" {
			threads[i].Name = alias
		}
	}
}

func LoadThreadAliases(ctx context.Context, getPref func(context.Context, string) (any, error)) map[string]string {
	aliases, err := loadThreadAliases(ctx, getPref)
	if err != nil {
		logger.Warn("thread aliases: load preference failed", logger.FieldError, err)
		return map[string]string{}
	}
	return aliases
}

func loadThreadAliases(ctx context.Context, getPref func(context.Context, string) (any, error)) (map[string]string, error) {
	if getPref == nil {
		return map[string]string{}, nil
	}
	value, err := getPref(ctx, PrefThreadAliases)
	if err != nil {
		return nil, err
	}
	return NormalizeThreadAliases(value), nil
}

func PersistThreadAlias(ctx context.Context, threadID string, alias string, getPref func(context.Context, string) (any, error), setPref func(context.Context, string, any) error) error {
	if getPref == nil || setPref == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	aliases, err := loadThreadAliases(ctx, getPref)
	if err != nil {
		return err
	}
	nextAlias := strings.TrimSpace(alias)
	if nextAlias == "" || nextAlias == id {
		delete(aliases, id)
	} else {
		aliases[id] = nextAlias
	}
	return setPref(ctx, PrefThreadAliases, aliases)
}

func StringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	}
	return ""
}

func LoadedThreadIDsFromAgents(agents []AgentInfo) []string {
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

func AppendThreadItems[T any](threads []ThreadListItem, seen map[string]struct{}, items []T) []ThreadListItem {
	switch src := any(items).(type) {
	case []AgentCodexBinding:
		return appendThreadItems(threads, seen, src, func(item AgentCodexBinding) (string, string, string) { return item.AgentID, item.AgentID, "idle" })
	case []AgentStatus:
		return appendThreadItems(threads, seen, src, func(item AgentStatus) (string, string, string) { return item.AgentID, item.AgentName, "idle" })
	case []AgentInfo:
		return appendThreadItems(threads, seen, src, func(item AgentInfo) (string, string, string) { return item.ID, item.Name, item.State })
	default:
		return threads
	}
}

func appendThreadItems[T any](threads []ThreadListItem, seen map[string]struct{}, items []T, fields func(T) (string, string, string)) []ThreadListItem {
	for _, item := range items {
		id, name, state := fields(item)
		threads = AppendThreadListItem(threads, seen, id, name, state)
	}
	return threads
}

func ToThreadSnapshots[T any](items []T) []ThreadSnapshot {
	switch src := any(items).(type) {
	case []AgentInfo:
		return toThreadSnapshots(src, func(item AgentInfo) (string, string, string) { return item.ID, item.Name, item.State })
	case []ThreadListItem:
		return toThreadSnapshots(src, func(item ThreadListItem) (string, string, string) { return item.ID, item.Name, item.State })
	default:
		return nil
	}
}

func toThreadSnapshots[T any](items []T, fields func(T) (string, string, string)) []ThreadSnapshot {
	snapshots := make([]ThreadSnapshot, 0, len(items))
	for _, item := range items {
		id, name, state := fields(item)
		if id, name, ok := NormalizeThreadListItem(id, name); ok {
			snapshots = append(snapshots, ThreadSnapshot{ID: id, Name: name, State: state})
		}
	}
	return snapshots
}

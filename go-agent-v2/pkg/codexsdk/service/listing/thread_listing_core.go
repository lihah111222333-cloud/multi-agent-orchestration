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

type AgentInfo struct {
	ID    string
	Name  string
	State string
}

type AgentCodexBinding struct {
	AgentID string
}

type AgentStatus struct {
	AgentID   string
	AgentName string
}

type ThreadSnapshot struct {
	ID    string
	Name  string
	State string
}

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

func BuildThreadList(
	ctx context.Context,
	methodName string,
	syncRuntime bool,
	runningAgents func() []AgentInfo,
	appendHistoryFromStores func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem,
	loadThreadAliases func(context.Context) map[string]string,
	syncRuntimeThreads func([]ThreadListItem),
) ([]ThreadListItem, error) {
	agents := []AgentInfo(nil)
	if runningAgents != nil {
		agents = runningAgents()
	}
	threads := make([]ThreadListItem, 0, len(agents)+32)
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
		threads = append(threads, ThreadListItem{ID: id, Name: name, State: item.State})
		seen[id] = struct{}{}
	}
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

func AppendThreadHistoryFromStores(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
	appendHistoryFromBindingStore func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem,
	appendHistoryFromStatusStore func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem,
	appendHistoryFromArchiveState func(context.Context, []ThreadListItem, map[string]struct{}, string) []ThreadListItem,
) []ThreadListItem {
	idMethod := strings.TrimSpace(methodName)
	if idMethod == "" {
		idMethod = "thread/list"
	}
	if appendHistoryFromBindingStore != nil {
		threads = appendHistoryFromBindingStore(ctx, threads, seen, idMethod)
	}
	if appendHistoryFromStatusStore != nil {
		threads = appendHistoryFromStatusStore(ctx, threads, seen, idMethod)
	}
	if appendHistoryFromArchiveState != nil {
		threads = appendHistoryFromArchiveState(ctx, threads, seen, idMethod)
	}
	return threads
}

func AppendHistoryFromBindingStore(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
	listBindings func(context.Context) ([]AgentCodexBinding, error),
) []ThreadListItem {
	if listBindings == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(history.EnsureContext(ctx), 5*time.Second)
	items, err := listBindings(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from agent_codex_binding failed", logger.FieldError, err)
		return threads
	}
	return AppendThreadItems(threads, seen, items)
}

func AppendHistoryFromStatusStore(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
	listStatus func(context.Context) ([]AgentStatus, error),
) []ThreadListItem {
	if listStatus == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(history.EnsureContext(ctx), 5*time.Second)
	items, err := listStatus(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from agent_status failed", logger.FieldError, err)
		return threads
	}
	return AppendThreadItems(threads, seen, items)
}

func AppendHistoryFromArchiveState(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) []ThreadListItem {
	if loadThreadArchiveMap == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(history.EnsureContext(ctx), 3*time.Second)
	archivedMap, err := loadThreadArchiveMap(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from threadArchives.chat failed", logger.FieldError, err)
		return threads
	}
	return AppendArchivedThreads(threads, seen, archivedMap)
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
		threads = append(threads, ThreadListItem{ID: item.ID, Name: item.ID, State: "idle"})
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
		for threadID, alias := range typed {
			addNormalizedThreadAlias(aliases, threadID, alias)
		}
	case map[string]any:
		for threadID, alias := range typed {
			addNormalizedThreadAlias(aliases, threadID, alias)
		}
	case string:
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &decoded); err == nil {
			for threadID, alias := range decoded {
				addNormalizedThreadAlias(aliases, threadID, alias)
			}
		}
	case json.RawMessage:
		decoded := map[string]any{}
		if err := json.Unmarshal(typed, &decoded); err == nil {
			for threadID, alias := range decoded {
				addNormalizedThreadAlias(aliases, threadID, alias)
			}
		}
	}
	return aliases
}

func addNormalizedThreadAlias(aliases map[string]string, threadID string, alias any) {
	if aliases == nil {
		return
	}
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
		alias := strings.TrimSpace(aliases[id])
		if alias == "" {
			continue
		}
		threads[i].Name = alias
	}
}

func LoadThreadAliases(ctx context.Context, getPref func(context.Context, string) (any, error)) map[string]string {
	if getPref == nil {
		return map[string]string{}
	}
	value, err := getPref(ctx, PrefThreadAliases)
	if err != nil {
		logger.Warn("thread aliases: load preference failed", logger.FieldError, err)
		return map[string]string{}
	}
	return NormalizeThreadAliases(value)
}

func PersistThreadAlias(
	ctx context.Context,
	threadID string,
	alias string,
	getPref func(context.Context, string) (any, error),
	setPref func(context.Context, string, any) error,
) error {
	if getPref == nil || setPref == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	value, err := getPref(ctx, PrefThreadAliases)
	if err != nil {
		return err
	}
	aliases := NormalizeThreadAliases(value)
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
	default:
		return ""
	}
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

func appendThreadSnapshot(snapshots []ThreadSnapshot, id, name, state string) []ThreadSnapshot {
	trimmedID, trimmedName, ok := NormalizeThreadListItem(id, name)
	if !ok {
		return snapshots
	}
	return append(snapshots, ThreadSnapshot{ID: trimmedID, Name: trimmedName, State: state})
}

func appendBindingThreadItems(threads []ThreadListItem, seen map[string]struct{}, items []AgentCodexBinding) []ThreadListItem {
	for _, item := range items {
		threads = AppendThreadListItem(threads, seen, item.AgentID, item.AgentID, "idle")
	}
	return threads
}

func appendAgentStatusThreadItems(threads []ThreadListItem, seen map[string]struct{}, items []AgentStatus) []ThreadListItem {
	for _, item := range items {
		threads = AppendThreadListItem(threads, seen, item.AgentID, item.AgentName, "idle")
	}
	return threads
}

func appendRunnerThreadItems(threads []ThreadListItem, seen map[string]struct{}, items []AgentInfo) []ThreadListItem {
	for _, item := range items {
		threads = AppendThreadListItem(threads, seen, item.ID, item.Name, item.State)
	}
	return threads
}

func toRunnerThreadSnapshots(items []AgentInfo) []ThreadSnapshot {
	snapshots := make([]ThreadSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = appendThreadSnapshot(snapshots, item.ID, item.Name, item.State)
	}
	return snapshots
}

func toListItemSnapshots(items []ThreadListItem) []ThreadSnapshot {
	snapshots := make([]ThreadSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = appendThreadSnapshot(snapshots, item.ID, item.Name, item.State)
	}
	return snapshots
}

func AppendThreadItems[T any](threads []ThreadListItem, seen map[string]struct{}, items []T) []ThreadListItem {
	switch src := any(items).(type) {
	case []AgentCodexBinding:
		return appendBindingThreadItems(threads, seen, src)
	case []AgentStatus:
		return appendAgentStatusThreadItems(threads, seen, src)
	case []AgentInfo:
		return appendRunnerThreadItems(threads, seen, src)
	default:
		return threads
	}
}

func ToThreadSnapshots[T any](items []T) []ThreadSnapshot {
	switch src := any(items).(type) {
	case []AgentInfo:
		return toRunnerThreadSnapshots(src)
	case []ThreadListItem:
		return toListItemSnapshots(src)
	default:
		return nil
	}
}

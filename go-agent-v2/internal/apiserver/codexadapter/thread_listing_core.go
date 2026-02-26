package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func paginateLoadedThreadIDs(ids []string, cursor *string, limit *uint32) ([]string, *string) {
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

func buildThreadList(
	ctx context.Context,
	methodName string,
	syncRuntime bool,
	runningAgents func() []runner.AgentInfo,
	appendHistoryFromStores func(context.Context, []threadListItem, map[string]struct{}, string) []threadListItem,
	loadThreadAliases func(context.Context) map[string]string,
	syncRuntimeThreads func([]threadListItem),
) ([]threadListItem, error) {
	agents := []runner.AgentInfo(nil)
	if runningAgents != nil {
		agents = runningAgents()
	}
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
		threads = append(threads, threadListItem{ID: id, Name: name, State: string(item.State)})
		seen[id] = struct{}{}
	}
	if appendHistoryFromStores != nil {
		threads = appendHistoryFromStores(ctx, threads, seen, methodName)
	}
	if loadThreadAliases != nil {
		applyThreadAliases(threads, loadThreadAliases(ctx))
	}
	if syncRuntime && syncRuntimeThreads != nil {
		syncRuntimeThreads(threads)
	}
	return threads, nil
}

func appendThreadHistoryFromStores(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
	appendHistoryFromBindingStore func(context.Context, []threadListItem, map[string]struct{}, string) []threadListItem,
	appendHistoryFromStatusStore func(context.Context, []threadListItem, map[string]struct{}, string) []threadListItem,
	appendHistoryFromArchiveState func(context.Context, []threadListItem, map[string]struct{}, string) []threadListItem,
) []threadListItem {
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

func appendHistoryFromBindingStore(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
	bindingStore *store.AgentCodexBindingStore,
) []threadListItem {
	if bindingStore == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(ensureContext(ctx), 5*time.Second)
	bindings, err := bindingStore.ListAll(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from agent_codex_binding failed", logger.FieldError, err)
		return threads
	}
	return appendThreadItems(threads, seen, bindings)
}

func appendHistoryFromStatusStore(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
	statusStore *store.AgentStatusStore,
) []threadListItem {
	if statusStore == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(ensureContext(ctx), 5*time.Second)
	items, err := statusStore.List(dbCtx, "")
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from agent_status failed", logger.FieldError, err)
		return threads
	}
	return appendThreadItems(threads, seen, items)
}

func appendHistoryFromArchiveState(
	ctx context.Context,
	threads []threadListItem,
	seen map[string]struct{},
	methodName string,
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) []threadListItem {
	if loadThreadArchiveMap == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(ensureContext(ctx), 3*time.Second)
	archivedMap, err := loadThreadArchiveMap(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from threadArchives.chat failed", logger.FieldError, err)
		return threads
	}
	return appendArchivedThreads(threads, seen, archivedMap)
}

func appendArchivedThreads(threads []threadListItem, seen map[string]struct{}, archived map[string]int64) []threadListItem {
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
		threads = append(threads, threadListItem{
			ID:    item.ID,
			Name:  item.ID,
			State: "idle",
		})
		seen[item.ID] = struct{}{}
	}
	return threads
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
	return append(threads, threadListItem{
		ID:    trimmedID,
		Name:  trimmedName,
		State: state,
	})
}

// normalizeThreadAliases parses various formats (map, JSON string, json.RawMessage)
// into a normalized map[string]string of thread aliases.
func normalizeThreadAliases(value any) map[string]string {
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
	name := strings.TrimSpace(stringValue(alias))
	if name == "" || name == id {
		return
	}
	aliases[id] = name
}

func applyThreadAliases(threads []threadListItem, aliases map[string]string) {
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

func loadThreadAliases(ctx context.Context, getPref func(context.Context, string) (any, error)) map[string]string {
	if getPref == nil {
		return map[string]string{}
	}
	value, err := getPref(ctx, prefThreadAliases)
	if err != nil {
		logger.Warn("thread aliases: load preference failed", logger.FieldError, err)
		return map[string]string{}
	}
	return normalizeThreadAliases(value)
}

func persistThreadAlias(
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
	value, err := getPref(ctx, prefThreadAliases)
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
	return setPref(ctx, prefThreadAliases, aliases)
}

// stringValue extracts a string from any value (string or fmt.Stringer).
func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

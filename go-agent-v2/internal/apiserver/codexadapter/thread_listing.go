package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const prefThreadAliases = "threads.aliases"

// threadListItem models thread list entry payload.
type threadListItem = contracts.ThreadListItem

// ThreadList returns thread/list payload and syncs runtime snapshots.
func (a *Adapter) ThreadList(ctx context.Context) ([]threadListItem, error) {
	return a.threadList(ctx, "thread/list", true)
}

// ThreadLoadedList returns thread/loaded/list payload.
func (a *Adapter) ThreadLoadedList(ctx context.Context) ([]threadListItem, error) {
	return a.threadList(ctx, "thread/loaded/list", false)
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

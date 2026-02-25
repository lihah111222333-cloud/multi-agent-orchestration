package codexadapter

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ThreadListItem models thread list entry payload.
type ThreadListItem = contracts.ThreadListItem

// ThreadList returns thread/list payload and syncs runtime snapshots.
func (a *Adapter) ThreadList(ctx context.Context) ([]ThreadListItem, error) {
	return a.threadList(ctx, "thread/list", true)
}

// ThreadLoadedList returns thread/loaded/list payload.
func (a *Adapter) ThreadLoadedList(ctx context.Context) ([]ThreadListItem, error) {
	return a.threadList(ctx, "thread/loaded/list", false)
}

func (a *Adapter) threadList(ctx context.Context, methodName string, syncRuntime bool) ([]ThreadListItem, error) {
	agents := a.runningAgents()
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
		threads = append(threads, ThreadListItem{
			ID:    id,
			Name:  name,
			State: string(item.State),
		})
		seen[id] = struct{}{}
	}

	threads = a.appendThreadHistoryFromStores(ctx, threads, seen, methodName)
	applyThreadAliases(threads, a.loadThreadAliases(ctx))
	if syncRuntime && a != nil && a.ctx != nil && a.ctx.UIRuntime != nil {
		a.ctx.UIRuntime.ReplaceThreads(toThreadSnapshots(threads))
	}
	return threads, nil
}

func (a *Adapter) runningAgents() []runner.AgentInfo {
	if a == nil || a.ctx == nil || a.ctx.Manager == nil {
		return nil
	}
	return a.ctx.Manager.List()
}

func (a *Adapter) appendThreadHistoryFromStores(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
) []ThreadListItem {
	idMethod := strings.TrimSpace(methodName)
	if idMethod == "" {
		idMethod = "thread/list"
	}
	if a == nil || a.ctx == nil {
		return threads
	}
	threads = a.appendHistoryFromBindingStore(ctx, threads, seen, idMethod)
	threads = a.appendHistoryFromStatusStore(ctx, threads, seen, idMethod)
	threads = a.appendHistoryFromArchiveState(ctx, threads, seen, idMethod)
	return threads
}

func (a *Adapter) appendHistoryFromBindingStore(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
) []ThreadListItem {
	if a == nil || a.ctx == nil || a.ctx.BindingStore == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	bindings, err := a.ctx.BindingStore.ListAll(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from agent_codex_binding failed", logger.FieldError, err)
		return threads
	}
	return appendThreadItems(threads, seen, bindings)
}

func (a *Adapter) appendHistoryFromStatusStore(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
) []ThreadListItem {
	if a == nil || a.ctx == nil || a.ctx.AgentStatusStore == nil {
		return threads
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	items, err := a.ctx.AgentStatusStore.List(dbCtx, "")
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from agent_status failed", logger.FieldError, err)
		return threads
	}
	return appendThreadItems(threads, seen, items)
}

func (a *Adapter) appendHistoryFromArchiveState(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
) []ThreadListItem {
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	archivedMap, err := a.loadThreadArchiveMap(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(methodName+": load history threads from threadArchives.chat failed", logger.FieldError, err)
		return threads
	}
	return appendArchivedThreads(threads, seen, archivedMap)
}

func appendArchivedThreads(threads []ThreadListItem, seen map[string]struct{}, archived map[string]int64) []ThreadListItem {
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
		threads = append(threads, ThreadListItem{
			ID:    item.ID,
			Name:  item.ID,
			State: "idle",
		})
		seen[item.ID] = struct{}{}
	}
	return threads
}

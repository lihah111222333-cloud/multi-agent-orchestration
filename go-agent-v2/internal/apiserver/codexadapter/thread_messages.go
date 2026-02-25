package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	threadMessageHydrationMaxRecords = 20000
	threadMessageHydrationPageSize   = 500
	prefKeyShowInjectedPromptInChat  = "settings.showInjectedPromptInChat"
)

// LoadAllThreadMessagesFromRollout loads rollout history using constructor-time dependencies.
func (a *Adapter) LoadAllThreadMessagesFromRollout(ctx context.Context, threadID string) ([]ThreadHistoryMessage, error) {
	showInjectedPromptInChat := a.showInjectedPromptInChat(ctx)
	return LoadAllThreadMessagesFromCodexRollout(
		ctx,
		threadID,
		a.resolveRolloutHistorySource,
		NormalizeCodexThreadID,
		nil,
		nil,
		showInjectedPromptInChat,
	)
}

func (a *Adapter) showInjectedPromptInChat(ctx context.Context) bool {
	if a == nil || a.ctx == nil || a.ctx.Store == nil {
		return false
	}
	value, err := a.ctx.Store.Get(ctx, prefKeyShowInjectedPromptInChat)
	if err != nil {
		logger.Warn("ui preferences: load injected prompt visibility failed", logger.FieldError, err)
		return false
	}
	return parsePreferenceBool(value, false)
}

func parsePreferenceBool(value any, fallback bool) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return fallback
}

func (a *Adapter) threadMessagesStats(threadID string) (int, int) {
	if a == nil || a.ctx == nil || a.ctx.UIRuntime == nil {
		return 0, 0
	}
	runtime := a.ctx.UIRuntime
	return len(runtime.ThreadDiff(threadID)), len(runtime.ThreadTimeline(threadID))
}

func (a *Adapter) handleThreadMessagesHydration(threadID string, all, page []ThreadHistoryMessage, before int64, _ int) {
	if a == nil || a.ctx == nil || a.ctx.UIRuntime == nil {
		return
	}
	runtime := a.ctx.UIRuntime
	HandleThreadMessagesHydration(
		threadID,
		all,
		page,
		before,
		calculateHydrationLoadLimit,
		runtime.HydrateHistory,
		a.streamRemainingHistory,
		util.SafeGo,
	)
}

func (a *Adapter) streamRemainingHistory(threadID string, all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int) {
	if a == nil || a.ctx == nil || a.ctx.UIRuntime == nil {
		return
	}
	runtime := a.ctx.UIRuntime
	StreamRemainingHistory(
		threadID,
		all,
		firstPage,
		limit,
		threadMessageHydrationPageSize,
		PaginateRolloutMessages,
		runtime.AppendHistory,
		func(id string) int { return len(runtime.ThreadDiff(id)) },
		func(id string) int { return len(runtime.ThreadTimeline(id)) },
		func(id string, totalLoaded int, pages int) {
			a.ctx.Notify("thread/messages/page", map[string]any{
				"threadId":   id,
				"totalCount": totalLoaded,
				"pages":      pages,
			})
		},
	)
}

func calculateHydrationLoadLimit(initialCount int, total int64) int {
	if initialCount < 0 {
		initialCount = 0
	}
	limit := initialCount
	if total > int64(limit) {
		limit = int(total)
	}
	if limit > threadMessageHydrationMaxRecords {
		limit = threadMessageHydrationMaxRecords
	}
	return limit
}

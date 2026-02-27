package codexadapter

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	threadMessageHydrationMaxRecords = 20000
	threadMessageHydrationPageSize   = 500
	prefKeyShowInjectedPromptInChat  = "settings.showInjectedPromptInChat"
)

type threadHistoryMessage struct {
	ID        int64           `json:"id"`
	AgentID   string          `json:"agentId"`
	Role      string          `json:"role"`
	EventType string          `json:"eventType"`
	Method    string          `json:"method"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

// ThreadMessages handles rollout history paging and runtime hydration.
func (a *Adapter) ThreadMessages(ctx context.Context, threadID string, limit int, before int64) (map[string]any, error) {
	id, err := requireThreadID("Server.threadMessages", threadID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	allMsgs, err := a.LoadAllThreadMessagesFromRollout(ctx, id)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadMessages", "load codex rollout messages")
	}
	total := int64(len(allMsgs))
	msgs := paginateRolloutMessages(allMsgs, limit, before)
	logger.Info("thread/messages: page selected",
		append(threadLogFields(id),
			"before", before,
			"limit", limit,
			"page_count", len(msgs),
			"total", total,
		)...,
	)

	a.handleThreadMessagesHydration(id, allMsgs, msgs, before, limit)

	diffLen, timelineLen := a.threadMessagesStats(id)
	logger.Info("thread/messages: response prepared",
		append(threadLogFields(id),
			"page_count", len(msgs),
			"total", total,
			"timeline_len", timelineLen,
			"diff_len", diffLen,
		)...,
	)
	return buildThreadMessagesResponse(msgs, total), nil
}

func buildThreadMessagesResponse(messages []threadHistoryMessage, total int64) map[string]any {
	return map[string]any{
		"messages": messages,
		"total":    total,
	}
}

func buildThreadMessagesPagePayload(threadID string, totalLoaded int, pages int) map[string]any {
	return map[string]any{
		"threadId":   strings.TrimSpace(threadID),
		"totalCount": totalLoaded,
		"pages":      pages,
	}
}

// LoadAllThreadMessagesFromRollout loads rollout history using constructor-time dependencies.
func (a *Adapter) LoadAllThreadMessagesFromRollout(ctx context.Context, threadID string) ([]threadHistoryMessage, error) {
	showInjectedPromptInChat := a.showInjectedPromptInChat(ctx)
	return loadAllThreadMessagesFromCodexRollout(
		ctx,
		threadID,
		a.resolveRolloutHistorySource,
		normalizeCodexThreadID,
		showInjectedPromptInChat,
	)
}

func (a *Adapter) showInjectedPromptInChat(ctx context.Context) bool {
	store := a.store()
	if store == nil {
		return false
	}
	value, err := store.Get(ctx, prefKeyShowInjectedPromptInChat)
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
	runtime := a.uiRuntime()
	if runtime == nil {
		return 0, 0
	}
	return len(runtime.ThreadDiff(threadID)), len(runtime.ThreadTimeline(threadID))
}

func (a *Adapter) handleThreadMessagesHydration(threadID string, all, page []threadHistoryMessage, before int64, _ int) {
	runtime := a.uiRuntime()
	if runtime == nil {
		return
	}
	a.hydrateThreadMessagesWithRuntime(threadID, all, page, before, runtime)
}

func (a *Adapter) hydrateThreadMessagesWithRuntime(threadID string, all, page []threadHistoryMessage, before int64, runtime *uistate.RuntimeManager) {
	handleThreadMessagesHydration(threadID, all, page, before, calculateHydrationLoadLimit, runtime.HydrateHistory, a.streamRemainingHistory, util.SafeGo)
}

func (a *Adapter) streamRemainingHistory(threadID string, all []threadHistoryMessage, firstPage []threadHistoryMessage, limit int) {
	runtime := a.uiRuntime()
	if runtime == nil {
		return
	}
	notifyPage := func(id string, totalLoaded int, pages int) {
		a.notify("thread/messages/page", buildThreadMessagesPagePayload(id, totalLoaded, pages))
	}
	streamRemainingHistoryWithRuntime(threadID, all, firstPage, limit, runtime, notifyPage)
}

func streamRemainingHistoryWithRuntime(
	threadID string,
	all []threadHistoryMessage,
	firstPage []threadHistoryMessage,
	limit int,
	runtime *uistate.RuntimeManager,
	notifyPage func(string, int, int),
) {
	streamRemainingHistory(
		threadID,
		all,
		firstPage,
		limit,
		threadMessageHydrationPageSize,
		paginateRolloutMessages,
		runtime.AppendHistory,
		func(id string) int { return len(runtime.ThreadDiff(id)) },
		func(id string) int { return len(runtime.ThreadTimeline(id)) },
		notifyPage,
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

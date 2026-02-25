package codexadapter

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/codex"
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
	return map[string]any{
		"messages": msgs,
		"total":    total,
	}, nil
}

// LoadAllThreadMessagesFromRollout loads rollout history using constructor-time dependencies.
func (a *Adapter) LoadAllThreadMessagesFromRollout(ctx context.Context, threadID string) ([]threadHistoryMessage, error) {
	showInjectedPromptInChat := a.showInjectedPromptInChat(ctx)
	return loadAllThreadMessagesFromCodexRollout(
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
	handleThreadMessagesHydration(
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

func (a *Adapter) streamRemainingHistory(threadID string, all []threadHistoryMessage, firstPage []threadHistoryMessage, limit int) {
	runtime := a.uiRuntime()
	if runtime == nil {
		return
	}
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
		func(id string, totalLoaded int, pages int) {
			a.notify("thread/messages/page", map[string]any{
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

// loadAllThreadMessagesFromCodexRollout loads and normalizes rollout history.
func loadAllThreadMessagesFromCodexRollout(
	ctx context.Context,
	threadID string,
	resolveRolloutHistorySource func(context.Context, string) (string, string),
	normalizeCodexThreadID func(string) string,
	findRolloutPath func(string) (string, error),
	readRolloutMessagesWithTrim func(path string, trimInjected bool) ([]codex.RolloutMessage, error),
	showInjectedPromptInChat bool,
) ([]threadHistoryMessage, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return []threadHistoryMessage{}, nil
	}
	resolve := resolveRolloutHistorySource
	if resolve == nil {
		return []threadHistoryMessage{}, nil
	}
	normalize := normalizeCodexThreadID
	if normalize == nil {
		normalize = strings.TrimSpace
	}
	findRollout := findRolloutPath
	if findRollout == nil {
		findRollout = codex.FindRolloutPath
	}
	readRollout := readRolloutMessagesWithTrim
	if readRollout == nil {
		readRollout = codex.ReadRolloutMessagesWithTrim
	}

	codexThreadID, rolloutPath := resolve(ctx, threadID)
	codexThreadID = normalize(codexThreadID)
	if codexThreadID == "" {
		return []threadHistoryMessage{}, nil
	}

	path := strings.TrimSpace(rolloutPath)
	if path == "" {
		resolvedPath, err := findRollout(codexThreadID)
		if err != nil {
			return []threadHistoryMessage{}, nil
		}
		path = resolvedPath
	}
	if path == "" {
		return []threadHistoryMessage{}, nil
	}
	if _, err := os.Stat(path); err != nil {
		return []threadHistoryMessage{}, nil
	}

	trimInjected := !showInjectedPromptInChat
	rolloutMsgs, err := readRollout(path, trimInjected)
	if err != nil {
		return nil, err
	}
	if len(rolloutMsgs) == 0 {
		return []threadHistoryMessage{}, nil
	}

	all := make([]threadHistoryMessage, 0, len(rolloutMsgs))
	for i, item := range rolloutMsgs {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		createdAt := parseRolloutTimestamp(item.Timestamp)
		eventType := ""
		if role == "assistant" {
			eventType = agentcore.EventAgentMessage
		}
		all = append(all, threadHistoryMessage{
			ID:        int64(i + 1),
			AgentID:   threadID,
			Role:      role,
			EventType: eventType,
			Method:    "",
			Content:   item.Content,
			Metadata:  nil,
			CreatedAt: createdAt,
		})
	}
	if len(all) == 0 {
		return []threadHistoryMessage{}, nil
	}
	return all, nil
}

// historyMessagesToRecords converts thread messages to UI history records.
func historyMessagesToRecords(msgs []threadHistoryMessage) []uistate.HistoryRecord {
	records := make([]uistate.HistoryRecord, 0, len(msgs))
	for _, msg := range msgs {
		records = append(records, uistate.HistoryRecord{
			ID:        msg.ID,
			Role:      msg.Role,
			EventType: msg.EventType,
			Method:    msg.Method,
			Content:   msg.Content,
			Metadata:  msg.Metadata,
			CreatedAt: msg.CreatedAt,
		})
	}
	return records
}

// parseRolloutTimestamp parses rollout timestamp in RFC3339/RFC3339Nano formats.
func parseRolloutTimestamp(raw string) time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	return time.Time{}
}

// paginateRolloutMessages selects newest-first page from rollout messages.
func paginateRolloutMessages(all []threadHistoryMessage, limit int, before int64) []threadHistoryMessage {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(all) == 0 {
		return []threadHistoryMessage{}
	}
	page := make([]threadHistoryMessage, 0, min(limit, len(all)))
	for idx := len(all) - 1; idx >= 0; idx-- {
		item := all[idx]
		if before > 0 && item.ID >= before {
			continue
		}
		page = append(page, item)
		if len(page) >= limit {
			break
		}
	}
	return page
}

// resolveRolloutHistorySource resolves codex thread id and optional rollout path.
func resolveRolloutHistorySource(
	ctx context.Context,
	threadID string,
	getRunningCodexThreadID func(threadID string) string,
	findBinding func(context.Context, string) (codexThreadID string, rolloutPath string, err error),
	findStatusSessionID func(context.Context, string) (string, error),
	normalizeCodexThreadID func(string) string,
) (codexThreadID string, rolloutPath string) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", ""
	}
	normalize := normalizeCodexThreadID
	if normalize == nil {
		normalize = strings.TrimSpace
	}

	if getRunningCodexThreadID != nil {
		candidate := normalize(getRunningCodexThreadID(id))
		if candidate != "" {
			return candidate, ""
		}
	}

	if findBinding != nil {
		boundID, path, err := findBinding(ctx, id)
		if err == nil {
			candidate := normalize(boundID)
			if candidate != "" {
				return candidate, strings.TrimSpace(path)
			}
		}
	}

	if findStatusSessionID != nil {
		sessionID, err := findStatusSessionID(ctx, id)
		if err == nil {
			candidate := normalize(sessionID)
			if candidate != "" {
				return candidate, ""
			}
		}
	}

	if candidate := normalize(id); candidate != "" {
		return candidate, ""
	}
	return "", ""
}

// ResolveRolloutHistorySource resolves codex thread id and rollout path via adapter context stores.
func (a *Adapter) ResolveRolloutHistorySource(
	ctx context.Context,
	threadID string,
	normalizeCodexThreadID func(string) string,
) (string, string) {
	return resolveRolloutHistorySource(
		ctx,
		threadID,
		a.runningCodexThreadID,
		a.bindingRolloutSourceByAgentID,
		a.statusSessionIDByAgentID,
		normalizeCodexThreadID,
	)
}

func (a *Adapter) runningCodexThreadID(threadID string) string {
	manager := a.manager()
	if manager == nil {
		return ""
	}
	proc := manager.Get(threadID)
	if proc == nil || proc.Client == nil {
		return ""
	}
	return a.GetThreadID(proc)
}

func (a *Adapter) bindingRolloutSourceByAgentID(ctx context.Context, agentID string) (string, string, error) {
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return "", "", nil
	}
	binding, err := bindingStore.FindByAgentID(ctx, agentID)
	if err != nil {
		return "", "", err
	}
	if binding == nil {
		return "", "", nil
	}
	return binding.CodexThreadID, binding.RolloutPath, nil
}

func (a *Adapter) resolveRolloutHistorySource(ctx context.Context, threadID string) (string, string) {
	return a.ResolveRolloutHistorySource(ctx, threadID, NormalizeCodexThreadID)
}

// streamRemainingHistory appends history pages beyond first page and emits one summary notification.
func streamRemainingHistory(
	threadID string,
	all []threadHistoryMessage,
	first []threadHistoryMessage,
	limit int,
	pageSize int,
	paginate func([]threadHistoryMessage, int, int64) []threadHistoryMessage,
	appendHistory func(string, []uistate.HistoryRecord),
	threadDiffLen func(string) int,
	threadTimelineLen func(string) int,
	notifyPage func(threadID string, totalLoaded int, pages int),
) {
	if appendHistory == nil || len(all) == 0 || limit <= 0 || limit <= len(first) {
		return
	}
	if paginate == nil {
		paginate = paginateRolloutMessages
	}
	if pageSize <= 0 {
		pageSize = 500
	}

	before := int64(0)
	if len(first) > 0 {
		before = first[len(first)-1].ID
	}

	remaining := make([]threadHistoryMessage, 0, limit-len(first))
	pageNum := 1
	loaded := len(first)

	for loaded < limit {
		batchLimit := min(pageSize, limit-loaded)
		batch := paginate(all, batchLimit, before)
		if len(batch) == 0 {
			break
		}
		remaining = append(remaining, batch...)
		pageNum++
		loaded += len(batch)
		if len(batch) < batchLimit {
			break
		}
		before = batch[len(batch)-1].ID
	}
	if len(remaining) == 0 {
		return
	}

	appendHistory(threadID, historyMessagesToRecords(remaining))
	diffLen := 0
	if threadDiffLen != nil {
		diffLen = threadDiffLen(threadID)
	}
	timelineLen := 0
	if threadTimelineLen != nil {
		timelineLen = threadTimelineLen(threadID)
	}
	if notifyPage != nil {
		notifyPage(threadID, loaded, pageNum)
	}
	logger.Debug("thread/messages: streaming hydration complete",
		logger.FieldAgentID, threadID,
		"total_loaded", loaded,
		"pages", pageNum,
	)
	logger.Info("thread/messages: streaming page notified",
		append(threadLogFields(threadID),
			"total_loaded", loaded,
			"pages", pageNum,
			"timeline_len", timelineLen,
			"diff_len", diffLen,
		)...,
	)
}

// handleThreadMessagesHydration hydrates first page immediately and streams the rest in background.
func handleThreadMessagesHydration(
	threadID string,
	all []threadHistoryMessage,
	page []threadHistoryMessage,
	before int64,
	calculateHydrationLoadLimit func(initialCount int, total int64) int,
	hydrateHistory func(threadID string, records []uistate.HistoryRecord) bool,
	streamRemainingHistory func(threadID string, all []threadHistoryMessage, firstPage []threadHistoryMessage, limit int),
	asyncGo func(func()),
) {
	if hydrateHistory == nil {
		return
	}
	total := int64(len(all))
	if before == 0 {
		firstRecords := historyMessagesToRecords(page)
		hydrated := hydrateHistory(threadID, firstRecords)
		logger.Debug("thread/messages: first page hydrated",
			logger.FieldAgentID, threadID,
			"first_page_count", len(page),
			"total", total,
			"hydrated", hydrated,
		)
		if hydrated && calculateHydrationLoadLimit != nil && streamRemainingHistory != nil {
			hydrateLimit := calculateHydrationLoadLimit(len(page), total)
			if hydrateLimit > len(page) {
				threadIDCopy := threadID
				allCopy := append([]threadHistoryMessage(nil), all...)
				firstCopy := append([]threadHistoryMessage(nil), page...)
				runAsync := asyncGo
				if runAsync == nil {
					runAsync = util.SafeGo
				}
				runAsync(func() {
					streamRemainingHistory(threadIDCopy, allCopy, firstCopy, hydrateLimit)
				})
			}
		}
		return
	}
	records := historyMessagesToRecords(page)
	_ = hydrateHistory(threadID, records)
}

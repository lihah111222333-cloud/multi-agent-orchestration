package messages

import (
	"context"
	"os"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
	rolloutsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/rollout"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	ThreadMessageHydrationMaxRecords = 20000
	ThreadMessageHydrationPageSize   = 500
)

type ThreadHistoryMessage = rolloutsvc.ThreadHistoryMessage

func BuildThreadMessagesResponse(messages []ThreadHistoryMessage, total int64) map[string]any {
	return map[string]any{"messages": messages, "total": total}
}

func BuildThreadMessagesPagePayload(threadID string, totalLoaded int, pages int) map[string]any {
	return map[string]any{"threadId": strings.TrimSpace(threadID), "totalCount": totalLoaded, "pages": pages}
}

func ParsePreferenceBool(value any, fallback bool) bool {
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

func CalculateHydrationLoadLimit(initialCount int, total int64) int {
	if initialCount < 0 {
		initialCount = 0
	}
	limit := initialCount
	if total > int64(limit) {
		limit = int(total)
	}
	if limit > ThreadMessageHydrationMaxRecords {
		limit = ThreadMessageHydrationMaxRecords
	}
	return limit
}

func LoadAllThreadMessagesFromCodexRollout(
	ctx context.Context,
	threadID string,
	resolveRolloutHistorySource func(context.Context, string) (string, string),
	normalizeCodexThreadID func(string) string,
	showInjectedPromptInChat bool,
) ([]ThreadHistoryMessage, error) {
	return rolloutsvc.LoadAllThreadMessagesFromCodexRollout(
		ctx,
		threadID,
		resolveRolloutHistorySource,
		normalizeCodexThreadID,
		codex.FindRolloutPath,
		func(path string) bool { _, err := os.Stat(path); return err == nil },
		codex.ReadRolloutMessagesWithTrim,
		showInjectedPromptInChat,
	)
}

func PaginateRolloutMessages(all []ThreadHistoryMessage, limit int, before int64) []ThreadHistoryMessage {
	return rolloutsvc.PaginateRolloutMessages(all, limit, before)
}

func StreamRemainingHistory(
	threadID string,
	all []ThreadHistoryMessage,
	first []ThreadHistoryMessage,
	limit int,
	pageSize int,
	paginate func([]ThreadHistoryMessage, int, int64) []ThreadHistoryMessage,
	appendHistory func(string, []ThreadHistoryMessage),
	threadDiffLen func(string) int,
	threadTimelineLen func(string) int,
	notifyPage func(threadID string, totalLoaded int, pages int),
) {
	if appendHistory == nil || len(all) == 0 || limit <= 0 || limit <= len(first) {
		return
	}
	if paginate == nil {
		paginate = PaginateRolloutMessages
	}
	if pageSize <= 0 {
		pageSize = ThreadMessageHydrationPageSize
	}

	before := int64(0)
	if len(first) > 0 {
		before = first[len(first)-1].ID
	}

	pageNum := 1
	loaded := len(first)
	for loaded < limit {
		batchLimit := min(pageSize, limit-loaded)
		batch := paginate(all, batchLimit, before)
		if len(batch) == 0 {
			break
		}
		appendHistory(threadID, batch)
		pageNum++
		loaded += len(batch)
		if len(batch) < batchLimit {
			break
		}
		before = batch[len(batch)-1].ID
	}
	if loaded == len(first) {
		return
	}
	if threadDiffLen != nil {
		_ = threadDiffLen(threadID)
	}
	if threadTimelineLen != nil {
		_ = threadTimelineLen(threadID)
	}
	if notifyPage != nil {
		notifyPage(threadID, loaded, pageNum)
	}
}

func HandleThreadMessagesHydration(
	threadID string,
	all []ThreadHistoryMessage,
	page []ThreadHistoryMessage,
	before int64,
	calculateHydrationLoadLimit func(initialCount int, total int64) int,
	hydrateHistory func(threadID string, records []ThreadHistoryMessage) bool,
	streamRemainingHistory func(threadID string, all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int),
	asyncGo func(func()),
) {
	if hydrateHistory == nil {
		return
	}
	if before == 0 {
		hydrated := hydrateHistory(threadID, page)
		logger.Debug("thread/messages: first page hydrated",
			logger.FieldAgentID, threadID,
			"first_page_count", len(page),
			"total", len(all),
			"hydrated", hydrated,
		)
		if hydrated && calculateHydrationLoadLimit != nil && streamRemainingHistory != nil {
			hydrateLimit := calculateHydrationLoadLimit(len(page), int64(len(all)))
			if hydrateLimit > len(page) {
				runAsync := asyncGo
				if runAsync == nil {
					runAsync = util.SafeGo
				}
				runAsync(func() {
					streamRemainingHistory(threadID, all, page, hydrateLimit)
				})
			}
		}
		return
	}
	hydrateHistory(threadID, page)
}

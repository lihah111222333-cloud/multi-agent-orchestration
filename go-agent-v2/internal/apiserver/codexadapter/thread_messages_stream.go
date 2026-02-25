package codexadapter

import (
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// StreamRemainingHistory appends history pages beyond first page and emits one summary notification.
func StreamRemainingHistory(
	threadID string,
	all []ThreadHistoryMessage,
	first []ThreadHistoryMessage,
	limit int,
	pageSize int,
	paginate func([]ThreadHistoryMessage, int, int64) []ThreadHistoryMessage,
	appendHistory func(string, []uistate.HistoryRecord),
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
		pageSize = 500
	}

	before := int64(0)
	if len(first) > 0 {
		before = first[len(first)-1].ID
	}

	remaining := make([]ThreadHistoryMessage, 0, limit-len(first))
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

	appendHistory(threadID, HistoryMessagesToRecords(remaining))
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
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		"total_loaded", loaded,
		"pages", pageNum,
		"timeline_len", timelineLen,
		"diff_len", diffLen,
	)
}

// HandleThreadMessagesHydration hydrates first page immediately and streams the rest in background.
func HandleThreadMessagesHydration(
	threadID string,
	all []ThreadHistoryMessage,
	page []ThreadHistoryMessage,
	before int64,
	calculateHydrationLoadLimit func(initialCount int, total int64) int,
	hydrateHistory func(threadID string, records []uistate.HistoryRecord) bool,
	streamRemainingHistory func(threadID string, all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int),
	asyncGo func(func()),
) {
	if hydrateHistory == nil {
		return
	}
	total := int64(len(all))
	if before == 0 {
		firstRecords := HistoryMessagesToRecords(page)
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
				allCopy := append([]ThreadHistoryMessage(nil), all...)
				firstCopy := append([]ThreadHistoryMessage(nil), page...)
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
	records := HistoryMessagesToRecords(page)
	_ = hydrateHistory(threadID, records)
}

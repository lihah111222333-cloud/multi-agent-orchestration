package codexadapter

import (
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

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

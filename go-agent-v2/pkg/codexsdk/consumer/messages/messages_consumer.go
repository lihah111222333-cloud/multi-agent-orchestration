package messages

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	rolloutconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/rollout"
	rolloutsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/rollout"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	ThreadMessageHydrationMaxRecords = rolloutsvc.ThreadMessageHydrationMaxRecords
	ThreadMessageHydrationPageSize   = rolloutsvc.ThreadMessageHydrationPageSize
)

type ThreadHistoryMessage = rolloutconsumer.ThreadHistoryMessage

var (
	ParsePreferenceBool         = rolloutsvc.ParsePreferenceBool
	CalculateHydrationLoadLimit = rolloutsvc.CalculateHydrationLoadLimit
)

func BuildThreadMessagesResponse(messages []ThreadHistoryMessage, total int64) map[string]any {
	return map[string]any{"messages": messages, "total": total}
}

func BuildThreadMessagesPagePayload(threadID string, totalLoaded int, pages int) map[string]any {
	return map[string]any{"threadId": strings.TrimSpace(threadID), "totalCount": totalLoaded, "pages": pages}
}

func LoadAllThreadMessagesFromCodexRollout(
	ctx context.Context,
	threadID string,
	resolveRolloutHistorySource func(context.Context, string) (string, string),
	normalizeCodexThreadID func(string) string,
	showInjectedPromptInChat bool,
) ([]ThreadHistoryMessage, error) {
	return rolloutconsumer.LoadAllThreadMessagesFromCodexRollout(
		ctx,
		threadID,
		resolveRolloutHistorySource,
		normalizeCodexThreadID,
		showInjectedPromptInChat,
	)
}

func PaginateRolloutMessages(all []ThreadHistoryMessage, limit int, before int64) []ThreadHistoryMessage {
	return rolloutconsumer.PaginateRolloutMessages(all, limit, before)
}

func HistoryMessagesToRecords(msgs []ThreadHistoryMessage) []uistate.HistoryRecord {
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
	var appendPage func([]ThreadHistoryMessage)
	if appendHistory != nil {
		appendPage = func(batch []ThreadHistoryMessage) {
			appendHistory(threadID, HistoryMessagesToRecords(batch))
		}
	}

	rolloutsvc.StreamRemainingHistory(
		all,
		first,
		limit,
		pageSize,
		paginate,
		appendPage,
		func() {
			if threadDiffLen != nil {
				_ = threadDiffLen(threadID)
			}
			if threadTimelineLen != nil {
				_ = threadTimelineLen(threadID)
			}
		},
		func(totalLoaded int, pages int) {
			if notifyPage != nil {
				notifyPage(threadID, totalLoaded, pages)
			}
		},
	)
}

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

	var streamRemaining func(all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int)
	if streamRemainingHistory != nil {
		streamRemaining = func(all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int) {
			streamRemainingHistory(threadID, all, firstPage, limit)
		}
	}

	rolloutsvc.HandleThreadMessagesHydration(
		all,
		page,
		before,
		calculateHydrationLoadLimit,
		func(items []ThreadHistoryMessage) bool {
			return hydrateHistory(threadID, HistoryMessagesToRecords(items))
		},
		streamRemaining,
		asyncGo,
		func(firstPageCount int, total int, hydrated bool) {
			logger.Debug("thread/messages: first page hydrated",
				logger.FieldAgentID, threadID,
				"first_page_count", firstPageCount,
				"total", total,
				"hydrated", hydrated,
			)
		},
	)
}

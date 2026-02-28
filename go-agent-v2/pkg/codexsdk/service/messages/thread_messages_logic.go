package messages

import (
	"context"
	"os"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
	rolloutsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/rollout"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	ThreadMessageHydrationMaxRecords = rolloutsvc.ThreadMessageHydrationMaxRecords
	ThreadMessageHydrationPageSize   = rolloutsvc.ThreadMessageHydrationPageSize
)

type ThreadHistoryMessage = rolloutsvc.ThreadHistoryMessage

func BuildThreadMessagesResponse(messages []ThreadHistoryMessage, total int64) map[string]any {
	return map[string]any{"messages": messages, "total": total}
}

func BuildThreadMessagesPagePayload(threadID string, totalLoaded int, pages int) map[string]any {
	return map[string]any{"threadId": strings.TrimSpace(threadID), "totalCount": totalLoaded, "pages": pages}
}

var (
	ParsePreferenceBool         = rolloutsvc.ParsePreferenceBool
	CalculateHydrationLoadLimit = rolloutsvc.CalculateHydrationLoadLimit
	PaginateRolloutMessages     = rolloutsvc.PaginateRolloutMessages
)

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
	if appendHistory == nil {
		return
	}
	rolloutsvc.StreamRemainingHistory(
		all,
		first,
		limit,
		pageSize,
		paginate,
		func(batch []ThreadHistoryMessage) { appendHistory(threadID, batch) },
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
	hydrateHistory func(threadID string, records []ThreadHistoryMessage) bool,
	streamRemainingHistory func(threadID string, all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int),
	asyncGo func(func()),
) {
	if hydrateHistory == nil {
		return
	}
	var adaptedStream func(all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int)
	if streamRemainingHistory != nil {
		adaptedStream = func(all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int) {
			streamRemainingHistory(threadID, all, firstPage, limit)
		}
	}
	rolloutsvc.HandleThreadMessagesHydration(
		all,
		page,
		before,
		calculateHydrationLoadLimit,
		func(records []ThreadHistoryMessage) bool {
			return hydrateHistory(threadID, records)
		},
		adaptedStream,
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

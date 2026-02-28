package rollout

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	ThreadMessageHydrationMaxRecords = 20000
	ThreadMessageHydrationPageSize   = 500
)

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
	if total <= int64(initialCount) {
		return initialCount
	}
	if total > ThreadMessageHydrationMaxRecords {
		return ThreadMessageHydrationMaxRecords
	}
	return int(total)
}

func StreamRemainingHistory(
	all []ThreadHistoryMessage,
	first []ThreadHistoryMessage,
	limit int,
	pageSize int,
	paginate func([]ThreadHistoryMessage, int, int64) []ThreadHistoryMessage,
	appendPage func([]ThreadHistoryMessage),
	beforeNotify func(),
	notifyProgress func(totalLoaded int, pages int),
) {
	if appendPage == nil || len(all) == 0 || limit <= 0 || limit <= len(first) {
		return
	}
	if paginate == nil {
		paginate = PaginateRolloutMessages
	}
	if pageSize <= 0 {
		pageSize = ThreadMessageHydrationPageSize
	}

	before, pageNum := int64(0), 0
	if len(first) > 0 {
		before = first[len(first)-1].ID
		pageNum = 1
	}
	loaded := len(first)
	for loaded < limit {
		batchLimit := min(pageSize, limit-loaded)
		batch := paginate(all, batchLimit, before)
		if len(batch) == 0 {
			break
		}
		appendPage(batch)
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
	if beforeNotify != nil {
		beforeNotify()
	}
	if notifyProgress != nil {
		notifyProgress(loaded, pageNum)
	}
}

func HandleThreadMessagesHydration(
	all []ThreadHistoryMessage,
	page []ThreadHistoryMessage,
	before int64,
	calculateHydrationLoadLimit func(initialCount int, total int64) int,
	hydratePage func([]ThreadHistoryMessage) bool,
	streamRemainingHistory func(all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int),
	asyncGo func(func()),
	onFirstPageHydrated func(firstPageCount int, total int, hydrated bool),
) {
	if hydratePage == nil {
		return
	}
	if before != 0 {
		hydratePage(page)
		return
	}

	hydrated := hydratePage(page)
	if onFirstPageHydrated != nil {
		onFirstPageHydrated(len(page), len(all), hydrated)
	}
	if !hydrated || calculateHydrationLoadLimit == nil || streamRemainingHistory == nil {
		return
	}

	hydrateLimit := calculateHydrationLoadLimit(len(page), int64(len(all)))
	if hydrateLimit <= len(page) {
		return
	}
	if asyncGo == nil {
		asyncGo = util.SafeGo
	}
	asyncGo(func() {
		streamRemainingHistory(all, page, hydrateLimit)
	})
}

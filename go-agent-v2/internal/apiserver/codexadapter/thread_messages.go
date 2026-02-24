package codexadapter

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// ParseRolloutTimestamp parses rollout timestamp in RFC3339/RFC3339Nano formats.
func ParseRolloutTimestamp(raw string) time.Time {
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

// PaginateRolloutMessages selects newest-first page from rollout messages.
func PaginateRolloutMessages(all []ThreadHistoryMessage, limit int, before int64) []ThreadHistoryMessage {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(all) == 0 {
		return []ThreadHistoryMessage{}
	}
	page := make([]ThreadHistoryMessage, 0, min(limit, len(all)))
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

// ResolveRolloutHistorySourceOptions defines dependencies for rollout source lookup.
type ResolveRolloutHistorySourceOptions struct {
	ThreadID string

	GetRunningCodexThreadID func(threadID string) string
	FindBinding             func(context.Context, string) (codexThreadID string, rolloutPath string, err error)
	FindStatusSessionID     func(context.Context, string) (string, error)
	NormalizeCodexThreadID  func(string) string
}

// ResolveRolloutHistorySource resolves codex thread id and optional rollout path.
func ResolveRolloutHistorySource(ctx context.Context, opt ResolveRolloutHistorySourceOptions) (codexThreadID string, rolloutPath string) {
	id := strings.TrimSpace(opt.ThreadID)
	if id == "" {
		return "", ""
	}
	normalize := opt.NormalizeCodexThreadID
	if normalize == nil {
		normalize = strings.TrimSpace
	}

	if opt.GetRunningCodexThreadID != nil {
		candidate := normalize(opt.GetRunningCodexThreadID(id))
		if candidate != "" {
			return candidate, ""
		}
	}

	if opt.FindBinding != nil {
		boundID, path, err := opt.FindBinding(ctx, id)
		if err == nil {
			candidate := normalize(boundID)
			if candidate != "" {
				return candidate, strings.TrimSpace(path)
			}
		}
	}

	if opt.FindStatusSessionID != nil {
		sessionID, err := opt.FindStatusSessionID(ctx, id)
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
	var getRunningCodexThreadID func(string) string
	var findBinding func(context.Context, string) (string, string, error)
	var findStatusSessionID func(context.Context, string) (string, error)

	if a != nil && a.ctx != nil {
		if mgr := a.ctx.Manager(); mgr != nil {
			getRunningCodexThreadID = func(id string) string {
				proc := mgr.Get(id)
				if proc == nil || proc.Client == nil {
					return ""
				}
				return a.GetThreadID(proc)
			}
		}
		if bindingStore := a.ctx.BindingStore(); bindingStore != nil {
			findBinding = func(dbCtx context.Context, id string) (string, string, error) {
				binding, err := bindingStore.FindByAgentID(dbCtx, id)
				if err != nil {
					return "", "", err
				}
				if binding == nil {
					return "", "", nil
				}
				return binding.CodexThreadID, binding.RolloutPath, nil
			}
		}
		if statusStore := a.ctx.AgentStatusStore(); statusStore != nil {
			findStatusSessionID = func(dbCtx context.Context, id string) (string, error) {
				status, err := statusStore.Get(dbCtx, id)
				if err != nil {
					return "", err
				}
				if status == nil {
					return "", nil
				}
				return status.SessionID, nil
			}
		}
	}

	return ResolveRolloutHistorySource(ctx, ResolveRolloutHistorySourceOptions{
		ThreadID:                threadID,
		GetRunningCodexThreadID: getRunningCodexThreadID,
		FindBinding:             findBinding,
		FindStatusSessionID:     findStatusSessionID,
		NormalizeCodexThreadID:  normalizeCodexThreadID,
	})
}

// LoadAllThreadMessagesFromCodexRolloutOptions defines dependencies for rollout reads.
type LoadAllThreadMessagesFromCodexRolloutOptions struct {
	ThreadID string

	ResolveRolloutHistorySource func(context.Context, string) (string, string)
	NormalizeCodexThreadID      func(string) string
	FindRolloutPath             func(string) (string, error)
	ReadRolloutMessagesWithTrim func(path string, trimInjected bool) ([]codex.RolloutMessage, error)
	ShowInjectedPromptInChat    bool
}

// LoadAllThreadMessagesFromCodexRollout loads and normalizes rollout history.
func LoadAllThreadMessagesFromCodexRollout(ctx context.Context, opt LoadAllThreadMessagesFromCodexRolloutOptions) ([]ThreadHistoryMessage, error) {
	threadID := strings.TrimSpace(opt.ThreadID)
	if threadID == "" {
		return []ThreadHistoryMessage{}, nil
	}
	resolve := opt.ResolveRolloutHistorySource
	if resolve == nil {
		return []ThreadHistoryMessage{}, nil
	}
	normalize := opt.NormalizeCodexThreadID
	if normalize == nil {
		normalize = strings.TrimSpace
	}
	findRollout := opt.FindRolloutPath
	if findRollout == nil {
		findRollout = codex.FindRolloutPath
	}
	readRollout := opt.ReadRolloutMessagesWithTrim
	if readRollout == nil {
		readRollout = codex.ReadRolloutMessagesWithTrim
	}

	codexThreadID, rolloutPath := resolve(ctx, threadID)
	codexThreadID = normalize(codexThreadID)
	if codexThreadID == "" {
		return []ThreadHistoryMessage{}, nil
	}

	path := strings.TrimSpace(rolloutPath)
	if path == "" {
		resolvedPath, err := findRollout(codexThreadID)
		if err != nil {
			return []ThreadHistoryMessage{}, nil
		}
		path = resolvedPath
	}
	if path == "" {
		return []ThreadHistoryMessage{}, nil
	}
	if _, err := os.Stat(path); err != nil {
		return []ThreadHistoryMessage{}, nil
	}

	trimInjected := !opt.ShowInjectedPromptInChat
	rolloutMsgs, err := readRollout(path, trimInjected)
	if err != nil {
		return nil, err
	}
	if len(rolloutMsgs) == 0 {
		return []ThreadHistoryMessage{}, nil
	}

	all := make([]ThreadHistoryMessage, 0, len(rolloutMsgs))
	for i, item := range rolloutMsgs {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		createdAt := ParseRolloutTimestamp(item.Timestamp)
		eventType := ""
		if role == "assistant" {
			eventType = agentcore.EventAgentMessage
		}
		all = append(all, ThreadHistoryMessage{
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
		return []ThreadHistoryMessage{}, nil
	}
	return all, nil
}

// HistoryMessagesToRecords converts thread messages to UI history records.
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

// StreamRemainingHistoryOptions configures background history streaming.
type StreamRemainingHistoryOptions struct {
	ThreadID string
	All      []ThreadHistoryMessage
	First    []ThreadHistoryMessage
	Limit    int
	PageSize int

	Paginate          func([]ThreadHistoryMessage, int, int64) []ThreadHistoryMessage
	AppendHistory     func(string, []uistate.HistoryRecord)
	ThreadDiffLen     func(string) int
	ThreadTimelineLen func(string) int
	NotifyPage        func(threadID string, totalLoaded int, pages int)
}

// StreamRemainingHistory appends history pages beyond first page and emits one summary notification.
func StreamRemainingHistory(opt StreamRemainingHistoryOptions) {
	if opt.AppendHistory == nil || len(opt.All) == 0 || opt.Limit <= 0 || opt.Limit <= len(opt.First) {
		return
	}
	paginate := opt.Paginate
	if paginate == nil {
		paginate = PaginateRolloutMessages
	}
	pageSize := opt.PageSize
	if pageSize <= 0 {
		pageSize = 500
	}

	before := int64(0)
	if len(opt.First) > 0 {
		before = opt.First[len(opt.First)-1].ID
	}

	remaining := make([]ThreadHistoryMessage, 0, opt.Limit-len(opt.First))
	pageNum := 1
	loaded := len(opt.First)

	for loaded < opt.Limit {
		batchLimit := min(pageSize, opt.Limit-loaded)
		batch := paginate(opt.All, batchLimit, before)
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

	opt.AppendHistory(opt.ThreadID, HistoryMessagesToRecords(remaining))
	diffLen := 0
	if opt.ThreadDiffLen != nil {
		diffLen = opt.ThreadDiffLen(opt.ThreadID)
	}
	timelineLen := 0
	if opt.ThreadTimelineLen != nil {
		timelineLen = opt.ThreadTimelineLen(opt.ThreadID)
	}
	if opt.NotifyPage != nil {
		opt.NotifyPage(opt.ThreadID, loaded, pageNum)
	}
	logger.Debug("thread/messages: streaming hydration complete",
		logger.FieldAgentID, opt.ThreadID,
		"total_loaded", loaded,
		"pages", pageNum,
	)
	logger.Info("thread/messages: streaming page notified",
		logger.FieldAgentID, opt.ThreadID, logger.FieldThreadID, opt.ThreadID,
		"total_loaded", loaded,
		"pages", pageNum,
		"timeline_len", timelineLen,
		"diff_len", diffLen,
	)
}

// HandleThreadMessagesHydrationOptions defines first-page/page hydration behavior.
type HandleThreadMessagesHydrationOptions struct {
	ThreadID string
	All      []ThreadHistoryMessage
	Page     []ThreadHistoryMessage
	Before   int64

	CalculateHydrationLoadLimit func(initialCount int, total int64) int
	HydrateHistory              func(threadID string, records []uistate.HistoryRecord) bool
	StreamRemainingHistory      func(threadID string, all []ThreadHistoryMessage, firstPage []ThreadHistoryMessage, limit int)
	AsyncGo                     func(func())
}

// HandleThreadMessagesHydration hydrates first page immediately and streams the rest in background.
func HandleThreadMessagesHydration(opt HandleThreadMessagesHydrationOptions) {
	if opt.HydrateHistory == nil {
		return
	}
	total := int64(len(opt.All))
	if opt.Before == 0 {
		firstRecords := HistoryMessagesToRecords(opt.Page)
		hydrated := opt.HydrateHistory(opt.ThreadID, firstRecords)
		logger.Debug("thread/messages: first page hydrated",
			logger.FieldAgentID, opt.ThreadID,
			"first_page_count", len(opt.Page),
			"total", total,
			"hydrated", hydrated,
		)
		if hydrated && opt.CalculateHydrationLoadLimit != nil && opt.StreamRemainingHistory != nil {
			hydrateLimit := opt.CalculateHydrationLoadLimit(len(opt.Page), total)
			if hydrateLimit > len(opt.Page) {
				threadIDCopy := opt.ThreadID
				allCopy := append([]ThreadHistoryMessage(nil), opt.All...)
				firstCopy := append([]ThreadHistoryMessage(nil), opt.Page...)
				asyncGo := opt.AsyncGo
				if asyncGo == nil {
					asyncGo = util.SafeGo
				}
				asyncGo(func() {
					opt.StreamRemainingHistory(threadIDCopy, allCopy, firstCopy, hydrateLimit)
				})
			}
		}
		return
	}
	records := HistoryMessagesToRecords(opt.Page)
	_ = opt.HydrateHistory(opt.ThreadID, records)
}

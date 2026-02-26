package history

import (
	"context"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const DefaultHistoryLookupTimeout = 5 * time.Second

func EnsureContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func NormalizeHistoryTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return DefaultHistoryLookupTimeout
	}
	return timeout
}

func AppendUniqueThreadIDFallback(dst []string, seen map[string]struct{}, candidate string) []string {
	return common.AppendUniqueThreadIDFallback(dst, seen, candidate)
}

func ResolveCodexThreadCandidates(
	ctx context.Context,
	agentID string,
	timeout time.Duration,
	appendUniqueThreadID func(dst []string, seen map[string]struct{}, candidate string) []string,
	findBindingCodexThreadID func(context.Context, string) (string, error),
	findStatusSessionID func(context.Context, string) (string, error),
	previewCandidates func([]string, int) []string,
) []string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil
	}
	ctx = EnsureContext(ctx)
	timeout = NormalizeHistoryTimeout(timeout)
	appendUnique := appendUniqueThreadID
	if appendUnique == nil {
		appendUnique = AppendUniqueThreadIDFallback
	}
	preview := previewCandidates
	if preview == nil {
		preview = func(ids []string, _ int) []string { return ids }
	}
	ids := make([]string, 0, 2)
	seen := map[string]struct{}{}
	ids = appendUnique(ids, seen, id)
	if findBindingCodexThreadID != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		boundID, err := findBindingCodexThreadID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: resolve codex thread id from binding failed",
				append(common.ThreadLogFields(id), logger.FieldError, err)...,
			)
		} else {
			ids = appendUnique(ids, seen, boundID)
		}
	}
	if findStatusSessionID != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		sessionID, err := findStatusSessionID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: resolve codex thread id from agent_status failed",
				append(common.ThreadLogFields(id), logger.FieldError, err)...,
			)
		} else {
			ids = appendUnique(ids, seen, sessionID)
		}
	}
	logger.Info("turn/start: historical resume candidates",
		append(common.ThreadLogFields(id),
			"candidate_count", len(ids),
			"candidates", preview(ids, 4),
		)...,
	)
	return ids
}

func ThreadExistsInHistory(
	ctx context.Context,
	threadID string,
	timeout time.Duration,
	isLikelyCodexThreadID func(string) bool,
	findBindingByAgentID func(context.Context, string) (bool, error),
	getAgentStatusByID func(context.Context, string) (bool, error),
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	if isLikelyCodexThreadID != nil && isLikelyCodexThreadID(id) {
		return true
	}
	ctx = EnsureContext(ctx)
	timeout = NormalizeHistoryTimeout(timeout)
	if findBindingByAgentID != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		found, err := findBindingByAgentID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from agent_codex_binding failed",
				append(common.ThreadLogFields(id), logger.FieldError, err)...,
			)
		} else if found {
			return true
		}
	}
	if getAgentStatusByID != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		found, err := getAgentStatusByID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from agent_status failed",
				append(common.ThreadLogFields(id), logger.FieldError, err)...,
			)
		} else if found {
			return true
		}
	}
	if loadThreadArchiveMap != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		archivedMap, err := loadThreadArchiveMap(dbCtx)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from threadArchives.chat failed",
				append(common.ThreadLogFields(id), logger.FieldError, err)...,
			)
		} else if _, ok := archivedMap[id]; ok {
			return true
		}
	}
	return false
}

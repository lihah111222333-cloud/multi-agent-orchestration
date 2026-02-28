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
	if timeout > 0 {
		return timeout
	}
	return DefaultHistoryLookupTimeout
}

func lookupWithTimeout[T any](ctx context.Context, timeout time.Duration, lookup func(context.Context) (T, error)) (T, error) {
	var zero T
	if lookup == nil {
		return zero, nil
	}
	dbCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return lookup(dbCtx)
}

func warnHistoryLookup(agentID, message string, err error) {
	if err != nil {
		logger.Warn(message, append(common.ThreadLogFields(agentID), logger.FieldError, err)...)
	}
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
	if appendUniqueThreadID == nil {
		appendUniqueThreadID = common.AppendUniqueThreadIDFallback
	}
	if previewCandidates == nil {
		previewCandidates = func(ids []string, _ int) []string { return ids }
	}
	ids := make([]string, 0, 2)
	seen := map[string]struct{}{}
	resolveCandidate := func(lookup func(context.Context, string) (string, error), warnMsg string) {
		if lookup == nil {
			return
		}
		resolvedID, err := lookupWithTimeout(ctx, timeout, func(dbCtx context.Context) (string, error) {
			return lookup(dbCtx, id)
		})
		if err != nil {
			warnHistoryLookup(id, warnMsg, err)
			return
		}
		ids = appendUniqueThreadID(ids, seen, resolvedID)
	}
	ids = appendUniqueThreadID(ids, seen, id)
	resolveCandidate(findBindingCodexThreadID, "turn/start: resolve codex thread id from binding failed")
	resolveCandidate(findStatusSessionID, "turn/start: resolve codex thread id from agent_status failed")
	logger.Info("turn/start: historical resume candidates",
		append(common.ThreadLogFields(id),
			"candidate_count", len(ids),
			"candidates", previewCandidates(ids, 4),
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
	checkExists := func(lookup func(context.Context, string) (bool, error), warnMsg string) bool {
		if lookup == nil {
			return false
		}
		found, err := lookupWithTimeout(ctx, timeout, func(dbCtx context.Context) (bool, error) {
			return lookup(dbCtx, id)
		})
		if err != nil {
			warnHistoryLookup(id, warnMsg, err)
			return false
		}
		return found
	}
	if checkExists(findBindingByAgentID, "turn/start: check thread history from agent_codex_binding failed") {
		return true
	}
	if checkExists(getAgentStatusByID, "turn/start: check thread history from agent_status failed") {
		return true
	}
	if loadThreadArchiveMap != nil {
		archivedMap, err := lookupWithTimeout(ctx, timeout, loadThreadArchiveMap)
		if err != nil {
			warnHistoryLookup(id, "turn/start: check thread history from threadArchives.chat failed", err)
		} else if _, ok := archivedMap[id]; ok {
			return true
		}
	}
	return false
}

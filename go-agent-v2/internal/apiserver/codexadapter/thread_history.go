package codexadapter

import (
	"context"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const defaultHistoryLookupTimeout = 5 * time.Second

func ensureContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func normalizeHistoryTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultHistoryLookupTimeout
	}
	return timeout
}

func appendUniqueThreadIDFallback(dst []string, seen map[string]struct{}, candidate string) []string {
	id := strings.TrimSpace(candidate)
	if id == "" {
		return dst
	}
	if seen == nil {
		seen = map[string]struct{}{}
	}
	if _, ok := seen[id]; ok {
		return dst
	}
	seen[id] = struct{}{}
	return append(dst, id)
}

// resolveCodexThreadCandidates resolves ordered codex thread candidates from stores.
func resolveCodexThreadCandidates(
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
	ctx = ensureContext(ctx)
	timeout = normalizeHistoryTimeout(timeout)

	appendUnique := appendUniqueThreadID
	if appendUnique == nil {
		appendUnique = appendUniqueThreadIDFallback
	}
	preview := previewCandidates
	if preview == nil {
		preview = PreviewResumeCandidates
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
				append(threadLogFields(id), logger.FieldError, err)...,
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
				append(threadLogFields(id), logger.FieldError, err)...,
			)
		} else {
			ids = appendUnique(ids, seen, sessionID)
		}
	}

	logger.Info("turn/start: historical resume candidates",
		append(threadLogFields(id),
			"candidate_count", len(ids),
			"candidates", preview(ids, 4),
		)...,
	)
	return ids
}

// ResolveCodexThreadCandidates resolves candidate codex thread IDs via adapter context stores.
func (a *Adapter) ResolveCodexThreadCandidates(
	ctx context.Context,
	agentID string,
	appendUniqueThreadID func(dst []string, seen map[string]struct{}, candidate string) []string,
	previewCandidates func([]string, int) []string,
) []string {
	return resolveCodexThreadCandidates(
		ctx,
		agentID,
		0,
		appendUniqueThreadID,
		a.bindingCodexThreadIDByAgentID,
		a.statusSessionIDByAgentID,
		previewCandidates,
	)
}

// threadExistsInHistory checks whether a thread exists in runtime history sources.
func threadExistsInHistory(
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
	ctx = ensureContext(ctx)
	timeout = normalizeHistoryTimeout(timeout)

	if findBindingByAgentID != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		found, err := findBindingByAgentID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from agent_codex_binding failed",
				append(threadLogFields(id), logger.FieldError, err)...,
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
				append(threadLogFields(id), logger.FieldError, err)...,
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
				append(threadLogFields(id), logger.FieldError, err)...,
			)
		} else if _, ok := archivedMap[id]; ok {
			return true
		}
	}

	return false
}

// ThreadExistsInHistory checks whether a thread exists in historical sources via adapter context stores.
func (a *Adapter) ThreadExistsInHistory(ctx context.Context, threadID string) bool {
	return threadExistsInHistory(
		ctx,
		threadID,
		0,
		isLikelyCodexThreadID,
		a.bindingExistsByAgentID,
		a.agentStatusExistsByID,
		a.loadThreadArchiveMap,
	)
}

func (a *Adapter) bindingExistsByAgentID(ctx context.Context, agentID string) (bool, error) {
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return false, nil
	}
	binding, err := bindingStore.FindByAgentID(ctx, agentID)
	if err != nil {
		return false, err
	}
	return binding != nil, nil
}

func (a *Adapter) agentStatusExistsByID(ctx context.Context, agentID string) (bool, error) {
	statusStore := a.statusStore()
	if statusStore == nil {
		return false, nil
	}
	status, err := statusStore.Get(ctx, agentID)
	if err != nil {
		return false, err
	}
	return status != nil, nil
}

func (a *Adapter) bindingCodexThreadIDByAgentID(ctx context.Context, agentID string) (string, error) {
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return "", nil
	}
	binding, err := bindingStore.FindByAgentID(ctx, agentID)
	if err != nil {
		return "", err
	}
	if binding == nil {
		return "", nil
	}
	return binding.CodexThreadID, nil
}

func (a *Adapter) statusSessionIDByAgentID(ctx context.Context, agentID string) (string, error) {
	statusStore := a.statusStore()
	if statusStore == nil {
		return "", nil
	}
	status, err := statusStore.Get(ctx, agentID)
	if err != nil {
		return "", err
	}
	if status == nil {
		return "", nil
	}
	return status.SessionID, nil
}

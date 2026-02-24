package codexadapter

import (
	"context"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const defaultHistoryLookupTimeout = 3 * time.Second

// ThreadExistsInHistoryOptions configures history-existence checks.
type ThreadExistsInHistoryOptions struct {
	ThreadID              string
	Timeout               time.Duration
	IsLikelyCodexThreadID func(string) bool
	FindBindingByAgentID  func(context.Context, string) (bool, error)
	GetAgentStatusByID    func(context.Context, string) (bool, error)
	LoadThreadArchiveMap  func(context.Context) (map[string]int64, error)
}

// ThreadExistsInHistory checks whether a thread exists in runtime history sources.
func ThreadExistsInHistory(ctx context.Context, opt ThreadExistsInHistoryOptions) bool {
	id := strings.TrimSpace(opt.ThreadID)
	if id == "" {
		return false
	}
	if opt.IsLikelyCodexThreadID != nil && opt.IsLikelyCodexThreadID(id) {
		return true
	}
	ctx = ensureContext(ctx)
	timeout := normalizeHistoryTimeout(opt.Timeout)

	if opt.FindBindingByAgentID != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		found, err := opt.FindBindingByAgentID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from agent_codex_binding failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if found {
			return true
		}
	}

	if opt.GetAgentStatusByID != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		found, err := opt.GetAgentStatusByID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from agent_status failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if found {
			return true
		}
	}

	if opt.LoadThreadArchiveMap != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		archivedMap, err := opt.LoadThreadArchiveMap(dbCtx)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from threadArchives.chat failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if _, ok := archivedMap[id]; ok {
			return true
		}
	}

	return false
}

// ThreadExistsInHistory checks whether a thread exists in historical sources via adapter context stores.
func (a *Adapter) ThreadExistsInHistory(
	ctx context.Context,
	threadID string,
	isLikelyCodexThreadID func(string) bool,
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) bool {
	var findBindingByAgentID func(context.Context, string) (bool, error)
	var getAgentStatusByID func(context.Context, string) (bool, error)
	if a != nil && a.ctx != nil {
		if bindingStore := a.ctx.BindingStore(); bindingStore != nil {
			findBindingByAgentID = func(dbCtx context.Context, agentID string) (bool, error) {
				binding, err := bindingStore.FindByAgentID(dbCtx, agentID)
				if err != nil {
					return false, err
				}
				return binding != nil, nil
			}
		}
		if statusStore := a.ctx.AgentStatusStore(); statusStore != nil {
			getAgentStatusByID = func(dbCtx context.Context, agentID string) (bool, error) {
				status, err := statusStore.Get(dbCtx, agentID)
				if err != nil {
					return false, err
				}
				return status != nil, nil
			}
		}
	}
	return ThreadExistsInHistory(ctx, ThreadExistsInHistoryOptions{
		ThreadID:              threadID,
		IsLikelyCodexThreadID: isLikelyCodexThreadID,
		FindBindingByAgentID:  findBindingByAgentID,
		GetAgentStatusByID:    getAgentStatusByID,
		LoadThreadArchiveMap:  loadThreadArchiveMap,
	})
}

// ResolveCodexThreadCandidatesOptions configures codex-thread candidate resolution.
type ResolveCodexThreadCandidatesOptions struct {
	AgentID                  string
	Timeout                  time.Duration
	AppendUniqueThreadID     func(dst []string, seen map[string]struct{}, candidate string) []string
	FindBindingCodexThreadID func(context.Context, string) (string, error)
	FindStatusSessionID      func(context.Context, string) (string, error)
	PreviewCandidates        func([]string, int) []string
}

// ResolveCodexThreadCandidates resolves ordered codex thread candidates from stores.
func ResolveCodexThreadCandidates(ctx context.Context, opt ResolveCodexThreadCandidatesOptions) []string {
	id := strings.TrimSpace(opt.AgentID)
	if id == "" {
		return nil
	}
	ctx = ensureContext(ctx)
	timeout := normalizeHistoryTimeout(opt.Timeout)

	appendUnique := opt.AppendUniqueThreadID
	if appendUnique == nil {
		appendUnique = appendUniqueThreadIDFallback
	}
	preview := opt.PreviewCandidates
	if preview == nil {
		preview = PreviewResumeCandidates
	}

	ids := make([]string, 0, 2)
	seen := map[string]struct{}{}
	ids = appendUnique(ids, seen, id)

	if opt.FindBindingCodexThreadID != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		boundID, err := opt.FindBindingCodexThreadID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: resolve codex thread id from binding failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else {
			ids = appendUnique(ids, seen, boundID)
		}
	}

	if opt.FindStatusSessionID != nil {
		dbCtx, cancel := context.WithTimeout(ctx, timeout)
		sessionID, err := opt.FindStatusSessionID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: resolve codex thread id from agent_status failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else {
			ids = appendUnique(ids, seen, sessionID)
		}
	}

	logger.Info("turn/start: historical resume candidates",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"candidate_count", len(ids),
		"candidates", preview(ids, 4),
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
	var findBindingCodexThreadID func(context.Context, string) (string, error)
	var findStatusSessionID func(context.Context, string) (string, error)
	if a != nil && a.ctx != nil {
		if bindingStore := a.ctx.BindingStore(); bindingStore != nil {
			findBindingCodexThreadID = func(dbCtx context.Context, id string) (string, error) {
				binding, err := bindingStore.FindByAgentID(dbCtx, id)
				if err != nil {
					return "", err
				}
				if binding == nil {
					return "", nil
				}
				return binding.CodexThreadID, nil
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
	return ResolveCodexThreadCandidates(ctx, ResolveCodexThreadCandidatesOptions{
		AgentID:                  agentID,
		AppendUniqueThreadID:     appendUniqueThreadID,
		FindBindingCodexThreadID: findBindingCodexThreadID,
		FindStatusSessionID:      findStatusSessionID,
		PreviewCandidates:        previewCandidates,
	})
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func normalizeHistoryTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultHistoryLookupTimeout
	}
	return timeout
}

func appendUniqueThreadIDFallback(dst []string, seen map[string]struct{}, candidate string) []string {
	value := strings.TrimSpace(candidate)
	if value == "" {
		return dst
	}
	if _, ok := seen[value]; ok {
		return dst
	}
	seen[value] = struct{}{}
	return append(dst, value)
}

package codexadapter

import (
	"context"
	"strings"
	"time"
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

// ResolveRolloutHistorySource resolves codex thread id and optional rollout path.
func ResolveRolloutHistorySource(
	ctx context.Context,
	threadID string,
	getRunningCodexThreadID func(threadID string) string,
	findBinding func(context.Context, string) (codexThreadID string, rolloutPath string, err error),
	findStatusSessionID func(context.Context, string) (string, error),
	normalizeCodexThreadID func(string) string,
) (codexThreadID string, rolloutPath string) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", ""
	}
	normalize := normalizeCodexThreadID
	if normalize == nil {
		normalize = strings.TrimSpace
	}

	if getRunningCodexThreadID != nil {
		candidate := normalize(getRunningCodexThreadID(id))
		if candidate != "" {
			return candidate, ""
		}
	}

	if findBinding != nil {
		boundID, path, err := findBinding(ctx, id)
		if err == nil {
			candidate := normalize(boundID)
			if candidate != "" {
				return candidate, strings.TrimSpace(path)
			}
		}
	}

	if findStatusSessionID != nil {
		sessionID, err := findStatusSessionID(ctx, id)
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
	return ResolveRolloutHistorySource(
		ctx,
		threadID,
		a.runningCodexThreadID,
		a.bindingRolloutSourceByAgentID,
		a.statusSessionIDByAgentID,
		normalizeCodexThreadID,
	)
}

func (a *Adapter) runningCodexThreadID(threadID string) string {
	if a == nil || a.ctx == nil || a.ctx.Manager == nil {
		return ""
	}
	proc := a.ctx.Manager.Get(threadID)
	if proc == nil || proc.Client == nil {
		return ""
	}
	return a.GetThreadID(proc)
}

func (a *Adapter) bindingRolloutSourceByAgentID(ctx context.Context, agentID string) (string, string, error) {
	if a == nil || a.ctx == nil || a.ctx.BindingStore == nil {
		return "", "", nil
	}
	binding, err := a.ctx.BindingStore.FindByAgentID(ctx, agentID)
	if err != nil {
		return "", "", err
	}
	if binding == nil {
		return "", "", nil
	}
	return binding.CodexThreadID, binding.RolloutPath, nil
}

func (a *Adapter) resolveRolloutHistorySource(ctx context.Context, threadID string) (string, string) {
	return a.ResolveRolloutHistorySource(ctx, threadID, NormalizeCodexThreadID)
}

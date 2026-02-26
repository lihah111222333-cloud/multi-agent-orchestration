package codexadapter

import (
	"context"
	"strings"
	"time"
)

// parseRolloutTimestamp parses rollout timestamp in RFC3339/RFC3339Nano formats.
func parseRolloutTimestamp(raw string) time.Time {
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

// paginateRolloutMessages selects newest-first page from rollout messages.
func paginateRolloutMessages(all []threadHistoryMessage, limit int, before int64) []threadHistoryMessage {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(all) == 0 {
		return []threadHistoryMessage{}
	}
	page := make([]threadHistoryMessage, 0, min(limit, len(all)))
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

// resolveRolloutHistorySource resolves codex thread id and optional rollout path.
func resolveRolloutHistorySource(
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

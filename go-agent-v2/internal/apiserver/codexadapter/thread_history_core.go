package codexadapter

import (
	"context"
	"strings"
	"time"
)

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

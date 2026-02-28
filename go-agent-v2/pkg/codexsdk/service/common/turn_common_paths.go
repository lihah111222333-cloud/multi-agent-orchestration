package common

import (
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func CollectTrimmedUniqueValues(values []string, keyFn func(string) string) []string {
	if len(values) == 0 { return nil }
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" { continue }
		key := value
		if keyFn != nil {
			if key = strings.TrimSpace(keyFn(value)); key == "" { key = value }
		}
		if _, ok := seen[key]; ok { continue }
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 { return nil }
	return out
}

func RequireThreadID(caller, threadID string) (string, error) {
	id := strings.TrimSpace(threadID)
	if id == "" { return "", apperrors.New(caller, "threadId is required") }
	return id, nil
}

func ThreadLogFields(threadID string) []any {
	id := strings.TrimSpace(threadID)
	return []any{logger.FieldAgentID, id, logger.FieldThreadID, id}
}

func AppendUniqueThreadIDFallback(dst []string, seen map[string]struct{}, candidate string) []string {
	id := strings.TrimSpace(candidate)
	if id == "" { return dst }
	if seen != nil {
		if _, ok := seen[id]; ok { return dst }
		seen[id] = struct{}{}
	}
	return append(dst, id)
}

package codexadapter

import (
	"fmt"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// BuildResumeCandidates builds ordered resume candidates from thread id and resolved ids.
func BuildResumeCandidates(threadID string, resolved []string, normalize func(string) string) []string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	if normalize != nil {
		if normalized := normalize(id); normalized != "" {
			return []string{normalized}
		}
	}
	candidates := make([]string, 0, len(resolved))
	seen := map[string]struct{}{}
	for _, candidate := range resolved {
		value := strings.TrimSpace(candidate)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}
	if len(candidates) > 0 {
		return candidates
	}
	return []string{id}
}

// TryResumeCandidates attempts each candidate in order and returns first success.
func TryResumeCandidates(
	candidates []string,
	fallbackID string,
	resumeFn func(string) error,
	isCandidateError func(error) bool,
) (string, error) {
	if len(candidates) == 0 {
		logger.Warn("thread/resume: no resume candidates available",
			logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
			"reason", "no codex thread ID resolved from history",
		)
		return "", apperrors.Newf("tryResumeCandidates", "no resume candidates available for thread %s", fallbackID)
	}
	if isCandidateError == nil {
		isCandidateError = IsHistoricalResumeCandidateError
	}

	var lastErr error
	for _, id := range candidates {
		err := resumeFn(id)
		if err == nil {
			return id, nil
		}
		lastErr = err
		if isCandidateError(err) {
			logger.Warn("thread/resume: candidate unavailable, trying next",
				logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
				"resume_thread_id", id,
				logger.FieldError, err,
			)
			continue
		}
		return "", err
	}

	logger.Warn("thread/resume: all resume candidates exhausted",
		logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
		"candidate_count", len(candidates),
		"last_error", lastErr,
		"reason", "all historical rollouts unavailable",
	)
	if lastErr != nil {
		return "", apperrors.Wrapf(lastErr, "tryResumeCandidates", "all resume candidates unavailable for thread %s after %d attempts", fallbackID, len(candidates))
	}
	return "", apperrors.Newf("tryResumeCandidates", "all resume candidates unavailable for thread %s after %d attempts", fallbackID, len(candidates))
}

// IsHistoricalResumeCandidateError determines whether error means a candidate can be skipped.
func IsHistoricalResumeCandidateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no rollout found for thread id"):
		return true
	case strings.Contains(msg, "failed to load rollout"):
		return true
	case strings.Contains(msg, "thread/resume returned empty thread id"):
		return true
	case strings.Contains(msg, "thread/resume returned empty response without fallback thread id"):
		return true
	case strings.Contains(msg, "websocket: close 1006"):
		return true
	case strings.Contains(msg, "abnormal closure"):
		return true
	case strings.Contains(msg, "history not found"):
		return true
	case strings.Contains(msg, "already at oldest turn"):
		return true
	case strings.Contains(msg, "rollout file missing"):
		return true
	case strings.Contains(msg, "session file not found"):
		return true
	case strings.Contains(msg, "invalid thread id"):
		return true
	default:
		return false
	}
}

// IsCodexProcessCrashError determines whether error indicates codex process crash.
func IsCodexProcessCrashError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "websocket: close 1006") ||
		strings.Contains(msg, "abnormal closure")
}

// PreviewResumeCandidates returns a shortened candidate preview for logs.
func PreviewResumeCandidates(candidates []string, limit int) []string {
	if len(candidates) == 0 {
		return nil
	}
	if limit <= 0 || len(candidates) <= limit {
		return append([]string(nil), candidates...)
	}
	out := append([]string(nil), candidates[:limit]...)
	out = append(out, fmt.Sprintf("...+%d more", len(candidates)-limit))
	return out
}

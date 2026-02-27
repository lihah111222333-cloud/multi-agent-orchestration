package lifecycle

import (
	"fmt"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

var (
	codexProcessCrashMarkers         = strings.Split("websocket: close 1006|abnormal closure", "|")
	historicalResumeCandidateMarkers = strings.Split("no rollout found for thread id|failed to load rollout|thread/resume returned empty thread id|thread/resume returned empty response without fallback thread id|history not found|already at oldest turn|rollout file missing|session file not found|invalid thread id", "|")
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
	if candidates := common.CollectTrimmedUniqueValues(resolved, nil); len(candidates) > 0 {
		return candidates
	}
	return []string{id}
}

// TryResumeCandidates attempts each candidate in order and returns first success.
func TryResumeCandidates(candidates []string, fallbackID string, resumeFn func(string) error, isCandidateError func(error) bool) (string, error) {
	if len(candidates) == 0 {
		logger.Warn("thread/resume: no resume candidates available",
			append(common.ThreadLogFields(fallbackID), "reason", "no codex thread ID resolved from history")...,
		)
		return "", apperrors.Newf("tryResumeCandidates", "no resume candidates available for thread %s", fallbackID)
	}
	if isCandidateError == nil {
		isCandidateError = IsHistoricalResumeCandidateError
	}

	var lastErr error
	for _, id := range candidates {
		if err := resumeFn(id); err != nil {
			if !isCandidateError(err) {
				return "", err
			}
			lastErr = err
			logger.Warn("thread/resume: candidate unavailable, trying next",
				append(common.ThreadLogFields(fallbackID), "resume_thread_id", id, logger.FieldError, err)...,
			)
			continue
		}
		return id, nil
	}

	logger.Warn("thread/resume: all resume candidates exhausted",
		append(common.ThreadLogFields(fallbackID), "candidate_count", len(candidates), "last_error", lastErr, "reason", "all historical rollouts unavailable")...,
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
	return containsAnyMarker(msg, historicalResumeCandidateMarkers) || containsAnyMarker(msg, codexProcessCrashMarkers)
}

// IsCodexProcessCrashError determines whether error indicates codex process crash.
func IsCodexProcessCrashError(err error) bool {
	return err != nil && containsAnyMarker(strings.ToLower(err.Error()), codexProcessCrashMarkers)
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
	return append(out, fmt.Sprintf("...+%d more", len(candidates)-limit))
}

func containsAnyMarker(msg string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

package dashboard

import (
	"strings"
	"time"
)

func NormalizeDurationOrDefault(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func PruneOrchestrationPendingReports(pending map[string]map[string]time.Time, now time.Time, ttl time.Duration) {
	if len(pending) == 0 || ttl <= 0 {
		return
	}
	cutoff := now.Add(-ttl)
	for workerID, waiters := range pending {
		for requesterID, createdAt := range waiters {
			if createdAt.Before(cutoff) {
				delete(waiters, requesterID)
			}
		}
		if len(waiters) == 0 {
			delete(pending, workerID)
		}
	}
}

func RememberOrchestrationRequester(pending map[string]map[string]time.Time, workerID, requesterID string, now time.Time) int {
	target, requester := strings.TrimSpace(workerID), strings.TrimSpace(requesterID)
	if pending == nil || target == "" || requester == "" {
		return 0
	}
	waiters := pending[target]
	if waiters == nil {
		waiters = make(map[string]time.Time)
		pending[target] = waiters
	}
	waiters[requester] = now
	return len(waiters)
}

func TakeOrchestrationRequesters(pending map[string]map[string]time.Time, workerID string) []string {
	target := strings.TrimSpace(workerID)
	if pending == nil || target == "" {
		return nil
	}
	waiters := pending[target]
	if len(waiters) == 0 {
		return nil
	}
	delete(pending, target)
	requesters := make([]string, 0, len(waiters))
	for requesterID := range waiters {
		if requesterID = strings.TrimSpace(requesterID); requesterID != "" {
			requesters = append(requesters, requesterID)
		}
	}
	return requesters
}

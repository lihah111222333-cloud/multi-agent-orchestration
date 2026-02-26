package dashboard

import (
	"strings"
	"time"
)

// NormalizeDurationOrDefault returns fallback when value is not positive.
func NormalizeDurationOrDefault(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

// PruneOrchestrationPendingReports removes expired requester entries in place.
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

// RememberOrchestrationRequester records one requester and returns watcher count.
func RememberOrchestrationRequester(pending map[string]map[string]time.Time, workerID, requesterID string, now time.Time) int {
	if pending == nil {
		return 0
	}
	target := strings.TrimSpace(workerID)
	requester := strings.TrimSpace(requesterID)
	if target == "" || requester == "" {
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

// TakeOrchestrationRequesters removes and returns requesters for one worker.
func TakeOrchestrationRequesters(pending map[string]map[string]time.Time, workerID string) []string {
	if pending == nil {
		return nil
	}
	target := strings.TrimSpace(workerID)
	if target == "" {
		return nil
	}
	waiters := pending[target]
	if len(waiters) == 0 {
		return nil
	}
	delete(pending, target)
	requesters := make([]string, 0, len(waiters))
	for requesterID := range waiters {
		id := strings.TrimSpace(requesterID)
		if id != "" {
			requesters = append(requesters, id)
		}
	}
	return requesters
}

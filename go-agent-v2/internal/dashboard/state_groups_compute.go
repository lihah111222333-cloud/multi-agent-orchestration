package dashboard

import (
	"strings"
	"time"
)

func NormalizeDurationOrDefault(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func PruneOrchestrationPendingReports(pending map[string]map[string]time.Time, now time.Time, ttl time.Duration) {
	if len(pending) == 0 || ttl <= 0 {
		return
	}
	for workerID, waiters := range pending {
		for requesterID, createdAt := range waiters {
			if createdAt.Before(now.Add(-ttl)) {
				delete(waiters, requesterID)
			}
		}
		if len(waiters) == 0 {
			delete(pending, workerID)
		}
	}
}

func RememberOrchestrationRequester(pending map[string]map[string]time.Time, workerID, requesterID string, now time.Time) int {
	workerID, requesterID = strings.TrimSpace(workerID), strings.TrimSpace(requesterID)
	if pending == nil || workerID == "" || requesterID == "" {
		return 0
	}
	waiters := pending[workerID]
	if waiters == nil {
		waiters = make(map[string]time.Time)
		pending[workerID] = waiters
	}
	waiters[requesterID] = now
	return len(waiters)
}

func TakeOrchestrationRequesters(pending map[string]map[string]time.Time, workerID string) []string {
	workerID = strings.TrimSpace(workerID)
	if pending == nil || workerID == "" {
		return nil
	}
	waiters := pending[workerID]
	if len(waiters) == 0 {
		return nil
	}
	delete(pending, workerID)
	requesters := make([]string, 0, len(waiters))
	for requesterID := range waiters {
		if requesterID = strings.TrimSpace(requesterID); requesterID != "" {
			requesters = append(requesters, requesterID)
		}
	}
	return requesters
}

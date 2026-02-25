package codexadapter

import (
	"maps"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrackedTurnSummaryFromPayload extracts last agent summary from event payload.
func TrackedTurnSummaryFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if summary := ExtractTrackedString(payload, "lastAgentMessage", "last_agent_message"); summary != "" {
		return summary
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if summary := ExtractTrackedString(turn, "lastAgentMessage", "last_agent_message"); summary != "" {
			return summary
		}
	}
	if msg, ok := payload["msg"].(map[string]any); ok {
		if summary := ExtractTrackedString(msg, "lastAgentMessage", "last_agent_message"); summary != "" {
			return summary
		}
	}
	return ""
}

// TrackedTurnSummaryCacheKey returns summary cache key for thread + optional turn.
func TrackedTurnSummaryCacheKey(threadID, turnID string) string {
	return strings.TrimSpace(threadID) + "\x00" + strings.TrimSpace(turnID)
}

// PruneTrackedTurnSummaryCacheLocked removes expired and overflow entries.
func PruneTrackedTurnSummaryCacheLocked(cache map[string]TrackedTurnSummaryCacheEntry, ttl time.Duration, now time.Time) {
	if len(cache) == 0 {
		return
	}
	if ttl <= 0 {
		ttl = DefaultTrackedTurnSummaryTTL
	}

	cutoff := now.Add(-ttl)
	for key, entry := range cache {
		if entry.UpdatedAt.Before(cutoff) {
			delete(cache, key)
		}
	}

	if len(cache) <= TrackedTurnSummaryCacheMaxEntries {
		return
	}

	type summaryCacheKV struct {
		key       string
		updatedAt time.Time
	}
	entries := make([]summaryCacheKV, 0, len(cache))
	for key, entry := range cache {
		entries = append(entries, summaryCacheKV{key: key, updatedAt: entry.UpdatedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].updatedAt.Before(entries[j].updatedAt)
	})

	trimCount := len(cache) - TrackedTurnSummaryCacheMaxEntries
	for i := 0; i < trimCount && i < len(entries); i++ {
		delete(cache, entries[i].key)
	}
}

// RememberTrackedTurnSummary records the latest non-empty summary for thread/turn.
func RememberTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID, summary string) {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	msg := strings.TrimSpace(summary)
	if id == "" || msg == "" || turnMu == nil {
		return
	}

	now := time.Now()

	turnMu.Lock()
	defer turnMu.Unlock()
	EnsureTurnTrackerStateLocked(state)
	if state.TurnSummaryCache == nil || *state.TurnSummaryCache == nil {
		return
	}

	ttl := DefaultTrackedTurnSummaryTTL
	if state.TurnSummaryTTL != nil {
		ttl = *state.TurnSummaryTTL
	}
	cache := *state.TurnSummaryCache
	PruneTrackedTurnSummaryCacheLocked(cache, ttl, now)
	entry := TrackedTurnSummaryCacheEntry{
		TurnID:    tid,
		Summary:   msg,
		UpdatedAt: now,
	}
	cache[TrackedTurnSummaryCacheKey(id, "")] = entry
	if tid != "" {
		cache[TrackedTurnSummaryCacheKey(id, tid)] = entry
	}
}

// LookupTrackedTurnSummary finds a cached summary by thread + optional turn.
func LookupTrackedTurnSummary(state TurnTrackerState, turnMu *sync.Mutex, threadID, turnID string) string {
	id := strings.TrimSpace(threadID)
	tid := strings.TrimSpace(turnID)
	if id == "" || turnMu == nil {
		return ""
	}

	now := time.Now()

	turnMu.Lock()
	defer turnMu.Unlock()
	EnsureTurnTrackerStateLocked(state)
	if state.TurnSummaryCache == nil || *state.TurnSummaryCache == nil {
		return ""
	}

	ttl := DefaultTrackedTurnSummaryTTL
	if state.TurnSummaryTTL != nil {
		ttl = *state.TurnSummaryTTL
	}
	cache := *state.TurnSummaryCache
	PruneTrackedTurnSummaryCacheLocked(cache, ttl, now)

	if tid != "" {
		if entry, ok := cache[TrackedTurnSummaryCacheKey(id, tid)]; ok {
			return strings.TrimSpace(entry.Summary)
		}
	}
	if entry, ok := cache[TrackedTurnSummaryCacheKey(id, "")]; ok {
		entryTurnID := strings.TrimSpace(entry.TurnID)
		if tid != "" && entryTurnID != "" && !strings.EqualFold(tid, entryTurnID) {
			return ""
		}
		return strings.TrimSpace(entry.Summary)
	}
	return ""
}

// InjectTrackedTurnSummary writes summary into top-level and turn object payload.
func InjectTrackedTurnSummary(payload map[string]any, summary string) {
	if payload == nil {
		return
	}
	msg := strings.TrimSpace(summary)
	if msg == "" {
		return
	}

	payload["lastAgentMessage"] = msg

	turnPayload, _ := payload["turn"].(map[string]any)
	if turnPayload == nil {
		turnPayload = make(map[string]any)
	}
	turnPayload["lastAgentMessage"] = msg
	payload["turn"] = turnPayload
}

// MergeTrackedTurnCompletionPayload merges completion payload into original event payload.
func MergeTrackedTurnCompletionPayload(payload, completion map[string]any) {
	if payload == nil || completion == nil {
		return
	}
	for key, value := range completion {
		if key != "turn" {
			payload[key] = value
			continue
		}
		completionTurn, ok := value.(map[string]any)
		if !ok {
			payload[key] = value
			continue
		}
		currentTurn, ok := payload[key].(map[string]any)
		if !ok || currentTurn == nil {
			currentTurn = make(map[string]any, len(completionTurn))
		}
		maps.Copy(currentTurn, completionTurn)
		payload[key] = currentTurn
	}
}

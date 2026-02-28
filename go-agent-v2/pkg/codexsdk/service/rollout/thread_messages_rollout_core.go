package rollout

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
)

type ThreadHistoryMessage struct {
	ID        int64           `json:"id"`
	AgentID   string          `json:"agentId"`
	Role      string          `json:"role"`
	EventType string          `json:"eventType"`
	Method    string          `json:"method"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

func LoadAllThreadMessagesFromCodexRollout(
	ctx context.Context,
	threadID string,
	resolveRolloutHistorySource func(context.Context, string) (string, string),
	normalizeCodexThreadID func(string) string,
	findRolloutPath func(string) (string, error),
	rolloutPathExists func(string) bool,
	readRolloutMessagesWithTrim func(path string, trimInjected bool) ([]codex.RolloutMessage, error),
	showInjectedPromptInChat bool,
) ([]ThreadHistoryMessage, error) {
	empty := []ThreadHistoryMessage{}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || resolveRolloutHistorySource == nil { return empty, nil }
	if normalizeCodexThreadID == nil { normalizeCodexThreadID = strings.TrimSpace }
	codexThreadID, rolloutPath := resolveRolloutHistorySource(ctx, threadID)
	codexThreadID = normalizeCodexThreadID(codexThreadID)
	if codexThreadID == "" { return empty, nil }
	path := strings.TrimSpace(rolloutPath)
	if path == "" {
		if findRolloutPath == nil { return empty, nil }
		resolvedPath, err := findRolloutPath(codexThreadID)
		if err != nil { return empty, nil }
		path = strings.TrimSpace(resolvedPath)
	}
	if path == "" || (rolloutPathExists != nil && !rolloutPathExists(path)) || readRolloutMessagesWithTrim == nil {
		return empty, nil
	}
	rolloutMsgs, err := readRolloutMessagesWithTrim(path, !showInjectedPromptInChat)
	if err != nil {
		return nil, err
	}
	all := make([]ThreadHistoryMessage, 0, len(rolloutMsgs))
	for i, item := range rolloutMsgs {
		message, ok := rolloutMessageToThreadHistory(threadID, i, item)
		if !ok {
			continue
		}
		all = append(all, message)
	}
	return all, nil
}

func rolloutMessageToThreadHistory(threadID string, index int, item codex.RolloutMessage) (ThreadHistoryMessage, bool) {
	role := strings.ToLower(strings.TrimSpace(item.Role))
	if role != "user" && role != "assistant" {
		return ThreadHistoryMessage{}, false
	}
	eventType := ""
	if role == "assistant" {
		eventType = agentcore.EventAgentMessage
	}
	return ThreadHistoryMessage{
		ID:        int64(index + 1),
		AgentID:   threadID,
		Role:      role,
		EventType: eventType,
		Content:   item.Content,
		CreatedAt: ParseRolloutTimestamp(item.Timestamp),
	}, true
}

func ParseRolloutTimestamp(raw string) time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range [...]string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts
		}
	}
	return time.Time{}
}

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

func RunningCodexThreadIDFromManager(
	threadID string,
	getProcess func(string) any,
	getThreadID func(any) string,
) string {
	if getProcess == nil || getThreadID == nil { return "" }
	if proc := getProcess(threadID); proc != nil { return getThreadID(proc) }
	return ""
}

func ResolveRolloutHistorySource(
	ctx context.Context,
	threadID string,
	getRunningCodexThreadID func(threadID string) string,
	findBinding func(context.Context, string) (codexThreadID string, rolloutPath string, err error),
	findStatusSessionID func(context.Context, string) (string, error),
	normalizeCodexThreadID func(string) string,
) (codexThreadID string, rolloutPath string) {
	id := strings.TrimSpace(threadID)
	if id == "" { return "", "" }
	if normalizeCodexThreadID == nil { normalizeCodexThreadID = strings.TrimSpace }
	if getRunningCodexThreadID != nil {
		candidate := normalizeCodexThreadID(getRunningCodexThreadID(id))
		if candidate != "" { return candidate, "" }
	}
	if findBinding != nil {
		if boundID, path, err := findBinding(ctx, id); err == nil {
			candidate := normalizeCodexThreadID(boundID)
			if candidate != "" { return candidate, strings.TrimSpace(path) }
		}
	}
	if findStatusSessionID != nil {
		if sessionID, err := findStatusSessionID(ctx, id); err == nil {
			candidate := normalizeCodexThreadID(sessionID)
			if candidate != "" { return candidate, "" }
		}
	}
	return normalizeCodexThreadID(id), ""
}

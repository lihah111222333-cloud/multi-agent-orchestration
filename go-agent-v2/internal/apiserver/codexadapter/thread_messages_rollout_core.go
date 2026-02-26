package codexadapter

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
)

// loadAllThreadMessagesFromCodexRollout loads and normalizes rollout history.
func loadAllThreadMessagesFromCodexRollout(
	ctx context.Context,
	threadID string,
	resolveRolloutHistorySource func(context.Context, string) (string, string),
	normalizeCodexThreadID func(string) string,
	findRolloutPath func(string) (string, error),
	readRolloutMessagesWithTrim func(path string, trimInjected bool) ([]codex.RolloutMessage, error),
	showInjectedPromptInChat bool,
) ([]threadHistoryMessage, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return []threadHistoryMessage{}, nil
	}
	resolve := resolveRolloutHistorySource
	if resolve == nil {
		return []threadHistoryMessage{}, nil
	}
	normalize := normalizeCodexThreadID
	if normalize == nil {
		normalize = strings.TrimSpace
	}
	findRollout := findRolloutPath
	if findRollout == nil {
		findRollout = codex.FindRolloutPath
	}
	readRollout := readRolloutMessagesWithTrim
	if readRollout == nil {
		readRollout = codex.ReadRolloutMessagesWithTrim
	}

	codexThreadID, rolloutPath := resolve(ctx, threadID)
	codexThreadID = normalize(codexThreadID)
	if codexThreadID == "" {
		return []threadHistoryMessage{}, nil
	}

	path := strings.TrimSpace(rolloutPath)
	if path == "" {
		resolvedPath, err := findRollout(codexThreadID)
		if err != nil {
			return []threadHistoryMessage{}, nil
		}
		path = resolvedPath
	}
	if path == "" {
		return []threadHistoryMessage{}, nil
	}
	if _, err := os.Stat(path); err != nil {
		return []threadHistoryMessage{}, nil
	}

	trimInjected := !showInjectedPromptInChat
	rolloutMsgs, err := readRollout(path, trimInjected)
	if err != nil {
		return nil, err
	}
	if len(rolloutMsgs) == 0 {
		return []threadHistoryMessage{}, nil
	}

	all := make([]threadHistoryMessage, 0, len(rolloutMsgs))
	for i, item := range rolloutMsgs {
		message, ok := rolloutMessageToThreadHistory(threadID, i, item)
		if !ok {
			continue
		}
		all = append(all, message)
	}
	if len(all) == 0 {
		return []threadHistoryMessage{}, nil
	}
	return all, nil
}

func rolloutMessageToThreadHistory(threadID string, index int, item codex.RolloutMessage) (threadHistoryMessage, bool) {
	role := strings.ToLower(strings.TrimSpace(item.Role))
	if role != "user" && role != "assistant" {
		return threadHistoryMessage{}, false
	}
	eventType := ""
	if role == "assistant" {
		eventType = agentcore.EventAgentMessage
	}
	return threadHistoryMessage{
		ID:        int64(index + 1),
		AgentID:   threadID,
		Role:      role,
		EventType: eventType,
		Method:    "",
		Content:   item.Content,
		Metadata:  nil,
		CreatedAt: parseRolloutTimestamp(item.Timestamp),
	}, true
}

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

func runningCodexThreadIDFromManager(
	manager *runner.AgentManager,
	threadID string,
	getThreadID func(*runner.AgentProcess) string,
) string {
	if manager == nil || getThreadID == nil {
		return ""
	}
	proc := manager.Get(threadID)
	if proc == nil || proc.Client == nil {
		return ""
	}
	return getThreadID(proc)
}

func bindingRolloutSourceByAgentIDInStore(
	ctx context.Context,
	bindingStore *store.AgentCodexBindingStore,
	agentID string,
) (string, string, error) {
	if bindingStore == nil {
		return "", "", nil
	}
	binding, err := bindingStore.FindByAgentID(ctx, agentID)
	if err != nil {
		return "", "", err
	}
	if binding == nil {
		return "", "", nil
	}
	return binding.CodexThreadID, binding.RolloutPath, nil
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

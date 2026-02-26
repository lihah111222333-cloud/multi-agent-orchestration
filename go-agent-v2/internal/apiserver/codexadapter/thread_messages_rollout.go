package codexadapter

import (
	"context"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
	"os"
	"strings"
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

// ResolveRolloutHistorySource resolves codex thread id and rollout path via adapter context stores.
func (a *Adapter) ResolveRolloutHistorySource(
	ctx context.Context,
	threadID string,
	normalizeCodexThreadID func(string) string,
) (string, string) {
	return resolveRolloutHistorySource(
		ctx,
		threadID,
		a.runningCodexThreadID,
		a.bindingRolloutSourceByAgentID,
		a.statusSessionIDByAgentID,
		normalizeCodexThreadID,
	)
}

func (a *Adapter) runningCodexThreadID(threadID string) string {
	manager := a.manager()
	if manager == nil {
		return ""
	}
	proc := manager.Get(threadID)
	if proc == nil || proc.Client == nil {
		return ""
	}
	return a.GetThreadID(proc)
}

func (a *Adapter) bindingRolloutSourceByAgentID(ctx context.Context, agentID string) (string, string, error) {
	bindingStore := a.bindingStore()
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

func (a *Adapter) resolveRolloutHistorySource(ctx context.Context, threadID string) (string, string) {
	return a.ResolveRolloutHistorySource(ctx, threadID, normalizeCodexThreadID)
}

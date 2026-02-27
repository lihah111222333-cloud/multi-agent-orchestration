package codexadapter

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
	rolloutsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/rollout"
)

// ResolveRolloutHistorySource resolves codex thread id and rollout path via adapter context stores.
func (a *Adapter) ResolveRolloutHistorySource(
	ctx context.Context,
	threadID string,
	normalizeCodexThreadID func(string) string,
) (string, string) {
	return rolloutsvc.ResolveRolloutHistorySource(
		ctx,
		threadID,
		a.runningCodexThreadID,
		a.bindingRolloutSourceByAgentID,
		a.statusSessionIDByAgentID,
		normalizeCodexThreadID,
	)
}

func (a *Adapter) runningCodexThreadID(threadID string) string {
	return rolloutsvc.RunningCodexThreadIDFromManager(threadID, a.managerProcess, a.getThreadIDFromAny)
}

func (a *Adapter) getThreadIDFromAny(proc any) string {
	typed, _ := proc.(*runner.AgentProcess)
	return a.GetThreadID(typed)
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

func loadAllThreadMessagesFromCodexRollout(
	ctx context.Context,
	threadID string,
	resolveRolloutHistorySource func(context.Context, string) (string, string),
	normalizeCodexThreadID func(string) string,
	findRolloutPath func(string) (string, error),
	readRolloutMessagesWithTrim func(path string, trimInjected bool) ([]codex.RolloutMessage, error),
	showInjectedPromptInChat bool,
) ([]threadHistoryMessage, error) {
	items, err := rolloutsvc.LoadAllThreadMessagesFromCodexRollout(
		ctx,
		threadID,
		resolveRolloutHistorySource,
		normalizeCodexThreadID,
		findRolloutPath,
		readRolloutMessagesWithTrim,
		showInjectedPromptInChat,
	)
	if err != nil {
		return nil, err
	}
	out := make([]threadHistoryMessage, 0, len(items))
	for _, item := range items {
		out = append(out, threadHistoryMessage{
			ID:        item.ID,
			AgentID:   item.AgentID,
			Role:      item.Role,
			EventType: item.EventType,
			Method:    item.Method,
			Content:   item.Content,
			Metadata:  item.Metadata,
			CreatedAt: item.CreatedAt,
		})
	}
	return out, nil
}

func paginateRolloutMessages(all []threadHistoryMessage, limit int, before int64) []threadHistoryMessage {
	serviceItems := make([]rolloutsvc.ThreadHistoryMessage, 0, len(all))
	for _, item := range all {
		serviceItems = append(serviceItems, rolloutsvc.ThreadHistoryMessage{
			ID:        item.ID,
			AgentID:   item.AgentID,
			Role:      item.Role,
			EventType: item.EventType,
			Method:    item.Method,
			Content:   item.Content,
			Metadata:  item.Metadata,
			CreatedAt: item.CreatedAt,
		})
	}
	page := rolloutsvc.PaginateRolloutMessages(serviceItems, limit, before)
	out := make([]threadHistoryMessage, 0, len(page))
	for _, item := range page {
		out = append(out, threadHistoryMessage{
			ID:        item.ID,
			AgentID:   item.AgentID,
			Role:      item.Role,
			EventType: item.EventType,
			Method:    item.Method,
			Content:   item.Content,
			Metadata:  item.Metadata,
			CreatedAt: item.CreatedAt,
		})
	}
	return out
}

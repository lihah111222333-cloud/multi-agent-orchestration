package codexadapter

import "context"

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
	return runningCodexThreadIDFromManager(a.manager(), threadID, a.GetThreadID)
}

func (a *Adapter) bindingRolloutSourceByAgentID(ctx context.Context, agentID string) (string, string, error) {
	return bindingRolloutSourceByAgentIDInStore(ctx, a.bindingStore(), agentID)
}

func (a *Adapter) resolveRolloutHistorySource(ctx context.Context, threadID string) (string, string) {
	return a.ResolveRolloutHistorySource(ctx, threadID, normalizeCodexThreadID)
}

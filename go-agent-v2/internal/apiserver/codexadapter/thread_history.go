package codexadapter

import (
	"context"
	"time"

	historyconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/history"
	lifecycleconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/lifecycle"
)

const defaultHistoryLookupTimeout = 5 * time.Second

// resolveCodexThreadCandidates resolves ordered codex thread candidates from stores.
func resolveCodexThreadCandidates(
	ctx context.Context,
	agentID string,
	timeout time.Duration,
	appendUniqueThreadID func(dst []string, seen map[string]struct{}, candidate string) []string,
	findBindingCodexThreadID func(context.Context, string) (string, error),
	findStatusSessionID func(context.Context, string) (string, error),
	previewCandidates func([]string, int) []string,
) []string {
	preview := previewCandidates
	if preview == nil {
		preview = lifecycleconsumer.PreviewResumeCandidates
	}
	return historyconsumer.ResolveCodexThreadCandidates(
		ctx,
		agentID,
		timeout,
		appendUniqueThreadID,
		findBindingCodexThreadID,
		findStatusSessionID,
		preview,
	)
}

// ResolveCodexThreadCandidates resolves candidate codex thread IDs via adapter context stores.
func (a *Adapter) ResolveCodexThreadCandidates(ctx context.Context, agentID string, appendUniqueThreadID func(dst []string, seen map[string]struct{}, candidate string) []string, previewCandidates func([]string, int) []string) []string {
	return resolveCodexThreadCandidates(ctx, agentID, 0, appendUniqueThreadID, a.bindingCodexThreadIDByAgentID, a.statusSessionIDByAgentID, previewCandidates)
}

// threadExistsInHistory checks whether a thread exists in runtime history sources.
func threadExistsInHistory(
	ctx context.Context,
	threadID string,
	timeout time.Duration,
	isLikelyCodexThreadID func(string) bool,
	findBindingByAgentID func(context.Context, string) (bool, error),
	getAgentStatusByID func(context.Context, string) (bool, error),
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) bool {
	return historyconsumer.ThreadExistsInHistory(
		ctx,
		threadID,
		timeout,
		isLikelyCodexThreadID,
		findBindingByAgentID,
		getAgentStatusByID,
		loadThreadArchiveMap,
	)
}

// ThreadExistsInHistory checks whether a thread exists in historical sources via adapter context stores.
func (a *Adapter) ThreadExistsInHistory(ctx context.Context, threadID string) bool {
	return threadExistsInHistory(
		ctx,
		threadID,
		0,
		isLikelyCodexThreadID,
		a.bindingExistsByAgentID,
		a.agentStatusExistsByID,
		a.loadThreadArchiveMap,
	)
}

func (a *Adapter) bindingExistsByAgentID(ctx context.Context, agentID string) (bool, error) {
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return false, nil
	}
	binding, err := bindingStore.FindByAgentID(ctx, agentID)
	if err != nil {
		return false, err
	}
	return binding != nil, nil
}

func (a *Adapter) agentStatusExistsByID(ctx context.Context, agentID string) (bool, error) {
	statusStore := a.statusStore()
	if statusStore == nil {
		return false, nil
	}
	status, err := statusStore.Get(ctx, agentID)
	if err != nil {
		return false, err
	}
	return status != nil, nil
}

func (a *Adapter) bindingCodexThreadIDByAgentID(ctx context.Context, agentID string) (string, error) {
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return "", nil
	}
	binding, err := bindingStore.FindByAgentID(ctx, agentID)
	if err != nil {
		return "", err
	}
	if binding == nil {
		return "", nil
	}
	return binding.CodexThreadID, nil
}

func (a *Adapter) statusSessionIDByAgentID(ctx context.Context, agentID string) (string, error) {
	statusStore := a.statusStore()
	if statusStore == nil {
		return "", nil
	}
	status, err := statusStore.Get(ctx, agentID)
	if err != nil {
		return "", err
	}
	if status == nil {
		return "", nil
	}
	return status.SessionID, nil
}

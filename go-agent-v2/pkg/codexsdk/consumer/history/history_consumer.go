package history

import (
	"context"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/store"
	historysvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/history"
)

const DefaultHistoryLookupTimeout = historysvc.DefaultHistoryLookupTimeout

func EnsureContext(ctx context.Context) context.Context {
	return historysvc.EnsureContext(ctx)
}

func NormalizeHistoryTimeout(timeout time.Duration) time.Duration {
	return historysvc.NormalizeHistoryTimeout(timeout)
}

func AppendUniqueThreadIDFallback(dst []string, seen map[string]struct{}, candidate string) []string {
	return historysvc.AppendUniqueThreadIDFallback(dst, seen, candidate)
}

func ResolveCodexThreadCandidates(
	ctx context.Context,
	agentID string,
	timeout time.Duration,
	appendUniqueThreadID func(dst []string, seen map[string]struct{}, candidate string) []string,
	findBindingCodexThreadID func(context.Context, string) (string, error),
	findStatusSessionID func(context.Context, string) (string, error),
	previewCandidates func([]string, int) []string,
) []string {
	return historysvc.ResolveCodexThreadCandidates(
		ctx,
		agentID,
		timeout,
		appendUniqueThreadID,
		findBindingCodexThreadID,
		findStatusSessionID,
		previewCandidates,
	)
}

func ThreadExistsInHistory(
	ctx context.Context,
	threadID string,
	timeout time.Duration,
	isLikelyCodexThreadID func(string) bool,
	findBindingByAgentID func(context.Context, string) (bool, error),
	getAgentStatusByID func(context.Context, string) (bool, error),
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
) bool {
	return historysvc.ThreadExistsInHistory(
		ctx,
		threadID,
		timeout,
		isLikelyCodexThreadID,
		findBindingByAgentID,
		getAgentStatusByID,
		loadThreadArchiveMap,
	)
}

func findBindingByAgentID(ctx context.Context, bindingStore *store.AgentCodexBindingStore, agentID string) (*store.AgentCodexBinding, error) {
	if bindingStore == nil {
		return nil, nil
	}
	return bindingStore.FindByAgentID(ctx, agentID)
}

func findStatusByAgentID(ctx context.Context, statusStore *store.AgentStatusStore, agentID string) (*store.AgentStatus, error) {
	if statusStore == nil {
		return nil, nil
	}
	return statusStore.Get(ctx, agentID)
}

func BindingExistsByAgentID(ctx context.Context, bindingStore *store.AgentCodexBindingStore, agentID string) (bool, error) {
	binding, err := findBindingByAgentID(ctx, bindingStore, agentID)
	return binding != nil, err
}

func AgentStatusExistsByID(ctx context.Context, statusStore *store.AgentStatusStore, agentID string) (bool, error) {
	status, err := findStatusByAgentID(ctx, statusStore, agentID)
	return status != nil, err
}

func BindingCodexThreadIDByAgentID(ctx context.Context, bindingStore *store.AgentCodexBindingStore, agentID string) (string, error) {
	binding, err := findBindingByAgentID(ctx, bindingStore, agentID)
	if err != nil || binding == nil {
		return "", err
	}
	return binding.CodexThreadID, nil
}

func StatusSessionIDByAgentID(ctx context.Context, statusStore *store.AgentStatusStore, agentID string) (string, error) {
	status, err := findStatusByAgentID(ctx, statusStore, agentID)
	if err != nil || status == nil {
		return "", err
	}
	return status.SessionID, nil
}

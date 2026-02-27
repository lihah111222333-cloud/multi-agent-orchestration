package history

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/store"
	historysvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/history"
)

const DefaultHistoryLookupTimeout = historysvc.DefaultHistoryLookupTimeout

var (
	ResolveCodexThreadCandidates = historysvc.ResolveCodexThreadCandidates
	ThreadExistsInHistory        = historysvc.ThreadExistsInHistory
)

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

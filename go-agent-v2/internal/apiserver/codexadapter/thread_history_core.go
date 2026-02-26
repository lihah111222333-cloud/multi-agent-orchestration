package codexadapter

import (
	"context"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

func ensureContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func normalizeHistoryTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultHistoryLookupTimeout
	}
	return timeout
}

func appendUniqueThreadIDFallback(dst []string, seen map[string]struct{}, candidate string) []string {
	id := strings.TrimSpace(candidate)
	if id == "" {
		return dst
	}
	if seen == nil {
		seen = map[string]struct{}{}
	}
	if _, ok := seen[id]; ok {
		return dst
	}
	seen[id] = struct{}{}
	return append(dst, id)
}

func bindingExistsByAgentID(
	ctx context.Context,
	bindingStore *store.AgentCodexBindingStore,
	agentID string,
) (bool, error) {
	if bindingStore == nil {
		return false, nil
	}
	binding, err := bindingStore.FindByAgentID(ctx, agentID)
	if err != nil {
		return false, err
	}
	return binding != nil, nil
}

func agentStatusExistsByID(
	ctx context.Context,
	statusStore *store.AgentStatusStore,
	agentID string,
) (bool, error) {
	if statusStore == nil {
		return false, nil
	}
	status, err := statusStore.Get(ctx, agentID)
	if err != nil {
		return false, err
	}
	return status != nil, nil
}

func bindingCodexThreadIDByAgentID(
	ctx context.Context,
	bindingStore *store.AgentCodexBindingStore,
	agentID string,
) (string, error) {
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

func statusSessionIDByAgentID(
	ctx context.Context,
	statusStore *store.AgentStatusStore,
	agentID string,
) (string, error) {
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

package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ThreadRead fetches codex history list for the target thread.
func (a *Adapter) ThreadRead(_ context.Context, threadID string) (map[string]any, error) {
	return withProcess(a, "Server.threadRead", threadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			threads, listErr := a.ListThreads(proc)
			if listErr != nil {
				return nil, listErr
			}
			return map[string]any{"history": threads}, nil
		})
}

// ThreadResolve resolves thread identity from runtime and history sources.
func (a *Adapter) ThreadResolve(ctx context.Context, threadID string) (map[string]any, error) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New("Server.threadResolve", "threadId is required")
	}
	result := map[string]any{
		"threadId": id,
	}

	var codexThreadID string
	resolveSource := "history"
	if state, port, runtimeCodexThreadID, ok := a.resolveRunningThreadIdentity(id); ok {
		if state != "" {
			result["state"] = state
		}
		if port > 0 {
			result["port"] = port
		}
		codexThreadID = runtimeCodexThreadID
		resolveSource = "running"
	}
	if codexThreadID == "" {
		codexThreadID = a.firstResolvedCodexThreadID(ctx, id)
	}
	if codexThreadID != "" {
		result["codexThreadId"] = codexThreadID
	}
	if IsLikelyCodexThreadID(codexThreadID) {
		result["uuid"] = codexThreadID
	}
	hasHistory := a.ThreadExistsInHistory(ctx, id)
	result["hasHistory"] = hasHistory
	logger.Info("thread/resolve: identity resolved",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"source", resolveSource,
		"state", result["state"],
		logger.FieldPort, result["port"],
		"codex_thread_id", codexThreadID,
		"has_history", hasHistory,
	)
	return result, nil
}

func (a *Adapter) firstResolvedCodexThreadID(ctx context.Context, threadID string) string {
	candidates := a.ResolveCodexThreadCandidates(ctx, threadID, appendUniqueThreadIDFallback, PreviewResumeCandidates)
	if len(candidates) == 0 {
		return ""
	}
	return strings.TrimSpace(candidates[0])
}

func (a *Adapter) resolveRunningThreadIdentity(threadID string) (state string, port int, codexThreadID string, found bool) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", 0, "", false
	}
	for _, info := range a.runningAgents() {
		if strings.TrimSpace(info.ID) != id {
			continue
		}
		return strings.TrimSpace(string(info.State)), info.Port, strings.TrimSpace(info.ThreadID), true
	}
	return "", 0, "", false
}

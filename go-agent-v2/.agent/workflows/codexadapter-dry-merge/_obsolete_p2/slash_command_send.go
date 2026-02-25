package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (a *Adapter) sendSlashCommand(ctx context.Context, methodName, threadID, command, args string) (map[string]any, error) {
	id, err := a.resolveThreadForSlashCommand(ctx, threadID)
	if err != nil {
		return nil, err
	}
	return withProcess(a, methodName, id, func(proc *runner.AgentProcess) (map[string]any, error) {
		if proc == nil || proc.Client == nil {
			return nil, apperrors.New(methodName, "thread process not available")
		}
		if err := a.SendCommand(proc, command, args); err != nil {
			wrapped := apperrors.Wrap(err, methodName, "send slash command")
			if strings.Contains(strings.ToLower(err.Error()), "timeout") {
				logger.Warn("slash command timeout",
					logger.FieldThreadID, id,
					"command", command,
					logger.FieldError, err,
				)
			}
			return nil, wrapped
		}
		return map[string]any{}, nil
	})
}

func (a *Adapter) resolveThreadForSlashCommand(ctx context.Context, threadID string) (string, error) {
	id := strings.TrimSpace(threadID)
	if id != "" {
		return id, nil
	}
	threads, err := a.ThreadList(ctx)
	if err != nil {
		return "", apperrors.Wrap(err, "Server.sendSlashCommand", "resolve active thread")
	}
	for _, item := range threads {
		if strings.EqualFold(strings.TrimSpace(item.State), "running") {
			if resolved := strings.TrimSpace(item.ID); resolved != "" {
				return resolved, nil
			}
		}
	}
	if len(threads) > 0 {
		if fallback := strings.TrimSpace(threads[0].ID); fallback != "" {
			return fallback, nil
		}
	}
	return "", apperrors.New("Server.sendSlashCommand", "threadId is required")
}

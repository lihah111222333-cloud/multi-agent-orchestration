package codexadapter

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type slashCommandWithArgsParams struct {
	ThreadID string `json:"threadId"`
	Args     string `json:"args,omitempty"`
}

// SendSlashCommandFromRawParams parses raw params and sends slash command.
func (a *Adapter) SendSlashCommandFromRawParams(ctx context.Context, params json.RawMessage, command string) (any, error) {
	parsed, err := parseSlashCommandWithArgsParams(params)
	if err != nil {
		return nil, err
	}
	return a.sendSlashCommand(ctx, "Server.sendSlashCommand", parsed.ThreadID, command, parsed.Args)
}

// SendSlashCommandWithArgs parses key-based argument from raw params and sends slash command.
func (a *Adapter) SendSlashCommandWithArgs(params json.RawMessage, command, argKey string) (any, error) {
	parsed, err := parseSlashCommandArgParams(params, argKey)
	if err != nil {
		return nil, err
	}
	return a.sendSlashCommand(context.Background(), "Server.sendSlashCommand", parsed.ThreadID, command, parsed.Args)
}

// ThreadSkillsList sends /skills command to thread and returns placeholder payload.
func (a *Adapter) ThreadSkillsList() (any, error) {
	result, err := a.sendSlashCommand(context.Background(), "Server.threadSkillsList", "", "/skills", "")
	if err != nil {
		return nil, err
	}
	if result == nil {
		return map[string]any{}, nil
	}
	return result, nil
}

// parseSlashCommandWithArgsParams parses threadId + args payload for slash command handlers.
func parseSlashCommandWithArgsParams(params json.RawMessage) (slashCommandWithArgsParams, error) {
	return parseSlashCommandArgParams(params, "args")
}

// parseSlashCommandArgParams parses threadId + key-based arg payload for slash command handlers.
func parseSlashCommandArgParams(params json.RawMessage, argKey string) (slashCommandWithArgsParams, error) {
	parsed := slashCommandWithArgsParams{}
	if len(params) == 0 {
		return parsed, nil
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(params, &decoded); err != nil {
		return parsed, apperrors.Wrap(err, "Server.parseSlashCommandWithArgsParams", "invalid params")
	}
	parsed.ThreadID = extractTrackedString(decoded, "threadId", "threadID", "thread_id")
	keys := []string{"args"}
	if key := strings.TrimSpace(argKey); key != "" && !strings.EqualFold(key, "args") {
		keys = append([]string{key}, keys...)
	}
	parsed.Args = extractTrackedString(decoded, keys...)
	return parsed, nil
}

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

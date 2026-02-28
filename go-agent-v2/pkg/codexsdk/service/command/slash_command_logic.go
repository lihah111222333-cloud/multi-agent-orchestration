package command

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type ThreadListItem = agentcore.ThreadListItem
type SlashCommandWithArgsParams struct {
	ThreadID string `json:"threadId"`
	Args     string `json:"args,omitempty"`
}

func RunSendSlashCommand(
	ctx context.Context,
	methodName string,
	threadID string,
	command string,
	args string,
	requireThreadID bool,
	resolveThread func(context.Context, string, bool) (string, error),
	withProcess func(string, string, func(any) (map[string]any, error)) (map[string]any, error),
	sendCommand func(any, string, string) error,
) (map[string]any, error) {
	if resolveThread == nil {
		return nil, apperrors.New(methodName, "thread resolver is not initialized")
	}
	id, err := resolveThread(ctx, threadID, requireThreadID)
	if err != nil {
		return nil, err
	}
	if withProcess == nil {
		return nil, apperrors.New(methodName, "thread process resolver is not initialized")
	}
	return withProcess(methodName, id, func(proc any) (map[string]any, error) {
		if proc == nil {
			return nil, apperrors.New(methodName, "thread process not available")
		}
		if sendCommand == nil {
			return nil, apperrors.New(methodName, "command sender is not initialized")
		}
		if err := sendCommand(proc, command, args); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "timeout") {
				logger.Warn("slash command timeout",
					logger.FieldThreadID, id,
					"command", command,
					logger.FieldError, err,
				)
			}
			return nil, apperrors.Wrap(err, methodName, "send slash command")
		}
		return map[string]any{}, nil
	})
}

func ResolveThreadForSlashCommandLogic(
	ctx context.Context,
	threadID string,
	requireThreadID bool,
	threadList func(context.Context) ([]ThreadListItem, error),
) (string, error) {
	id := strings.TrimSpace(threadID)
	if id != "" {
		return id, nil
	}
	if requireThreadID {
		return "", apperrors.New("Server.sendSlashCommand", "threadId is required")
	}
	if threadList == nil {
		return "", apperrors.New("Server.sendSlashCommand", "thread resolver is not initialized")
	}
	threads, err := threadList(ctx)
	if err != nil {
		return "", apperrors.Wrap(err, "Server.sendSlashCommand", "resolve active thread")
	}
	for _, item := range threads {
		if resolved := strings.TrimSpace(item.ID); resolved != "" && strings.EqualFold(strings.TrimSpace(item.State), "running") {
			return resolved, nil
		}
	}
	if len(threads) > 0 {
		if fallback := strings.TrimSpace(threads[0].ID); fallback != "" {
			return fallback, nil
		}
	}
	return "", apperrors.New("Server.sendSlashCommand", "threadId is required")
}

func ThreadSkillsListResult(result map[string]any, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if result == nil {
		return map[string]any{}, nil
	}
	return result, nil
}

func ParseSlashCommandArgParams(params json.RawMessage, argKey string, extractString func(map[string]any, ...string) string) (SlashCommandWithArgsParams, error) {
	parsed := SlashCommandWithArgsParams{}
	if len(params) == 0 {
		return parsed, nil
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(params, &decoded); err != nil {
		return parsed, apperrors.Wrap(err, "Server.parseSlashCommandWithArgsParams", "invalid params")
	}
	parsed.ThreadID = extractString(decoded, "threadId", "threadID", "thread_id")
	key := "args"
	if arg := strings.TrimSpace(argKey); arg != "" && !strings.EqualFold(arg, key) {
		key = arg
	}
	parsed.Args = extractString(decoded, key, "args")
	return parsed, nil
}

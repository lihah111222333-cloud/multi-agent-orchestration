package codexadapter

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
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
	return a.sendSlashCommand(ctx, "Server.sendSlashCommand", parsed.ThreadID, command, parsed.Args, false)
}

// SendSlashCommandFromRawParamsRequireThreadID parses raw params and sends slash command.
// The threadId field must be provided and non-empty.
func (a *Adapter) SendSlashCommandFromRawParamsRequireThreadID(ctx context.Context, params json.RawMessage, command string) (any, error) {
	parsed, err := parseSlashCommandWithArgsParams(params)
	if err != nil {
		return nil, err
	}
	return a.sendSlashCommand(ctx, "Server.sendSlashCommand", parsed.ThreadID, command, parsed.Args, true)
}

// SendSlashCommandWithArgs parses key-based argument from raw params and sends slash command.
func (a *Adapter) SendSlashCommandWithArgs(params json.RawMessage, command, argKey string) (any, error) {
	parsed, err := parseSlashCommandArgParams(params, argKey)
	if err != nil {
		return nil, err
	}
	return a.sendSlashCommand(context.Background(), "Server.sendSlashCommand", parsed.ThreadID, command, parsed.Args, false)
}

// ThreadSkillsList sends /skills command to thread and returns placeholder payload.
func (a *Adapter) ThreadSkillsList() (any, error) {
	result, err := a.sendSlashCommand(context.Background(), "Server.threadSkillsList", "", "/skills", "", false)
	return threadSkillsListResult(result, err)
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

func (a *Adapter) sendSlashCommand(ctx context.Context, methodName, threadID, command, args string, requireThreadID bool) (map[string]any, error) {
	return runSendSlashCommand(
		ctx,
		methodName,
		threadID,
		command,
		args,
		requireThreadID,
		a.resolveThreadForSlashCommand,
		a.withProcessMap,
		a.SendCommand,
	)
}

func (a *Adapter) withProcessMap(
	methodName string,
	threadID string,
	fn func(*runner.AgentProcess) (map[string]any, error),
) (map[string]any, error) {
	return withProcess(a, methodName, threadID, fn)
}

func (a *Adapter) resolveThreadForSlashCommand(ctx context.Context, threadID string, requireThreadID bool) (string, error) {
	return resolveThreadForSlashCommandLogic(ctx, threadID, requireThreadID, a.ThreadList)
}

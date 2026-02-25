package codexadapter

import (
	"context"
	"encoding/json"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type slashCommandWithArgsParams struct {
	ThreadID string `json:"threadId"`
	Args     string `json:"args,omitempty"`
}

// SendSlashCommandFromRawParams parses raw params and sends slash command.
func (a *Adapter) SendSlashCommandFromRawParams(ctx context.Context, params json.RawMessage, command string) (any, error) {
	parsed, err := ParseSlashCommandWithArgsParams(params)
	if err != nil {
		return nil, err
	}
	return a.sendSlashCommand(ctx, "Server.sendSlashCommand", parsed.ThreadID, command, parsed.Args)
}

// SendSlashCommandWithArgs parses key-based argument from raw params and sends slash command.
func (a *Adapter) SendSlashCommandWithArgs(params json.RawMessage, command, argKey string) (any, error) {
	parsed, err := ParseSlashCommandArgParams(params, argKey)
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

// ParseSlashCommandWithArgsParams parses threadId + args payload for slash command handlers.
func ParseSlashCommandWithArgsParams(params json.RawMessage) (slashCommandWithArgsParams, error) {
	return ParseSlashCommandArgParams(params, "args")
}

// ParseSlashCommandArgParams parses threadId + key-based arg payload for slash command handlers.
func ParseSlashCommandArgParams(params json.RawMessage, argKey string) (slashCommandWithArgsParams, error) {
	parsed := slashCommandWithArgsParams{}
	if len(params) == 0 {
		return parsed, nil
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(params, &decoded); err != nil {
		return parsed, apperrors.Wrap(err, "Server.parseSlashCommandWithArgsParams", "invalid params")
	}
	parsed.ThreadID = ExtractTrackedString(decoded, "threadId", "threadID", "thread_id")
	keys := []string{"args"}
	if key := strings.TrimSpace(argKey); key != "" && !strings.EqualFold(key, "args") {
		keys = append([]string{key}, keys...)
	}
	parsed.Args = ExtractTrackedString(decoded, keys...)
	return parsed, nil
}

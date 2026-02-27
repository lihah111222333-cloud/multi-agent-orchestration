package command

import (
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	commandsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/command"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type ThreadListItem = commandsvc.ThreadListItem
type SlashCommandWithArgsParams struct {
	ThreadID string `json:"threadId"`
	Args     string `json:"args,omitempty"`
}

var (
	RunSendSlashCommand               = commandsvc.RunSendSlashCommand
	ResolveThreadForSlashCommandLogic = commandsvc.ResolveThreadForSlashCommandLogic
	ThreadSkillsListResult            = commandsvc.ThreadSkillsListResult
)

func ToThreadListItems(items []contracts.ThreadListItem) []ThreadListItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]ThreadListItem, len(items))
	for i, item := range items {
		out[i] = ThreadListItem{ID: item.ID, Name: item.Name, State: item.State}
	}
	return out
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
	if key := strings.TrimSpace(argKey); key == "" || strings.EqualFold(key, "args") {
		parsed.Args = extractString(decoded, "args")
	} else {
		parsed.Args = extractString(decoded, key, "args")
	}
	return parsed, nil
}

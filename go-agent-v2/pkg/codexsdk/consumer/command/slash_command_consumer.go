package command

import (
	"context"

	commandsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/command"
)

type ThreadListItem = commandsvc.ThreadListItem

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
	return commandsvc.RunSendSlashCommand(
		ctx,
		methodName,
		threadID,
		command,
		args,
		requireThreadID,
		resolveThread,
		withProcess,
		sendCommand,
	)
}

func ResolveThreadForSlashCommandLogic(
	ctx context.Context,
	threadID string,
	requireThreadID bool,
	threadList func(context.Context) ([]ThreadListItem, error),
) (string, error) {
	return commandsvc.ResolveThreadForSlashCommandLogic(ctx, threadID, requireThreadID, threadList)
}

func ThreadSkillsListResult(result map[string]any, err error) (any, error) {
	return commandsvc.ThreadSkillsListResult(result, err)
}

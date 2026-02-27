package rollout

import (
	"context"
	"os"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
	rolloutsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/rollout"
)

func LoadAllThreadMessagesFromCodexRollout(
	ctx context.Context,
	threadID string,
	resolveRolloutHistorySource func(context.Context, string) (string, string),
	normalizeCodexThreadID func(string) string,
	showInjectedPromptInChat bool,
) ([]rolloutsvc.ThreadHistoryMessage, error) {
	return rolloutsvc.LoadAllThreadMessagesFromCodexRollout(
		ctx,
		threadID,
		resolveRolloutHistorySource,
		normalizeCodexThreadID,
		codex.FindRolloutPath,
		func(path string) bool { _, err := os.Stat(path); return err == nil },
		codex.ReadRolloutMessagesWithTrim,
		showInjectedPromptInChat,
	)
}

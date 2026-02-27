package rollout

import (
	"context"
	"time"

	rolloutsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/rollout"
)

type ThreadHistoryMessage = rolloutsvc.ThreadHistoryMessage

func ParseRolloutTimestamp(raw string) time.Time {
	return rolloutsvc.ParseRolloutTimestamp(raw)
}

func PaginateRolloutMessages(all []ThreadHistoryMessage, limit int, before int64) []ThreadHistoryMessage {
	return rolloutsvc.PaginateRolloutMessages(all, limit, before)
}

func RunningCodexThreadIDFromManager(
	threadID string,
	getProcess func(string) any,
	getThreadID func(any) string,
) string {
	return rolloutsvc.RunningCodexThreadIDFromManager(threadID, getProcess, getThreadID)
}

func ResolveRolloutHistorySource(
	ctx context.Context,
	threadID string,
	getRunningCodexThreadID func(threadID string) string,
	findBinding func(context.Context, string) (codexThreadID string, rolloutPath string, err error),
	findStatusSessionID func(context.Context, string) (string, error),
	normalizeCodexThreadID func(string) string,
) (codexThreadID string, rolloutPath string) {
	return rolloutsvc.ResolveRolloutHistorySource(
		ctx,
		threadID,
		getRunningCodexThreadID,
		findBinding,
		findStatusSessionID,
		normalizeCodexThreadID,
	)
}

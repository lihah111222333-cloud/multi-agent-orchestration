package rollout

import rolloutsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/rollout"

type ThreadHistoryMessage = rolloutsvc.ThreadHistoryMessage

var (
	RunningCodexThreadIDFromManager = rolloutsvc.RunningCodexThreadIDFromManager
	PaginateRolloutMessages         = rolloutsvc.PaginateRolloutMessages
	ResolveRolloutHistorySource     = rolloutsvc.ResolveRolloutHistorySource
)

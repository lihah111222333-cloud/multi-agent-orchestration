package contracts

import "github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"

type (
	AutoMatchInput                = agentcore.AutoMatchInput
	SkillMatchCandidate           = agentcore.SkillMatchCandidate
	AutoMatchedSkillMatch         = agentcore.AutoMatchedSkillMatch
	AutoSkillMatchOptions         = agentcore.AutoSkillMatchOptions
	TurnInput                     = agentcore.TurnInput
	TurnStartRequest              = agentcore.TurnStartRequest
	TurnSteerRequest              = agentcore.TurnSteerRequest
	TurnAppendUserTimelineOptions = agentcore.TurnAppendUserTimelineOptions
	TurnStartEntryPrepareResult   = agentcore.TurnStartEntryPrepareResult
	TurnSteerEntryPrepareResult   = agentcore.TurnSteerEntryPrepareResult
	ThreadListItem                = agentcore.ThreadListItem
)

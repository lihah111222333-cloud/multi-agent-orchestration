package contracts

import "github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"

// All shared DTO types are canonical in agentcore.
// These type aliases preserve backward compatibility for apiserver code.

type AutoMatchInput = agentcore.AutoMatchInput
type SkillMatchCandidate = agentcore.SkillMatchCandidate
type AutoMatchedSkillMatch = agentcore.AutoMatchedSkillMatch
type AutoSkillMatchOptions = agentcore.AutoSkillMatchOptions
type TurnInput = agentcore.TurnInput
type TurnStartRequest = agentcore.TurnStartRequest
type TurnSteerRequest = agentcore.TurnSteerRequest
type TurnAppendUserTimelineOptions = agentcore.TurnAppendUserTimelineOptions
type TurnStartEntryPrepareResult = agentcore.TurnStartEntryPrepareResult
type TurnSteerEntryPrepareResult = agentcore.TurnSteerEntryPrepareResult
type ThreadListItem = agentcore.ThreadListItem

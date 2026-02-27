package codexadapter

import (
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	serviceruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
)

func (a *Adapter) CollectAutoMatchedSkillMatchesForThread(
	threadID, prompt string,
	input []contracts.TurnInput,
	options contracts.AutoSkillMatchOptions,
) []autoMatchedSkillMatch {
	matches := serviceruntime.CollectAutoMatchedSkillMatchesForThread(
		newServiceRuntimeBridge(a.runtimeConsumerDeps()),
		threadID,
		prompt,
		toRuntimeTurnInputs(input),
		toRuntimeAutoSkillMatchOptions(options),
	)
	return fromRuntimeAutoMatchedSkillMatches(matches)
}

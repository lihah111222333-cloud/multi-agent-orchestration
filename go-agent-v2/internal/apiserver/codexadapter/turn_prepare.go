package codexadapter

import (
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	consumerruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/runtime"
)

func (a *Adapter) CollectAutoMatchedSkillMatchesForThread(
	threadID string,
	prompt string,
	input []contracts.TurnInput,
	options contracts.AutoSkillMatchOptions,
) []autoMatchedSkillMatch {
	return consumerruntime.CollectAutoMatchedSkillMatchesForThread(a.runtimeConsumerDeps(), threadID, prompt, input, options)
}

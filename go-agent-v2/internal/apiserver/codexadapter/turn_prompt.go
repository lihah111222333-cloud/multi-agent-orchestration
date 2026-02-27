package codexadapter

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	promptconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/prompt"
)

// BuildSelectedSkillPrompt reads selected-skill prompt using adapter-owned dependencies.
func (a *Adapter) BuildSelectedSkillPrompt(selectedSkills []string) (string, int) {
	if a == nil {
		return "", 0
	}
	return promptconsumer.BuildSelectedSkillPrompt(selectedSkills, a.readSkillContent, commonadapter.SkillInputText)
}

// ResolveLSPUsagePromptHint resolves LSP hint using adapter-owned preference store.
func (a *Adapter) ResolveLSPUsagePromptHint(ctx context.Context, defaultHint string, maxHintLen int) string {
	var getPref func(context.Context, string) (any, error)
	if store := a.store(); store != nil {
		getPref = store.Get
	}
	return promptconsumer.ResolveLSPUsagePromptHint(ctx, defaultHint, maxHintLen, getPref)
}

// PrependLSPAvailabilityWarning resolves warning content with adapter-owned defaults.
func (a *Adapter) PrependLSPAvailabilityWarning(hint string, dynamicTools []codexsdk.DynamicTool, mergePromptText func(string, string) string) (string, []string) {
	return promptconsumer.PrependLSPAvailabilityWarning(
		hint,
		promptconsumer.CollectDynamicToolNames(dynamicTools),
		promptconsumer.CollectReferencedLSPToolNames,
		mergePromptText,
	)
}

type autoMatchInput = contracts.AutoMatchInput
type autoMatchedSkillMatch = contracts.AutoMatchedSkillMatch

// CollectAutoMatchedSkillMatches evaluates auto-match candidates with adapter entry.
func (a *Adapter) CollectAutoMatchedSkillMatches(
	prompt string,
	inputs []autoMatchInput,
	configuredSkillNames []string,
	candidates []contracts.SkillMatchCandidate,
	options contracts.AutoSkillMatchOptions,
) []autoMatchedSkillMatch {
	return promptconsumer.CollectAutoMatchedSkillMatches(prompt, inputs, configuredSkillNames, candidates, options)
}

// RenderAutoMatchedSkillPrompt renders matched-skill prompt using adapter-owned dependencies.
func (a *Adapter) RenderAutoMatchedSkillPrompt(agentID string, matches []autoMatchedSkillMatch) (string, int) {
	if a == nil {
		return "", 0
	}
	return promptconsumer.RenderAutoMatchedSkillPrompt(
		agentID,
		matches,
		a.readSkillContent,
		commonadapter.MergePromptText,
		commonadapter.SkillInputText,
	)
}

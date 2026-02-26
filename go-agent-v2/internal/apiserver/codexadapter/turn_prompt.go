package codexadapter

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	promptsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/prompt"
)

// BuildSelectedSkillPrompt reads selected-skill prompt using adapter-owned dependencies.
func (a *Adapter) BuildSelectedSkillPrompt(selectedSkills []string) (string, int) {
	if a == nil {
		return "", 0
	}
	return promptsvc.BuildSelectedSkillPrompt(selectedSkills, a.readSkillContent, commonadapter.SkillInputText)
}

// ResolveLSPUsagePromptHint resolves LSP hint using adapter-owned preference store.
func (a *Adapter) ResolveLSPUsagePromptHint(ctx context.Context, defaultHint string, maxHintLen int) string {
	var getPref func(context.Context, string) (any, error)
	if store := a.store(); store != nil {
		getPref = store.Get
	}
	return promptsvc.ResolveLSPUsagePromptHint(ctx, defaultHint, maxHintLen, getPref)
}

// PrependLSPAvailabilityWarning resolves warning content with adapter-owned defaults.
func (a *Adapter) PrependLSPAvailabilityWarning(hint string, dynamicTools []agentcore.DynamicTool, mergePromptText func(string, string) string) (string, []string) {
	return promptsvc.PrependLSPAvailabilityWarning(
		hint,
		promptsvc.CollectDynamicToolNames(dynamicTools),
		promptsvc.CollectReferencedLSPToolNames,
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
	return collectAutoMatchedSkillMatches(prompt, inputs, configuredSkillNames, candidates, options)
}

func collectAutoMatchedSkillMatches(
	prompt string,
	inputs []autoMatchInput,
	configuredSkillNames []string,
	candidates []contracts.SkillMatchCandidate,
	options contracts.AutoSkillMatchOptions,
) []autoMatchedSkillMatch {
	serviceInputs := make([]promptsvc.AutoMatchInput, 0, len(inputs))
	for _, input := range inputs {
		serviceInputs = append(serviceInputs, promptsvc.AutoMatchInput{Type: input.Type, Name: input.Name})
	}
	serviceCandidates := make([]promptsvc.SkillMatchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		serviceCandidates = append(serviceCandidates, promptsvc.SkillMatchCandidate{
			Name:         candidate.Name,
			ForceWords:   candidate.ForceWords,
			TriggerWords: candidate.TriggerWords,
		})
	}
	serviceMatches := promptsvc.CollectAutoMatchedSkillMatches(
		prompt,
		serviceInputs,
		configuredSkillNames,
		serviceCandidates,
		promptsvc.AutoSkillMatchOptions{
			IncludeConfiguredExplicit: options.IncludeConfiguredExplicit,
			IncludeConfiguredForce:    options.IncludeConfiguredForce,
		},
	)
	matches := make([]autoMatchedSkillMatch, 0, len(serviceMatches))
	for _, match := range serviceMatches {
		matches = append(matches, contracts.AutoMatchedSkillMatch{
			Name:         match.Name,
			MatchedBy:    match.MatchedBy,
			MatchedTerms: match.MatchedTerms,
		})
	}
	return matches
}

// RenderAutoMatchedSkillPrompt renders matched-skill prompt using adapter-owned dependencies.
func (a *Adapter) RenderAutoMatchedSkillPrompt(agentID string, matches []autoMatchedSkillMatch) (string, int) {
	if a == nil {
		return "", 0
	}
	serviceMatches := make([]promptsvc.AutoMatchedSkillMatch, 0, len(matches))
	for _, match := range matches {
		serviceMatches = append(serviceMatches, promptsvc.AutoMatchedSkillMatch{
			Name:         match.Name,
			MatchedBy:    match.MatchedBy,
			MatchedTerms: match.MatchedTerms,
		})
	}
	return promptsvc.RenderAutoMatchedSkillPrompt(
		agentID,
		serviceMatches,
		a.readSkillContent,
		commonadapter.MergePromptText,
		commonadapter.SkillInputText,
	)
}

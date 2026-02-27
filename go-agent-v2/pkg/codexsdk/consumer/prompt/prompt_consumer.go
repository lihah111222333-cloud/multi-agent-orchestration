package prompt

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	promptsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/prompt"
)

func BuildSelectedSkillPrompt(
	selectedSkills []string,
	readSkillContent func(skillName string) (string, error),
	skillInputText func(name, content string) string,
) (string, int) {
	return promptsvc.BuildSelectedSkillPrompt(selectedSkills, readSkillContent, skillInputText)
}

func ResolveLSPUsagePromptHint(
	ctx context.Context,
	defaultHint string,
	maxHintLen int,
	getPref func(context.Context, string) (any, error),
) string {
	return promptsvc.ResolveLSPUsagePromptHint(ctx, defaultHint, maxHintLen, getPref)
}

func CollectDynamicToolNames(dynamicTools []codexsdk.DynamicTool) map[string]struct{} {
	return promptsvc.CollectDynamicToolNames(dynamicTools)
}

func PrependLSPAvailabilityWarning(
	hint string,
	dynamicToolNames map[string]struct{},
	collectReferencedToolNames func(string) []string,
	mergePromptText func(string, string) string,
) (string, []string) {
	return promptsvc.PrependLSPAvailabilityWarning(
		hint,
		dynamicToolNames,
		collectReferencedToolNames,
		mergePromptText,
	)
}

func CollectReferencedLSPToolNames(hint string) []string {
	return promptsvc.CollectReferencedLSPToolNames(hint)
}

func CollectAutoMatchedSkillMatches(
	prompt string,
	inputs []contracts.AutoMatchInput,
	configuredSkillNames []string,
	candidates []contracts.SkillMatchCandidate,
	options contracts.AutoSkillMatchOptions,
) []contracts.AutoMatchedSkillMatch {
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

	matches := make([]contracts.AutoMatchedSkillMatch, 0, len(serviceMatches))
	for _, match := range serviceMatches {
		matches = append(matches, contracts.AutoMatchedSkillMatch{
			Name:         match.Name,
			MatchedBy:    match.MatchedBy,
			MatchedTerms: match.MatchedTerms,
		})
	}
	return matches
}

func RenderAutoMatchedSkillPrompt(
	agentID string,
	matches []contracts.AutoMatchedSkillMatch,
	readSkillContent func(skillName string) (string, error),
	mergePromptText func(prompt, extra string) string,
	skillInputText func(name, content string) string,
) (string, int) {
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
		readSkillContent,
		mergePromptText,
		skillInputText,
	)
}

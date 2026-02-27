package prompt

import (
	"context"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	promptsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/prompt"
)

// All shared DTO types — canonical in agentcore — are transparent aliases.
// No conversion is needed between contracts/service/consumer layers.

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

func CollectDynamicToolNames(dynamicTools []agentcore.DynamicTool) map[string]struct{} {
	return promptsvc.CollectDynamicToolNames(dynamicTools)
}

func PrependLSPAvailabilityWarning(
	hint string,
	dynamicToolNames map[string]struct{},
	collectReferencedToolNames func(string) []string,
	mergePromptText func(string, string) string,
) (string, []string) {
	return promptsvc.PrependLSPAvailabilityWarning(hint, dynamicToolNames, collectReferencedToolNames, mergePromptText)
}

func CollectReferencedLSPToolNames(hint string) []string {
	return promptsvc.CollectReferencedLSPToolNames(hint)
}

// CollectAutoMatchedSkillMatches delegates directly — types are shared aliases.
func CollectAutoMatchedSkillMatches(
	prompt string,
	inputs []agentcore.AutoMatchInput,
	configuredSkillNames []string,
	candidates []agentcore.SkillMatchCandidate,
	options agentcore.AutoSkillMatchOptions,
) []agentcore.AutoMatchedSkillMatch {
	return promptsvc.CollectAutoMatchedSkillMatches(prompt, inputs, configuredSkillNames, candidates, options)
}

// RenderAutoMatchedSkillPrompt delegates directly — types are shared aliases.
func RenderAutoMatchedSkillPrompt(
	agentID string,
	matches []agentcore.AutoMatchedSkillMatch,
	readSkillContent func(skillName string) (string, error),
	mergePromptText func(prompt, extra string) string,
	skillInputText func(name, content string) string,
) (string, int) {
	return promptsvc.RenderAutoMatchedSkillPrompt(agentID, matches, readSkillContent, mergePromptText, skillInputText)
}

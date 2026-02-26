package codexadapter

import (
	"context"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/skillutil"
	"strings"
)

// BuildSelectedSkillPrompt reads selected-skill prompt using adapter-owned dependencies.
func (a *Adapter) BuildSelectedSkillPrompt(selectedSkills []string) (string, int) {
	if a == nil {
		return "", 0
	}
	return buildSelectedSkillPrompt(selectedSkills, a.readSkillContent, commonadapter.SkillInputText)
}

// ResolveLSPUsagePromptHint resolves LSP hint using adapter-owned preference store.
func (a *Adapter) ResolveLSPUsagePromptHint(ctx context.Context, defaultHint string, maxHintLen int) string {
	var getPref func(context.Context, string) (any, error)
	if store := a.store(); store != nil {
		getPref = store.Get
	}
	return resolveLSPUsagePromptHint(ctx, defaultHint, maxHintLen, getPref)
}

// collectDynamicToolNames builds a set of dynamic tool names.
func collectDynamicToolNames(dynamicTools []agentcore.DynamicTool) map[string]struct{} {
	if len(dynamicTools) == 0 {
		return nil
	}
	toolNames := make(map[string]struct{}, len(dynamicTools))
	for _, tool := range dynamicTools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		toolNames[name] = struct{}{}
	}
	return toolNames
}

// PrependLSPAvailabilityWarning resolves warning content with adapter-owned defaults.
func (a *Adapter) PrependLSPAvailabilityWarning(hint string, dynamicTools []agentcore.DynamicTool, mergePromptText func(string, string) string) (string, []string) {
	return prependLSPAvailabilityWarning(
		hint,
		collectDynamicToolNames(dynamicTools),
		commonadapter.CollectReferencedLSPToolNames,
		mergePromptText,
	)
}

type autoMatchInput = contracts.AutoMatchInput
type autoMatchedSkillMatch = contracts.AutoMatchedSkillMatch

// collectAutoMatchedSkillMatches classifies force/explicit matched skills for turn prompt injection.
func collectAutoMatchedSkillMatches(
	prompt string,
	inputs []autoMatchInput,
	configuredSkillNames []string,
	candidates []contracts.SkillMatchCandidate,
	options contracts.AutoSkillMatchOptions,
) []autoMatchedSkillMatch {
	normalizedPrompt := strings.ToLower(strings.TrimSpace(prompt))
	if normalizedPrompt == "" || len(candidates) == 0 {
		return nil
	}
	inputSkillSet := skillutil.CollectInputSkillNames(
		inputs,
		func(input autoMatchInput) string { return input.Type },
		func(input autoMatchInput) string { return input.Name },
	)
	configuredSet := commonadapter.CollectSkillNameSet(configuredSkillNames)

	matches := make([]autoMatchedSkillMatch, 0, len(candidates))
	for _, skill := range candidates {
		skillName := strings.TrimSpace(skill.Name)
		if skillName == "" {
			continue
		}
		skillNameLower := strings.ToLower(skillName)
		if _, exists := inputSkillSet[skillNameLower]; exists {
			continue
		}
		matchedBy, matchedTerms := commonadapter.ClassifyAutoSkillMatch(normalizedPrompt, skillName, skill.ForceWords, skill.TriggerWords)
		if matchedBy == "" {
			continue
		}
		if _, configured := configuredSet[skillNameLower]; configured {
			includeConfigured := false
			switch matchedBy {
			case "explicit":
				includeConfigured = options.IncludeConfiguredExplicit
			case "force":
				includeConfigured = options.IncludeConfiguredForce
			}
			if !includeConfigured {
				continue
			}
		}
		matches = append(matches, autoMatchedSkillMatch{
			Name:         skillName,
			MatchedBy:    matchedBy,
			MatchedTerms: matchedTerms,
		})
	}
	return matches
}

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

// renderAutoMatchedSkillPrompt renders skill prompt payload for matched skills.
func renderAutoMatchedSkillPrompt(
	agentID string,
	matches []autoMatchedSkillMatch,
	readSkillContent func(skillName string) (string, error),
	mergePromptText func(prompt, extra string) string,
	skillInputText func(name, content string) string,
) (string, int) {
	if len(matches) == 0 || readSkillContent == nil || skillInputText == nil {
		return "", 0
	}

	texts := make([]string, 0, len(matches))
	for _, match := range matches {
		skillName := strings.TrimSpace(match.Name)
		if skillName == "" {
			continue
		}
		content, err := readSkillContent(skillName)
		if err != nil {
			logAutoMatchedSkillReadError(agentID, skillName, match.MatchedBy, err)
			continue
		}
		if match.MatchedBy == "force" {
			instruction := commonadapter.ForceMatchedSkillInstruction(match.MatchedTerms)
			if strings.TrimSpace(instruction) != "" {
				if mergePromptText != nil {
					content = mergePromptText(instruction, content)
				} else {
					content = strings.TrimSpace(instruction) + "\n" + strings.TrimSpace(content)
				}
			}
		}
		texts = append(texts, skillInputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

// RenderAutoMatchedSkillPrompt renders matched-skill prompt using adapter-owned dependencies.
func (a *Adapter) RenderAutoMatchedSkillPrompt(agentID string, matches []autoMatchedSkillMatch) (string, int) {
	if a == nil {
		return "", 0
	}
	return renderAutoMatchedSkillPrompt(
		agentID,
		matches,
		a.readSkillContent,
		commonadapter.MergePromptText,
		commonadapter.SkillInputText,
	)
}

package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/skillutil"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func passthroughSkillInputText(_ string, content string) string {
	return content
}

// buildSelectedSkillPrompt reads skill contents and joins them into a prompt string.
func buildSelectedSkillPrompt(
	selectedSkills []string,
	readSkillContent func(skillName string) (string, error),
	skillInputText func(name, content string) string,
) (string, int) {
	if readSkillContent == nil {
		return "", 0
	}
	ordered := collectTrimmedUniqueValues(selectedSkills, func(value string) string {
		return strings.ToLower(value)
	})
	if len(ordered) == 0 {
		return "", 0
	}

	texts := make([]string, 0, len(ordered))
	inputText := skillInputText
	if inputText == nil {
		inputText = passthroughSkillInputText
	}
	for _, skillName := range ordered {
		content, err := readSkillContent(skillName)
		if err != nil {
			logger.Warn("turn/start: selected skill unavailable, skip",
				logger.FieldSkill, skillName,
				logger.FieldError, err,
			)
			continue
		}
		texts = append(texts, inputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

// resolveLSPUsagePromptHint resolves the user-configured LSP usage prompt hint.
func resolveLSPUsagePromptHint(
	ctx context.Context,
	defaultHint string,
	maxHintLen int,
	getPref func(context.Context, string) (any, error),
) string {
	if getPref == nil {
		return defaultHint
	}
	value, err := getPref(ctx, "lsp_usage_prompt_hint")
	if err != nil {
		logger.Warn("lsp hint: load preference failed", logger.FieldError, err)
		return defaultHint
	}
	hint := ""
	if s, ok := value.(string); ok {
		hint = strings.TrimSpace(s)
	}
	if hint == "" {
		return defaultHint
	}
	if maxHintLen > 0 && len(hint) > maxHintLen {
		logger.Warn("lsp hint: invalid preference fallback to default",
			"hint_len", len(hint), "max_len", maxHintLen)
		return defaultHint
	}
	return hint
}

// prependLSPAvailabilityWarning adds a warning when referenced LSP tools are unavailable.
func prependLSPAvailabilityWarning(
	hint string,
	dynamicToolNames map[string]struct{},
	collectReferencedToolNames func(string) []string,
	mergePromptText func(string, string) string,
) (string, []string) {
	collectRefs := collectReferencedToolNames
	if collectRefs == nil {
		return hint, nil
	}
	referenced := collectRefs(hint)
	if len(referenced) == 0 {
		return hint, nil
	}
	missing := make([]string, 0, len(referenced))
	for _, name := range referenced {
		if _, ok := dynamicToolNames[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 {
		return hint, nil
	}
	warning := "注意：当前会话未注入以下 LSP 工具（无可用 language server）：" +
		strings.Join(missing, ", ") +
		"。不要调用这些工具，请改用当前可用工具完成任务。"
	merge := mergePromptText
	if merge == nil {
		return warning + "\n" + hint, missing
	}
	return merge(warning, hint), missing
}

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

func collectAutoMatchedSkillMatches(
	prompt string,
	inputs []contracts.AutoMatchInput,
	configuredSkillNames []string,
	candidates []contracts.SkillMatchCandidate,
	options contracts.AutoSkillMatchOptions,
) []contracts.AutoMatchedSkillMatch {
	normalizedPrompt := strings.ToLower(strings.TrimSpace(prompt))
	if normalizedPrompt == "" || len(candidates) == 0 {
		return nil
	}
	inputSkillSet := skillutil.CollectInputSkillNames(
		inputs,
		func(input contracts.AutoMatchInput) string { return input.Type },
		func(input contracts.AutoMatchInput) string { return input.Name },
	)
	configuredSet := commonadapter.CollectSkillNameSet(configuredSkillNames)

	matches := make([]contracts.AutoMatchedSkillMatch, 0, len(candidates))
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
		matches = append(matches, contracts.AutoMatchedSkillMatch{
			Name:         skillName,
			MatchedBy:    matchedBy,
			MatchedTerms: matchedTerms,
		})
	}
	return matches
}

func renderAutoMatchedSkillPrompt(
	agentID string,
	matches []contracts.AutoMatchedSkillMatch,
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

func logAutoMatchedSkillReadError(agentID, skillName, matchedBy string, readErr error) {
	logger.Warn("turn/start: auto-matched skill unavailable, skip",
		append(threadLogFields(agentID),
			logger.FieldSkill, skillName,
			"matched_by", matchedBy,
			logger.FieldError, readErr,
		)...,
	)
}

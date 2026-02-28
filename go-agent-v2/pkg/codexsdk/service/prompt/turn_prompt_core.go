package prompt

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/support"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func buildSelectedSkillPrompt(
	selectedSkills []string,
	readSkillContent func(skillName string) (string, error),
	skillInputText func(name, content string) string,
) (string, int) {
	if readSkillContent == nil { return "", 0 }
	ordered := common.CollectTrimmedUniqueValues(selectedSkills, func(value string) string {
		return strings.ToLower(value)
	})
	if len(ordered) == 0 { return "", 0 }

	texts := make([]string, 0, len(ordered))
	if skillInputText == nil {
		skillInputText = func(_ string, content string) string { return content }
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
		texts = append(texts, skillInputText(skillName, content))
	}
	if len(texts) == 0 { return "", 0 }
	return strings.Join(texts, "\n"), len(texts)
}

func resolveLSPUsagePromptHint(
	ctx context.Context,
	defaultHint string,
	maxHintLen int,
	getPref func(context.Context, string) (any, error),
) string {
	if getPref == nil { return defaultHint }
	value, err := getPref(ctx, "lsp_usage_prompt_hint")
	if err != nil {
		logger.Warn("lsp hint: load preference failed", logger.FieldError, err)
		return defaultHint
	}
	hint, _ := value.(string)
	hint = strings.TrimSpace(hint)
	if hint == "" { return defaultHint }
	if maxHintLen > 0 && len(hint) > maxHintLen {
		logger.Warn("lsp hint: invalid preference fallback to default",
			"hint_len", len(hint), "max_len", maxHintLen)
		return defaultHint
	}
	return hint
}

func prependLSPAvailabilityWarning(
	hint string,
	dynamicToolNames map[string]struct{},
	collectReferencedToolNames func(string) []string,
	mergePromptText func(string, string) string,
) (string, []string) {
	if collectReferencedToolNames == nil { return hint, nil }
	referenced := collectReferencedToolNames(hint)
	if len(referenced) == 0 { return hint, nil }
	missing := make([]string, 0, len(referenced))
	for _, name := range referenced {
		if _, ok := dynamicToolNames[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 { return hint, nil }
	warning := "注意：当前会话未注入以下 LSP 工具（无可用 language server）：" + strings.Join(missing, ", ") + "。不要调用这些工具，请改用当前可用工具完成任务。"
	if mergePromptText == nil {
		return warning + "\n" + hint, missing
	}
	return mergePromptText(warning, hint), missing
}

type AutoMatchInput = agentcore.AutoMatchInput
type SkillMatchCandidate = agentcore.SkillMatchCandidate
type AutoMatchedSkillMatch = agentcore.AutoMatchedSkillMatch
type AutoSkillMatchOptions = agentcore.AutoSkillMatchOptions

func collectAutoMatchedSkillMatches(
	prompt string,
	inputs []AutoMatchInput,
	configuredSkillNames []string,
	candidates []SkillMatchCandidate,
	options AutoSkillMatchOptions,
) []AutoMatchedSkillMatch {
	normalizedPrompt := strings.ToLower(strings.TrimSpace(prompt))
	if normalizedPrompt == "" || len(candidates) == 0 {
		return nil
	}
	inputSkillSet := map[string]struct{}{}
	for _, input := range inputs {
		if !strings.EqualFold(strings.TrimSpace(input.Type), "skill") {
			continue
		}
		if name := strings.ToLower(strings.TrimSpace(input.Name)); name != "" {
			inputSkillSet[name] = struct{}{}
		}
	}
	configuredSet := map[string]struct{}{}
	for _, item := range configuredSkillNames {
		if name := strings.ToLower(strings.TrimSpace(item)); name != "" {
			configuredSet[name] = struct{}{}
		}
	}

	matches := make([]AutoMatchedSkillMatch, 0, len(candidates))
	for _, skill := range candidates {
		skillName := strings.TrimSpace(skill.Name)
		if skillName == "" {
			continue
		}
		skillNameLower := strings.ToLower(skillName)
		if _, exists := inputSkillSet[skillNameLower]; exists {
			continue
		}
		matchedBy, matchedTerms := support.ClassifyAutoSkillMatch(normalizedPrompt, skillName, skill.ForceWords, skill.TriggerWords)
		if matchedBy == "" {
			continue
		}
		if _, configured := configuredSet[skillNameLower]; configured {
			if !((matchedBy == "explicit" && options.IncludeConfiguredExplicit) ||
				(matchedBy == "force" && options.IncludeConfiguredForce)) {
				continue
			}
		}
		matches = append(matches, AutoMatchedSkillMatch{
			Name:         skillName,
			MatchedBy:    matchedBy,
			MatchedTerms: matchedTerms,
		})
	}
	return matches
}

func renderAutoMatchedSkillPrompt(
	agentID string,
	matches []AutoMatchedSkillMatch,
	readSkillContent func(skillName string) (string, error),
	mergePromptText func(prompt, extra string) string,
	skillInputText func(name, content string) string,
) (string, int) {
	if len(matches) == 0 || readSkillContent == nil || skillInputText == nil { return "", 0 }
	texts := make([]string, 0, len(matches))
	for _, match := range matches {
		skillName := strings.TrimSpace(match.Name)
		if skillName == "" {
			continue
		}
		content, err := readSkillContent(skillName)
		if err != nil {
			logger.Warn("turn/start: auto-matched skill unavailable, skip",
				append(common.ThreadLogFields(agentID),
					logger.FieldSkill, skillName,
					"matched_by", match.MatchedBy,
					logger.FieldError, err,
				)...,
			)
			continue
		}
		if match.MatchedBy == "force" {
			if instruction := strings.TrimSpace(support.ForceMatchedSkillInstruction(match.MatchedTerms)); instruction != "" {
				if mergePromptText != nil {
					content = mergePromptText(instruction, content)
				} else {
					content = instruction + "\n" + strings.TrimSpace(content)
				}
			}
		}
		texts = append(texts, skillInputText(skillName, content))
	}
	if len(texts) == 0 { return "", 0 }
	return strings.Join(texts, "\n"), len(texts)
}

var (
	BuildSelectedSkillPrompt       = buildSelectedSkillPrompt
	ResolveLSPUsagePromptHint      = resolveLSPUsagePromptHint
	CollectDynamicToolNames        = support.CollectDynamicToolNames
	PrependLSPAvailabilityWarning  = prependLSPAvailabilityWarning
	CollectAutoMatchedSkillMatches = collectAutoMatchedSkillMatches
	RenderAutoMatchedSkillPrompt   = renderAutoMatchedSkillPrompt
	CollectReferencedLSPToolNames  = support.CollectReferencedLSPToolNames
)

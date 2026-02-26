package prompt

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
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
	ordered := common.CollectTrimmedUniqueValues(selectedSkills, func(value string) string {
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

type AutoMatchInput struct {
	Type string
	Name string
}

type SkillMatchCandidate struct {
	Name         string
	ForceWords   []string
	TriggerWords []string
}

type AutoMatchedSkillMatch struct {
	Name         string
	MatchedBy    string
	MatchedTerms []string
}

type AutoSkillMatchOptions struct {
	IncludeConfiguredExplicit bool
	IncludeConfiguredForce    bool
}

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
	inputSkillSet := collectInputSkillNames(inputs)
	configuredSet := collectSkillNameSet(configuredSkillNames)

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
		matchedBy, matchedTerms := classifyAutoSkillMatch(normalizedPrompt, skillName, skill.ForceWords, skill.TriggerWords)
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
			instruction := forceMatchedSkillInstruction(match.MatchedTerms)
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
		append(common.ThreadLogFields(agentID),
			logger.FieldSkill, skillName,
			"matched_by", matchedBy,
			logger.FieldError, readErr,
		)...,
	)
}

func collectInputSkillNames(inputs []AutoMatchInput) map[string]struct{} {
	if len(inputs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if !strings.EqualFold(strings.TrimSpace(input.Type), "skill") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(input.Name))
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

func collectSkillNameSet(raw []string) map[string]struct{} {
	if len(raw) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

func lowerMatchedTerms(text string, candidates []string) []string {
	if text == "" || len(candidates) == 0 {
		return nil
	}
	terms := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, raw := range candidates {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		lowerCandidate := strings.ToLower(candidate)
		if _, ok := seen[lowerCandidate]; ok {
			continue
		}
		if !strings.Contains(text, lowerCandidate) {
			continue
		}
		seen[lowerCandidate] = struct{}{}
		terms = append(terms, candidate)
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
}

func explicitSkillMentionTerms(normalizedPrompt, skillName string, triggerWords []string) []string {
	trimmedName := strings.TrimSpace(skillName)
	candidates := make([]string, 0, 1+len(triggerWords))
	if trimmedName != "" {
		candidates = append(candidates, "@"+trimmedName)
		candidates = append(candidates, "[skill:"+trimmedName+"]")
	}
	for _, raw := range triggerWords {
		word := strings.TrimSpace(raw)
		if word == "" {
			continue
		}
		lowerWord := strings.ToLower(word)
		if strings.HasPrefix(lowerWord, "@") || strings.HasPrefix(lowerWord, "[skill:") {
			candidates = append(candidates, word)
		}
	}
	terms := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		lowerCandidate := strings.ToLower(strings.TrimSpace(candidate))
		if lowerCandidate == "" {
			continue
		}
		if _, exists := seen[lowerCandidate]; exists {
			continue
		}
		if !strings.Contains(normalizedPrompt, lowerCandidate) {
			continue
		}
		seen[lowerCandidate] = struct{}{}
		terms = append(terms, candidate)
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
}

func classifyAutoSkillMatch(normalizedPrompt, skillName string, forceWords, triggerWords []string) (string, []string) {
	forceTerms := lowerMatchedTerms(normalizedPrompt, forceWords)
	if len(forceTerms) > 0 {
		return "force", forceTerms
	}
	explicitTerms := explicitSkillMentionTerms(normalizedPrompt, skillName, triggerWords)
	if len(explicitTerms) > 0 {
		return "explicit", explicitTerms
	}
	triggerTerms := lowerMatchedTerms(normalizedPrompt, triggerWords)
	if len(triggerTerms) > 0 {
		return "trigger", triggerTerms
	}
	return "", nil
}

func forceMatchedSkillInstruction(matchedTerms []string) string {
	terms := make([]string, 0, len(matchedTerms))
	for _, raw := range matchedTerms {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		terms = append(terms, trimmed)
	}
	if len(terms) == 0 {
		return "执行要求: 本轮必须遵循该技能。"
	}
	return fmt.Sprintf("强制触发词: %s\n执行要求: 本轮必须遵循该技能。", strings.Join(terms, ", "))
}

var inlineCodeTokenPattern = regexp.MustCompile("`([^`\\n]+)`")

func CollectReferencedLSPToolNames(hint string) []string {
	trimmed := strings.TrimSpace(hint)
	if trimmed == "" {
		return nil
	}
	matches := inlineCodeTokenPattern.FindAllStringSubmatch(trimmed, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if !strings.HasPrefix(name, "lsp_") {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return names
}

func BuildSelectedSkillPrompt(
	selectedSkills []string,
	readSkillContent func(skillName string) (string, error),
	skillInputText func(name, content string) string,
) (string, int) {
	return buildSelectedSkillPrompt(selectedSkills, readSkillContent, skillInputText)
}

func ResolveLSPUsagePromptHint(
	ctx context.Context,
	defaultHint string,
	maxHintLen int,
	getPref func(context.Context, string) (any, error),
) string {
	return resolveLSPUsagePromptHint(ctx, defaultHint, maxHintLen, getPref)
}

func CollectDynamicToolNames(dynamicTools []agentcore.DynamicTool) map[string]struct{} {
	return collectDynamicToolNames(dynamicTools)
}

func PrependLSPAvailabilityWarning(
	hint string,
	dynamicToolNames map[string]struct{},
	collectReferencedToolNames func(string) []string,
	mergePromptText func(string, string) string,
) (string, []string) {
	return prependLSPAvailabilityWarning(hint, dynamicToolNames, collectReferencedToolNames, mergePromptText)
}

func CollectAutoMatchedSkillMatches(
	prompt string,
	inputs []AutoMatchInput,
	configuredSkillNames []string,
	candidates []SkillMatchCandidate,
	options AutoSkillMatchOptions,
) []AutoMatchedSkillMatch {
	return collectAutoMatchedSkillMatches(prompt, inputs, configuredSkillNames, candidates, options)
}

func RenderAutoMatchedSkillPrompt(
	agentID string,
	matches []AutoMatchedSkillMatch,
	readSkillContent func(skillName string) (string, error),
	mergePromptText func(prompt, extra string) string,
	skillInputText func(name, content string) string,
) (string, int) {
	return renderAutoMatchedSkillPrompt(agentID, matches, readSkillContent, mergePromptText, skillInputText)
}

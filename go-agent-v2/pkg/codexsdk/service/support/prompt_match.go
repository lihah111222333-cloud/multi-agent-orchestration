package support

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
)

func CollectDynamicToolNames(dynamicTools []agentcore.DynamicTool) map[string]struct{} {
	if len(dynamicTools) == 0 {
		return nil
	}
	toolNames := make(map[string]struct{}, len(dynamicTools))
	for _, tool := range dynamicTools {
		if name := strings.TrimSpace(tool.Name); name != "" {
			toolNames[name] = struct{}{}
		}
	}
	return toolNames
}

func LowerMatchedTerms(text string, candidates []string) []string {
	if text == "" || len(candidates) == 0 {
		return nil
	}
	text = strings.ToLower(text)
	unique := common.CollectTrimmedUniqueValues(candidates, strings.ToLower)
	var terms []string
	for _, candidate := range unique {
		if strings.Contains(text, strings.ToLower(candidate)) {
			terms = append(terms, candidate)
		}
	}
	return terms
}

func ExplicitSkillMentionTerms(normalizedPrompt, skillName string, triggerWords []string) []string {
	candidates := make([]string, 0, 2+len(triggerWords))
	if trimmedName := strings.TrimSpace(skillName); trimmedName != "" {
		candidates = append(candidates, "@"+trimmedName, "[skill:"+trimmedName+"]")
	}
	for _, raw := range triggerWords {
		word := strings.TrimSpace(raw)
		if word == "" {
			continue
		}
		if lowerWord := strings.ToLower(word); strings.HasPrefix(lowerWord, "@") || strings.HasPrefix(lowerWord, "[skill:") {
			candidates = append(candidates, word)
		}
	}
	return LowerMatchedTerms(normalizedPrompt, candidates)
}

func ClassifyAutoSkillMatch(normalizedPrompt, skillName string, forceWords, triggerWords []string) (string, []string) {
	if terms := LowerMatchedTerms(normalizedPrompt, forceWords); len(terms) > 0 {
		return "force", terms
	}
	if terms := ExplicitSkillMentionTerms(normalizedPrompt, skillName, triggerWords); len(terms) > 0 {
		return "explicit", terms
	}
	if terms := LowerMatchedTerms(normalizedPrompt, triggerWords); len(terms) > 0 {
		return "trigger", terms
	}
	return "", nil
}

func ForceMatchedSkillInstruction(matchedTerms []string) string {
	terms := common.CollectTrimmedUniqueValues(matchedTerms, strings.ToLower)
	if len(terms) == 0 { return "执行要求: 本轮必须遵循该技能。" }
	return fmt.Sprintf("强制触发词: %s\n执行要求: 本轮必须遵循该技能。", strings.Join(terms, ", "))
}

var inlineCodeTokenPattern = regexp.MustCompile("`([^`\\n]+)`")

func CollectReferencedLSPToolNames(hint string) []string {
	trimmed := strings.TrimSpace(hint)
	if trimmed == "" {
		return nil
	}
	matches := inlineCodeTokenPattern.FindAllStringSubmatch(trimmed, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if name := strings.TrimSpace(match[1]); strings.HasPrefix(name, "lsp_") {
			names = append(names, name)
		}
	}
	names = common.CollectTrimmedUniqueValues(names, nil)
	if len(names) > 1 { sort.Strings(names) }
	return names
}

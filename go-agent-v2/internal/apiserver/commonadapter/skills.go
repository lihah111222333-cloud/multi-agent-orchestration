package commonadapter

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/skillutil"
)

var inlineCodeTokenPattern = regexp.MustCompile("`([^`\\n]+)`")

func CollectSkillNameSet(raw []string) map[string]struct{} {
	if len(raw) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		if name := strings.ToLower(strings.TrimSpace(item)); name != "" {
			set[name] = struct{}{}
		}
	}
	return set
}

func matchedTerms(text string, candidates []string) []string {
	if text == "" || len(candidates) == 0 {
		return nil
	}
	terms := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, raw := range candidates {
		term := strings.TrimSpace(raw)
		if term == "" {
			continue
		}
		lowerTerm := strings.ToLower(term)
		if _, ok := seen[lowerTerm]; ok || !strings.Contains(text, lowerTerm) {
			continue
		}
		seen[lowerTerm] = struct{}{}
		terms = append(terms, term)
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
}

func LowerMatchedTerms(text string, candidates []string) []string {
	return matchedTerms(text, candidates)
}

func ExplicitSkillMentionTerms(normalizedPrompt, skillName string, triggerWords []string) []string {
	candidates := make([]string, 0, 2+len(triggerWords))
	if name := strings.TrimSpace(skillName); name != "" {
		candidates = append(candidates, "@"+name, "[skill:"+name+"]")
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
	return matchedTerms(normalizedPrompt, candidates)
}

func ClassifyAutoSkillMatch(normalizedPrompt, skillName string, forceWords, triggerWords []string) (string, []string) {
	forceTerms := LowerMatchedTerms(normalizedPrompt, forceWords)
	if len(forceTerms) > 0 {
		return "force", forceTerms
	}
	explicitTerms := ExplicitSkillMentionTerms(normalizedPrompt, skillName, triggerWords)
	if len(explicitTerms) > 0 {
		return "explicit", explicitTerms
	}
	triggerTerms := LowerMatchedTerms(normalizedPrompt, triggerWords)
	if len(triggerTerms) > 0 {
		return "trigger", triggerTerms
	}
	return "", nil
}

func ForceMatchedSkillInstruction(matchedTerms []string) string {
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

func NormalizeSkillName(raw string) (string, error) {
	return skillutil.NormalizeName(raw)
}

func NormalizeSkillNames(rawNames []string) ([]string, error) {
	if len(rawNames) == 0 {
		return []string{}, nil
	}
	names := make([]string, 0, len(rawNames))
	seen := make(map[string]struct{}, len(rawNames))
	for _, raw := range rawNames {
		name, err := skillutil.NormalizeName(raw)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}

func CollectReferencedLSPToolNames(hint string) []string {
	if hint = strings.TrimSpace(hint); hint == "" {
		return nil
	}
	matches := inlineCodeTokenPattern.FindAllStringSubmatch(hint, -1)
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

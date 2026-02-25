package commonadapter

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/skillutil"
)

var inlineCodeTokenPattern = regexp.MustCompile("`([^`\\n]+)`")

// CollectSkillNameSet normalizes a string list into lowercase unique set.
func (a *Adapter) CollectSkillNameSet(raw []string) map[string]struct{} {
	return CollectSkillNameSet(raw)
}

// CollectSkillNameSet normalizes a string list into lowercase unique set.
func CollectSkillNameSet(raw []string) map[string]struct{} {
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

// LowerMatchedTerms filters trigger terms that are included in prompt text.
func (a *Adapter) LowerMatchedTerms(text string, candidates []string) []string {
	return LowerMatchedTerms(text, candidates)
}

// LowerMatchedTerms filters trigger terms that are included in prompt text.
func LowerMatchedTerms(text string, candidates []string) []string {
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

// ExplicitSkillMentionTerms extracts explicit @skill and [skill:*] mentions.
func (a *Adapter) ExplicitSkillMentionTerms(normalizedPrompt, skillName string, triggerWords []string) []string {
	return ExplicitSkillMentionTerms(normalizedPrompt, skillName, triggerWords)
}

// ExplicitSkillMentionTerms extracts explicit @skill and [skill:*] mentions.
func ExplicitSkillMentionTerms(normalizedPrompt, skillName string, triggerWords []string) []string {
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

// ClassifyAutoSkillMatch classifies prompt/skill match source and matched terms.
func (a *Adapter) ClassifyAutoSkillMatch(normalizedPrompt, skillName string, forceWords, triggerWords []string) (string, []string) {
	return ClassifyAutoSkillMatch(normalizedPrompt, skillName, forceWords, triggerWords)
}

// ClassifyAutoSkillMatch classifies prompt/skill match source and matched terms.
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

// ForceMatchedSkillInstruction builds instruction prefix for force-matched skills.
func (a *Adapter) ForceMatchedSkillInstruction(matchedTerms []string) string {
	return ForceMatchedSkillInstruction(matchedTerms)
}

// ForceMatchedSkillInstruction builds instruction prefix for force-matched skills.
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

// NormalizeSkillName validates skill name.
func (a *Adapter) NormalizeSkillName(raw string) (string, error) {
	return NormalizeSkillName(raw)
}

// NormalizeSkillName validates skill name.
func NormalizeSkillName(raw string) (string, error) {
	return skillutil.NormalizeName(raw)
}

// NormalizeSkillNames deduplicates and validates skill name list.
func (a *Adapter) NormalizeSkillNames(rawNames []string) ([]string, error) {
	return NormalizeSkillNames(rawNames)
}

// NormalizeSkillNames deduplicates and validates skill name list.
func NormalizeSkillNames(rawNames []string) ([]string, error) {
	if len(rawNames) == 0 {
		return []string{}, nil
	}
	names := make([]string, 0, len(rawNames))
	seen := make(map[string]struct{}, len(rawNames))
	for _, raw := range rawNames {
		name, err := NormalizeSkillName(raw)
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

// CollectReferencedLSPToolNames extracts referenced lsp_* tool names from markdown inline code.
func (a *Adapter) CollectReferencedLSPToolNames(hint string) []string {
	return CollectReferencedLSPToolNames(hint)
}

// CollectReferencedLSPToolNames extracts referenced lsp_* tool names from markdown inline code.
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

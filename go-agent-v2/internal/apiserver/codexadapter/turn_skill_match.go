package codexadapter

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/skillutil"
)

type AutoMatchInput = contracts.AutoMatchInput
type SkillMatchCandidate = contracts.SkillMatchCandidate
type AutoMatchedSkillMatch = contracts.AutoMatchedSkillMatch
type AutoSkillMatchOptions = contracts.AutoSkillMatchOptions

// CollectAutoMatchedSkillMatches classifies force/explicit matched skills for turn prompt injection.
func CollectAutoMatchedSkillMatches(
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
	inputSkillSet := skillutil.CollectInputSkillNames(
		inputs,
		func(input AutoMatchInput) string { return input.Type },
		func(input AutoMatchInput) string { return input.Name },
	)
	configuredSet := commonadapter.CollectSkillNameSet(configuredSkillNames)

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
		matches = append(matches, AutoMatchedSkillMatch{
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
	inputs []AutoMatchInput,
	configuredSkillNames []string,
	candidates []SkillMatchCandidate,
	options AutoSkillMatchOptions,
) []AutoMatchedSkillMatch {
	return CollectAutoMatchedSkillMatches(prompt, inputs, configuredSkillNames, candidates, options)
}

type RenderAutoMatchedSkillPromptOptions struct {
	Matches          []AutoMatchedSkillMatch
	ReadSkillContent func(skillName string) (string, error)
	MergePromptText  func(prompt, extra string) string
	SkillInputText   func(name, content string) string
	OnReadSkillError func(skillName, matchedBy string, err error)
}

// RenderAutoMatchedSkillPrompt renders skill prompt payload for matched skills.
func RenderAutoMatchedSkillPrompt(opt RenderAutoMatchedSkillPromptOptions) (string, int) {
	if len(opt.Matches) == 0 || opt.ReadSkillContent == nil || opt.SkillInputText == nil {
		return "", 0
	}

	texts := make([]string, 0, len(opt.Matches))
	for _, match := range opt.Matches {
		skillName := strings.TrimSpace(match.Name)
		if skillName == "" {
			continue
		}
		content, err := opt.ReadSkillContent(skillName)
		if err != nil {
			if opt.OnReadSkillError != nil {
				opt.OnReadSkillError(skillName, match.MatchedBy, err)
			}
			continue
		}
		if match.MatchedBy == "force" {
			instruction := commonadapter.ForceMatchedSkillInstruction(match.MatchedTerms)
			if strings.TrimSpace(instruction) != "" {
				if opt.MergePromptText != nil {
					content = opt.MergePromptText(instruction, content)
				} else {
					content = strings.TrimSpace(instruction) + "\n" + strings.TrimSpace(content)
				}
			}
		}
		texts = append(texts, opt.SkillInputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

// RenderAutoMatchedSkillPrompt renders matched-skill prompt using adapter-owned dependencies.
func (a *Adapter) RenderAutoMatchedSkillPrompt(matches []AutoMatchedSkillMatch, onReadSkillError func(skillName, matchedBy string, err error)) (string, int) {
	if a == nil {
		return "", 0
	}
	return RenderAutoMatchedSkillPrompt(RenderAutoMatchedSkillPromptOptions{
		Matches:          matches,
		ReadSkillContent: a.readSkillContent,
		MergePromptText:  commonadapter.MergePromptText,
		SkillInputText:   commonadapter.SkillInputText,
		OnReadSkillError: onReadSkillError,
	})
}

package codexadapter

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (a *Adapter) buildTurnSkillPrompt(
	threadID,
	prompt string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (string, int, int) {
	selectedSkillPrompt, selectedSkillCount := a.BuildSelectedSkillPrompt(selectedSkills)
	if manualSkillSelection || selectedSkillCount > 0 {
		return selectedSkillPrompt, selectedSkillCount, 0
	}
	autoSkillPrompt, autoSkillCount := a.buildForcedOrExplicitMatchedSkillPrompt(threadID, prompt, input)
	return commonadapter.MergePromptText(selectedSkillPrompt, autoSkillPrompt), selectedSkillCount, autoSkillCount
}

func (a *Adapter) buildForcedOrExplicitMatchedSkillPrompt(agentID, prompt string, input []contracts.TurnInput) (string, int) {
	matches := a.collectAutoMatchedSkillMatches(agentID, prompt, input, AutoSkillMatchOptions{
		IncludeConfiguredExplicit: true,
		IncludeConfiguredForce:    true,
	})
	if len(matches) == 0 {
		return "", 0
	}
	filtered := make([]AutoMatchedSkillMatch, 0, len(matches))
	for _, match := range matches {
		switch match.MatchedBy {
		case "force", "explicit":
			filtered = append(filtered, match)
		}
	}
	return a.renderAutoMatchedSkillPrompt(agentID, filtered)
}

func (a *Adapter) collectAutoMatchedSkillMatches(agentID, prompt string, input []contracts.TurnInput, options AutoSkillMatchOptions) []AutoMatchedSkillMatch {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	candidates, err := a.listSkillMatchCandidates()
	if err != nil {
		logger.Warn("skills/auto-match: list skills failed",
			logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
			logger.FieldError, err,
		)
		return nil
	}
	if len(candidates) == 0 {
		return nil
	}
	return a.CollectAutoMatchedSkillMatches(
		prompt,
		buildAutoMatchInputs(input),
		a.listAgentSkills(agentID),
		candidates,
		options,
	)
}

// CollectAutoMatchedSkillMatchesForThread evaluates auto-match candidates for one thread.
func (a *Adapter) CollectAutoMatchedSkillMatchesForThread(
	threadID string,
	prompt string,
	input []contracts.TurnInput,
	options AutoSkillMatchOptions,
) []AutoMatchedSkillMatch {
	return a.collectAutoMatchedSkillMatches(threadID, prompt, input, options)
}

func buildAutoMatchInputs(input []contracts.TurnInput) []AutoMatchInput {
	if len(input) == 0 {
		return nil
	}
	out := make([]AutoMatchInput, 0, len(input))
	for _, item := range input {
		out = append(out, AutoMatchInput{
			Type: item.Type,
			Name: item.Name,
		})
	}
	return out
}

func (a *Adapter) renderAutoMatchedSkillPrompt(agentID string, matches []AutoMatchedSkillMatch) (string, int) {
	if len(matches) == 0 {
		return "", 0
	}
	return a.RenderAutoMatchedSkillPrompt(agentID, matches)
}

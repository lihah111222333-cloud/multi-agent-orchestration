package codexadapter

import (
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
)

func (a *Adapter) prepareTurnStartSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (contracts.TurnStartEntryPrepareResult, error) {
	prompt, images, files := ExtractTurnInputs(input)
	skillPrompt, selectedSkillCount, autoMatchedSkillCount := a.buildTurnSkillPrompt(threadID, prompt, input, selectedSkills, manualSkillSelection)
	submitPrompt := commonadapter.MergePromptText(prompt, skillPrompt)
	return contracts.TurnStartEntryPrepareResult{
		Prompt:                prompt,
		SubmitPrompt:          submitPrompt,
		Images:                images,
		Files:                 files,
		SelectedSkillCount:    selectedSkillCount,
		AutoMatchedSkillCount: autoMatchedSkillCount,
	}, nil
}

func (a *Adapter) prepareTurnSteerSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (contracts.TurnSteerEntryPrepareResult, error) {
	prompt, images, files := ExtractTurnInputs(input)
	skillPrompt, _, _ := a.buildTurnSkillPrompt(threadID, prompt, input, selectedSkills, manualSkillSelection)
	submitPrompt := commonadapter.MergePromptText(prompt, skillPrompt)
	return contracts.TurnSteerEntryPrepareResult{
		SubmitPrompt: submitPrompt,
		Images:       images,
		Files:        files,
	}, nil
}

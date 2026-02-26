package codexadapter

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	serviceruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
)

type turnStartPreparedSubmission struct {
	Prompt                string
	SubmitPrompt          string
	Images                []string
	Files                 []string
	TimelineAttachments   []uistate.TimelineAttachment
	SelectedSkillCount    int
	AutoMatchedSkillCount int
}

type parsedTurnInputs struct {
	Prompt              string
	Images              []string
	Files               []string
	TimelineAttachments []uistate.TimelineAttachment
}

type turnPreparedSubmissionCommon struct {
	parsed                parsedTurnInputs
	submitPrompt          string
	selectedSkillCount    int
	autoMatchedSkillCount int
}

func (a *Adapter) prepareTurnSubmissionCommon(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) turnPreparedSubmissionCommon {
	prepared := serviceruntime.PrepareTurnSubmissionCommon(
		newServiceRuntimeBridge(a),
		threadID,
		toRuntimeTurnInputs(input),
		selectedSkills,
		manualSkillSelection,
	)
	return fromRuntimePreparedSubmissionCommon(prepared)
}

func (a *Adapter) prepareTurnStartSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (turnStartPreparedSubmission, error) {
	prepared, err := serviceruntime.PrepareTurnStartSubmission(
		newServiceRuntimeBridge(a),
		threadID,
		toRuntimeTurnInputs(input),
		selectedSkills,
		manualSkillSelection,
	)
	if err != nil {
		return turnStartPreparedSubmission{}, err
	}
	return fromRuntimeTurnStartPreparedSubmission(prepared), nil
}

func (a *Adapter) prepareTurnSteerSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (contracts.TurnSteerEntryPrepareResult, error) {
	prepared, err := serviceruntime.PrepareTurnSteerSubmission(
		newServiceRuntimeBridge(a),
		threadID,
		toRuntimeTurnInputs(input),
		selectedSkills,
		manualSkillSelection,
	)
	if err != nil {
		return contracts.TurnSteerEntryPrepareResult{}, err
	}
	return contracts.TurnSteerEntryPrepareResult{
		SubmitPrompt: prepared.SubmitPrompt,
		Images:       prepared.Images,
		Files:        prepared.Files,
	}, nil
}

func (a *Adapter) resolveTurnSteerAlignment(req turnSteerRequest) (string, string, error) {
	return serviceruntime.ResolveTurnSteerAlignment(newServiceRuntimeBridge(a), toRuntimeTurnSteerRequest(req))
}

func parseTurnInputs(inputs []contracts.TurnInput) parsedTurnInputs {
	parsed := serviceruntime.ParseTurnInputs(toRuntimeTurnInputs(inputs), commonadapter.FileContentInputText)
	return fromRuntimeParsedTurnInputs(parsed)
}

func appendImageTimelineAttachment(
	attachments []uistate.TimelineAttachment,
	name string,
	path string,
	preview string,
) []uistate.TimelineAttachment {
	return append(attachments, uistate.TimelineAttachment{
		Kind:       "image",
		Name:       name,
		Path:       path,
		PreviewURL: serviceruntime.BuildAttachmentPreviewURL(preview),
	})
}

func appendFileTimelineAttachment(
	attachments []uistate.TimelineAttachment,
	name string,
	path string,
) []uistate.TimelineAttachment {
	return append(attachments, uistate.TimelineAttachment{
		Kind: "file",
		Name: name,
		Path: path,
	})
}

func extractTurnInputs(inputs []contracts.TurnInput) (prompt string, images, files []string) {
	return serviceruntime.ExtractTurnInputs(toRuntimeTurnInputs(inputs), commonadapter.FileContentInputText)
}

func buildUserTimelineAttachments(images, files []string) []uistate.TimelineAttachment {
	return fromRuntimeTimelineAttachments(serviceruntime.BuildUserTimelineAttachments(images, files))
}

func buildUserTimelineAttachmentsFromInputs(inputs []contracts.TurnInput) []uistate.TimelineAttachment {
	return fromRuntimeTimelineAttachments(serviceruntime.BuildUserTimelineAttachmentsFromInputs(
		toRuntimeTurnInputs(inputs),
		commonadapter.FileContentInputText,
	))
}

func (a *Adapter) appendTurnStartUserTimeline(
	ctx context.Context,
	attachments []uistate.TimelineAttachment,
	opt contracts.TurnAppendUserTimelineOptions,
) {
	serviceruntime.AppendTurnStartUserTimeline(
		newServiceRuntimeBridge(a),
		ctx,
		toRuntimeTimelineAttachments(attachments),
		serviceruntime.TurnAppendUserTimelineOptions{
			ThreadID:     opt.ThreadID,
			Prompt:       opt.Prompt,
			SubmitPrompt: opt.SubmitPrompt,
			Images:       opt.Images,
			Files:        opt.Files,
		},
	)
}

func (a *Adapter) threadTimelineAlreadyShowsInjectedPrompt(threadID string) bool {
	return serviceruntime.ThreadTimelineAlreadyShowsInjectedPrompt(newServiceRuntimeBridge(a), threadID)
}

func composeUserTimelineTextForTurn(prompt, submitPrompt, injectedHint string, showInjected bool) string {
	return serviceruntime.ComposeUserTimelineTextForTurn(prompt, submitPrompt, injectedHint, showInjected, commonadapter.MergePromptText)
}

func (a *Adapter) buildTurnSkillPrompt(
	threadID,
	prompt string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (string, int, int) {
	return serviceruntime.BuildTurnSkillPrompt(
		newServiceRuntimeBridge(a),
		threadID,
		prompt,
		toRuntimeTurnInputs(input),
		selectedSkills,
		manualSkillSelection,
	)
}

func (a *Adapter) buildForcedOrExplicitMatchedSkillPrompt(agentID, prompt string, input []contracts.TurnInput) (string, int) {
	return serviceruntime.BuildForcedOrExplicitMatchedSkillPrompt(newServiceRuntimeBridge(a), agentID, prompt, toRuntimeTurnInputs(input))
}

func (a *Adapter) collectAutoMatchedSkillMatches(agentID, prompt string, input []contracts.TurnInput, options contracts.AutoSkillMatchOptions) []autoMatchedSkillMatch {
	matches := serviceruntime.CollectAutoMatchedSkillMatches(
		newServiceRuntimeBridge(a),
		agentID,
		prompt,
		toRuntimeTurnInputs(input),
		toRuntimeAutoSkillMatchOptions(options),
	)
	return fromRuntimeAutoMatchedSkillMatches(matches)
}

func (a *Adapter) CollectAutoMatchedSkillMatchesForThread(
	threadID string,
	prompt string,
	input []contracts.TurnInput,
	options contracts.AutoSkillMatchOptions,
) []autoMatchedSkillMatch {
	matches := serviceruntime.CollectAutoMatchedSkillMatchesForThread(
		newServiceRuntimeBridge(a),
		threadID,
		prompt,
		toRuntimeTurnInputs(input),
		toRuntimeAutoSkillMatchOptions(options),
	)
	return fromRuntimeAutoMatchedSkillMatches(matches)
}

func buildAutoMatchInputs(input []contracts.TurnInput) []autoMatchInput {
	return fromRuntimeAutoMatchInputs(serviceruntime.BuildAutoMatchInputs(toRuntimeTurnInputs(input)))
}

func (a *Adapter) renderAutoMatchedSkillPrompt(agentID string, matches []autoMatchedSkillMatch) (string, int) {
	return serviceruntime.RenderAutoMatchedSkillPrompt(newServiceRuntimeBridge(a), agentID, toRuntimeAutoMatchedSkillMatches(matches))
}

func fromRuntimeParsedTurnInputs(parsed serviceruntime.ParsedTurnInputs) parsedTurnInputs {
	return parsedTurnInputs{
		Prompt:              parsed.Prompt,
		Images:              parsed.Images,
		Files:               parsed.Files,
		TimelineAttachments: fromRuntimeTimelineAttachments(parsed.TimelineAttachments),
	}
}

func fromRuntimePreparedSubmissionCommon(prepared serviceruntime.PreparedSubmissionCommon) turnPreparedSubmissionCommon {
	return turnPreparedSubmissionCommon{
		parsed:                fromRuntimeParsedTurnInputs(prepared.Parsed),
		submitPrompt:          prepared.SubmitPrompt,
		selectedSkillCount:    prepared.SelectedSkillCount,
		autoMatchedSkillCount: prepared.AutoMatchedSkillCount,
	}
}

func fromRuntimeTurnStartPreparedSubmission(prepared serviceruntime.TurnStartPreparedSubmission) turnStartPreparedSubmission {
	return turnStartPreparedSubmission{
		Prompt:                prepared.Prompt,
		SubmitPrompt:          prepared.SubmitPrompt,
		Images:                prepared.Images,
		Files:                 prepared.Files,
		TimelineAttachments:   fromRuntimeTimelineAttachments(prepared.TimelineAttachments),
		SelectedSkillCount:    prepared.SelectedSkillCount,
		AutoMatchedSkillCount: prepared.AutoMatchedSkillCount,
	}
}

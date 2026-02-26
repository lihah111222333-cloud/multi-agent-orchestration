package codexadapter

import (
	"context"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
	"strings"
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
	parsed := parseTurnInputs(input)
	skillPrompt, selectedSkillCount, autoMatchedSkillCount := a.buildTurnSkillPrompt(threadID, parsed.Prompt, input, selectedSkills, manualSkillSelection)
	return turnPreparedSubmissionCommon{
		parsed:                parsed,
		submitPrompt:          commonadapter.MergePromptText(parsed.Prompt, skillPrompt),
		selectedSkillCount:    selectedSkillCount,
		autoMatchedSkillCount: autoMatchedSkillCount,
	}
}

func (a *Adapter) prepareTurnStartSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (turnStartPreparedSubmission, error) {
	prepared := a.prepareTurnSubmissionCommon(threadID, input, selectedSkills, manualSkillSelection)
	return turnStartPreparedSubmission{
		Prompt:                prepared.parsed.Prompt,
		SubmitPrompt:          prepared.submitPrompt,
		Images:                prepared.parsed.Images,
		Files:                 prepared.parsed.Files,
		TimelineAttachments:   prepared.parsed.TimelineAttachments,
		SelectedSkillCount:    prepared.selectedSkillCount,
		AutoMatchedSkillCount: prepared.autoMatchedSkillCount,
	}, nil
}

func (a *Adapter) prepareTurnSteerSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (contracts.TurnSteerEntryPrepareResult, error) {
	prepared := a.prepareTurnSubmissionCommon(threadID, input, selectedSkills, manualSkillSelection)
	return contracts.TurnSteerEntryPrepareResult{
		SubmitPrompt: prepared.submitPrompt,
		Images:       prepared.parsed.Images,
		Files:        prepared.parsed.Files,
	}, nil
}

func (a *Adapter) resolveTurnSteerAlignment(req turnSteerRequest) (string, string, error) {
	threadID, err := requireThreadID("Server.turnSteer", req.ThreadID)
	if err != nil {
		return "", "", err
	}
	expectedTurnID := strings.TrimSpace(req.ExpectedTurnID)
	if expectedTurnID == "" {
		return "", "", apperrors.New("Server.turnSteer", "expectedTurnId must not be empty")
	}
	activeTurnID, hasActiveTurn := a.activeTrackedTurnID(threadID)
	if !hasActiveTurn {
		return "", "", apperrors.New("Server.turnSteer", "no active turn to steer")
	}
	if !strings.EqualFold(expectedTurnID, activeTurnID) {
		return "", "", apperrors.Newf(
			"Server.turnSteer",
			"expectedTurnId mismatch: expected %s, active %s",
			expectedTurnID,
			activeTurnID,
		)
	}
	return threadID, activeTurnID, nil
}

func (a *Adapter) turnSteerFromInputAligned(req turnSteerRequest) (map[string]any, error) {
	_, activeTurnID, err := a.resolveTurnSteerAlignment(req)
	if err != nil {
		return nil, err
	}
	result, err := a.TurnSteerFromInput(req)
	if err != nil {
		return nil, err
	}
	return ensureTurnSteerResultTurnID(result, activeTurnID), nil
}

func parseTurnInputs(inputs []contracts.TurnInput) parsedTurnInputs {
	if len(inputs) == 0 {
		return parsedTurnInputs{}
	}
	texts := make([]string, 0, len(inputs))
	images := make([]string, 0, len(inputs))
	files := make([]string, 0, len(inputs))
	attachments := make([]uistate.TimelineAttachment, 0, len(inputs))

	for _, inp := range inputs {
		switch strings.ToLower(strings.TrimSpace(inp.Type)) {
		case "text":
			text := util.StripLeadingSystemNoise(inp.Text)
			if strings.TrimSpace(text) != "" {
				texts = append(texts, text)
			}
		case "image":
			image := strings.TrimSpace(inp.URL)
			if image == "" {
				image = strings.TrimSpace(inp.Path)
			}
			if image == "" {
				continue
			}
			images = append(images, image)
			attachments = appendImageTimelineAttachment(attachments, buildAttachmentName(image), image, image)
		case "localimage":
			imagePath := strings.TrimSpace(inp.Path)
			preview := strings.TrimSpace(inp.URL)
			if util.IsRemoteImageURL(preview) {
				images = append(images, preview)
			} else if imagePath != "" {
				images = append(images, imagePath)
			}
			if preview == "" {
				preview = imagePath
			}
			if preview == "" {
				continue
			}
			nameSource := imagePath
			if nameSource == "" {
				nameSource = preview
			}
			attachments = appendImageTimelineAttachment(attachments, buildAttachmentName(nameSource), imagePath, preview)
		case "filecontent":
			path := strings.TrimSpace(inp.Path)
			if path != "" {
				files = append(files, path)
				attachments = appendFileTimelineAttachment(attachments, buildAttachmentName(path), path)
				continue
			}
			if inline := commonadapter.FileContentInputText(inp.Name, inp.Content); inline != "" {
				texts = append(texts, inline)
			}
			if strings.TrimSpace(inp.Content) == "" {
				continue
			}
			name := strings.TrimSpace(inp.Name)
			if name == "" {
				name = "inline-file"
			}
			attachments = appendFileTimelineAttachment(attachments, name, "")
		case "mention", "file":
			path := strings.TrimSpace(inp.Path)
			if path == "" {
				continue
			}
			files = append(files, path)
			attachments = appendFileTimelineAttachment(attachments, buildAttachmentName(path), path)
		case "skill":
			// 技能注入统一由 selectedSkills 处理，避免透传输入中的摘要内容。
		}
	}

	return parsedTurnInputs{
		Prompt:              strings.Join(texts, "\n"),
		Images:              images,
		Files:               files,
		TimelineAttachments: attachments,
	}
}

func appendImageTimelineAttachment(
	attachments []uistate.TimelineAttachment,
	name string,
	path string,
	preview string,
) []uistate.TimelineAttachment {
	previewURL := util.BuildAttachmentPreviewURL(preview)
	return append(attachments, uistate.TimelineAttachment{
		Kind:       "image",
		Name:       name,
		Path:       path,
		PreviewURL: previewURL,
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
	parsed := parseTurnInputs(inputs)
	return parsed.Prompt, parsed.Images, parsed.Files
}

func buildUserTimelineAttachments(images, files []string) []uistate.TimelineAttachment {
	attachments := make([]uistate.TimelineAttachment, 0, len(images)+len(files))
	for _, raw := range images {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = appendImageTimelineAttachment(attachments, buildAttachmentName(path), path, path)
	}
	for _, raw := range files {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = appendFileTimelineAttachment(attachments, buildAttachmentName(path), path)
	}
	return attachments
}

func buildUserTimelineAttachmentsFromInputs(inputs []contracts.TurnInput) []uistate.TimelineAttachment {
	parsed := parseTurnInputs(inputs)
	if len(parsed.TimelineAttachments) == 0 {
		return nil
	}
	return append([]uistate.TimelineAttachment(nil), parsed.TimelineAttachments...)
}

func (a *Adapter) appendTurnStartUserTimeline(
	ctx context.Context,
	attachments []uistate.TimelineAttachment,
	opt contracts.TurnAppendUserTimelineOptions,
) {
	uiRuntime := a.uiRuntime()
	if uiRuntime == nil {
		return
	}
	if len(attachments) == 0 {
		attachments = buildUserTimelineAttachments(opt.Images, opt.Files)
	}
	showInjected := a.showInjectedPromptInChat(ctx)
	appendInjectedHint := showInjected && !a.threadTimelineAlreadyShowsInjectedPrompt(opt.ThreadID)
	injectedHint := ""
	if appendInjectedHint {
		injectedHint = a.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen)
	}
	timelineText := composeUserTimelineTextForTurn(opt.Prompt, opt.SubmitPrompt, injectedHint, showInjected)
	uiRuntime.AppendUserMessage(opt.ThreadID, timelineText, attachments)
}

func (a *Adapter) threadTimelineAlreadyShowsInjectedPrompt(threadID string) bool {
	uiRuntime := a.uiRuntime()
	if uiRuntime == nil {
		return false
	}
	const marker = "\n已注入"
	for _, item := range uiRuntime.ThreadTimeline(threadID) {
		if item.Kind != "user" {
			continue
		}
		if strings.Contains(item.Text, marker) {
			return true
		}
	}
	return false
}

func composeUserTimelineTextForTurn(prompt, submitPrompt, injectedHint string, showInjected bool) string {
	if !showInjected {
		return prompt
	}
	hint := strings.TrimSpace(injectedHint)
	if hint == "" {
		return submitPrompt
	}
	if strings.Contains(submitPrompt, hint) {
		return submitPrompt
	}
	return commonadapter.MergePromptText(submitPrompt, hint)
}

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
	matches := a.collectAutoMatchedSkillMatches(agentID, prompt, input, contracts.AutoSkillMatchOptions{
		IncludeConfiguredExplicit: true,
		IncludeConfiguredForce:    true,
	})
	if len(matches) == 0 {
		return "", 0
	}
	filtered := make([]autoMatchedSkillMatch, 0, len(matches))
	for _, match := range matches {
		switch match.MatchedBy {
		case "force", "explicit":
			filtered = append(filtered, match)
		}
	}
	return a.renderAutoMatchedSkillPrompt(agentID, filtered)
}

func (a *Adapter) collectAutoMatchedSkillMatches(agentID, prompt string, input []contracts.TurnInput, options contracts.AutoSkillMatchOptions) []autoMatchedSkillMatch {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	candidates, err := a.listSkillMatchCandidates()
	if err != nil {
		logger.Warn("skills/auto-match: list skills failed",
			append(threadLogFields(agentID), logger.FieldError, err)...,
		)
		return nil
	}
	if len(candidates) == 0 {
		return nil
	}
	return collectAutoMatchedSkillMatches(
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
	options contracts.AutoSkillMatchOptions,
) []autoMatchedSkillMatch {
	return a.collectAutoMatchedSkillMatches(threadID, prompt, input, options)
}

func buildAutoMatchInputs(input []contracts.TurnInput) []autoMatchInput {
	if len(input) == 0 {
		return nil
	}
	out := make([]autoMatchInput, 0, len(input))
	for _, item := range input {
		out = append(out, autoMatchInput{
			Type: item.Type,
			Name: item.Name,
		})
	}
	return out
}

func (a *Adapter) renderAutoMatchedSkillPrompt(agentID string, matches []autoMatchedSkillMatch) (string, int) {
	if len(matches) == 0 {
		return "", 0
	}
	return a.RenderAutoMatchedSkillPrompt(agentID, matches)
}

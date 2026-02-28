package runtime

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func EnsureTurnSteerResultTurnID(result map[string]any, activeTurnID string) map[string]any {
	if result == nil {
		result = map[string]any{}
	}
	if currentID, _ := result["turnId"].(string); strings.TrimSpace(currentID) == "" {
		result["turnId"] = strings.TrimSpace(activeTurnID)
	}
	return result
}

func PrepareTurnStartSubmission(a PrepareAdapter, threadID string, input []TurnInput, selectedSkills []string, manualSkillSelection bool) (TurnStartPreparedSubmission, error) {
	a = normalizePrepareAdapter(a)
	prepared := prepareTurnSubmissionCommon(a, threadID, input, selectedSkills, manualSkillSelection)
	return TurnStartPreparedSubmission{
		Prompt: prepared.Parsed.Prompt, SubmitPrompt: prepared.SubmitPrompt, Images: prepared.Parsed.Images, Files: prepared.Parsed.Files,
		TimelineAttachments: prepared.Parsed.TimelineAttachments, SelectedSkillCount: prepared.SelectedSkillCount, AutoMatchedSkillCount: prepared.AutoMatchedSkillCount,
	}, nil
}

func PrepareTurnSteerSubmission(a PrepareAdapter, threadID string, input []TurnInput, selectedSkills []string, manualSkillSelection bool) (agentcore.TurnSteerEntryPrepareResult, error) {
	a = normalizePrepareAdapter(a)
	prepared := prepareTurnSubmissionCommon(a, threadID, input, selectedSkills, manualSkillSelection)
	return agentcore.TurnSteerEntryPrepareResult{SubmitPrompt: prepared.SubmitPrompt, Images: prepared.Parsed.Images, Files: prepared.Parsed.Files}, nil
}

func ResolveTurnSteerAlignment(a PrepareAdapter, req agentcore.TurnSteerRequest) (string, string, error) {
	a = normalizePrepareAdapter(a)
	threadID, err := a.RequireThreadID("Server.turnSteer", req.ThreadID)
	if err != nil {
		return "", "", err
	}
	expectedTurnID := strings.TrimSpace(req.ExpectedTurnID)
	if expectedTurnID == "" {
		return "", "", a.NewError("Server.turnSteer", "expectedTurnId must not be empty")
	}
	activeTurnID, hasActiveTurn := a.ActiveTrackedTurnID(threadID)
	if !hasActiveTurn {
		return "", "", a.NewError("Server.turnSteer", "no active turn to steer")
	}
	if !strings.EqualFold(expectedTurnID, activeTurnID) {
		return "", "", a.NewErrorf("Server.turnSteer", "expectedTurnId mismatch: expected %s, active %s", expectedTurnID, activeTurnID)
	}
	return threadID, activeTurnID, nil
}

func TurnSteerFromInputAligned(req agentcore.TurnSteerRequest, resolve func(agentcore.TurnSteerRequest) (string, string, error), turnSteerFromInput func(agentcore.TurnSteerRequest) (map[string]any, error)) (map[string]any, error) {
	if resolve == nil {
		return nil, nil
	}
	_, activeTurnID, err := resolve(req)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if turnSteerFromInput != nil {
		result, err = turnSteerFromInput(req)
		if err != nil {
			return nil, err
		}
	}
	return EnsureTurnSteerResultTurnID(result, activeTurnID), nil
}

func AppendTurnStartUserTimeline(a PrepareAdapter, ctx context.Context, attachments []TimelineAttachment, opt agentcore.TurnAppendUserTimelineOptions) {
	a = normalizePrepareAdapter(a)
	uiRuntime := a.UIRuntime()
	if uiRuntime == nil {
		return
	}
	if len(attachments) == 0 {
		attachments = BuildUserTimelineAttachments(opt.Images, opt.Files, a.BuildAttachmentName, a.BuildAttachmentPreviewURL)
	}
	showInjected := a.ShowInjectedPromptInChat(ctx)
	appendInjectedHint := showInjected && !ThreadTimelineAlreadyShowsInjectedPrompt(a, opt.ThreadID)
	injectedHint := ""
	if appendInjectedHint {
		injectedHint = a.ResolveLSPUsagePromptHint(ctx, a.DefaultLSPUsagePromptHint(), a.MaxLSPUsagePromptHintLen())
	}
	timelineText := ComposeUserTimelineTextForTurn(opt.Prompt, opt.SubmitPrompt, injectedHint, showInjected, a.MergePromptText)
	uiRuntime.AppendUserMessage(opt.ThreadID, timelineText, attachments)
}

func ThreadTimelineAlreadyShowsInjectedPrompt(a PrepareAdapter, threadID string) bool {
	a = normalizePrepareAdapter(a)
	uiRuntime := a.UIRuntime()
	if uiRuntime == nil {
		return false
	}
	const marker = "\n已注入"
	for _, item := range uiRuntime.ThreadTimeline(threadID) {
		if item.Kind == "user" && strings.Contains(item.Text, marker) {
			return true
		}
	}
	return false
}

func CollectAutoMatchedSkillMatchesForThread(a PrepareAdapter, threadID string, prompt string, input []TurnInput, options AutoSkillMatchOptions) []AutoMatchedSkillMatch {
	return collectAutoMatchedSkillMatches(normalizePrepareAdapter(a), threadID, prompt, input, options)
}

func prepareTurnSubmissionCommon(a PrepareAdapter, threadID string, input []TurnInput, selectedSkills []string, manualSkillSelection bool) PreparedSubmissionCommon {
	parsed := ParseTurnInputs(input, a.FileContentInputText, a.BuildAttachmentName, a.BuildAttachmentPreviewURL)
	selectedSkillPrompt, selectedSkillCount := a.BuildSelectedSkillPrompt(selectedSkills)
	autoSkillPrompt, autoMatchedSkillCount := "", 0
	if !manualSkillSelection && selectedSkillCount == 0 {
		autoSkillPrompt, autoMatchedSkillCount = buildForcedOrExplicitMatchedSkillPrompt(a, threadID, parsed.Prompt, input)
	}
	skillPrompt := a.MergePromptText(selectedSkillPrompt, autoSkillPrompt)
	return PreparedSubmissionCommon{Parsed: parsed, SubmitPrompt: a.MergePromptText(parsed.Prompt, skillPrompt), SelectedSkillCount: selectedSkillCount, AutoMatchedSkillCount: autoMatchedSkillCount}
}

func buildForcedOrExplicitMatchedSkillPrompt(a PrepareAdapter, agentID, prompt string, input []TurnInput) (string, int) {
	matches := collectAutoMatchedSkillMatches(a, agentID, prompt, input, AutoSkillMatchOptions{IncludeConfiguredExplicit: true, IncludeConfiguredForce: true})
	if len(matches) == 0 {
		return "", 0
	}
	filtered := matches[:0]
	for _, match := range matches {
		if match.MatchedBy == "force" || match.MatchedBy == "explicit" {
			filtered = append(filtered, match)
		}
	}
	if len(filtered) == 0 {
		return "", 0
	}
	return a.RenderAutoMatchedSkillPrompt(agentID, filtered)
}

func collectAutoMatchedSkillMatches(a PrepareAdapter, agentID, prompt string, input []TurnInput, options AutoSkillMatchOptions) []AutoMatchedSkillMatch {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	candidates, err := a.ListSkillMatchCandidates()
	if err != nil || len(candidates) == 0 {
		if err != nil {
			logger.Warn("skills/auto-match: list skills failed", logger.FieldError, err, "agent_id", agentID)
		}
		return nil
	}
	autoMatchInputs := make([]AutoMatchInput, 0, len(input))
	for _, item := range input {
		autoMatchInputs = append(autoMatchInputs, AutoMatchInput{Type: item.Type, Name: item.Name})
	}
	return a.CollectAutoMatchedSkillMatches(prompt, autoMatchInputs, a.ListAgentSkills(agentID), candidates, options)
}

func ParseTurnInputs(inputs []TurnInput, fileContentInputText func(string, string) string, buildAttachmentName func(string) string, buildAttachmentPreviewURL func(string) string) ParsedTurnInputs {
	if len(inputs) == 0 {
		return ParsedTurnInputs{}
	}
	buildAttachmentName, buildAttachmentPreviewURL = normalizeAttachmentBuilders(buildAttachmentName, buildAttachmentPreviewURL)
	texts := make([]string, 0, len(inputs))
	images := make([]string, 0, len(inputs))
	files := make([]string, 0, len(inputs))
	attachments := make([]TimelineAttachment, 0, len(inputs))

	for _, inp := range inputs {
		switch strings.ToLower(strings.TrimSpace(inp.Type)) {
		case "text":
			if text := util.StripLeadingSystemNoise(inp.Text); strings.TrimSpace(text) != "" {
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
			attachments = appendImageTimelineAttachment(attachments, buildAttachmentName, buildAttachmentPreviewURL, image, image, image)
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
			attachments = appendImageTimelineAttachment(attachments, buildAttachmentName, buildAttachmentPreviewURL, nameSource, imagePath, preview)
		case "filecontent":
			path := strings.TrimSpace(inp.Path)
			if path != "" {
				files = append(files, path)
				attachments = appendFileTimelineAttachment(attachments, buildAttachmentName(path), path)
				continue
			}
			if fileContentInputText != nil {
				if inline := fileContentInputText(inp.Name, inp.Content); inline != "" {
					texts = append(texts, inline)
				}
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
		}
	}

	return ParsedTurnInputs{Prompt: strings.Join(texts, "\n"), Images: images, Files: files, TimelineAttachments: attachments}
}

func appendImageTimelineAttachment(attachments []TimelineAttachment, buildAttachmentName func(string) string, buildAttachmentPreviewURL func(string) string, nameSource string, path string, preview string) []TimelineAttachment {
	return append(attachments, TimelineAttachment{Kind: "image", Name: buildAttachmentName(nameSource), Path: path, PreviewURL: buildAttachmentPreviewURL(preview)})
}

func appendFileTimelineAttachment(attachments []TimelineAttachment, name string, path string) []TimelineAttachment {
	return append(attachments, TimelineAttachment{Kind: "file", Name: name, Path: path})
}

func BuildUserTimelineAttachments(images, files []string, buildAttachmentName func(string) string, buildAttachmentPreviewURL func(string) string) []TimelineAttachment {
	buildAttachmentName, buildAttachmentPreviewURL = normalizeAttachmentBuilders(buildAttachmentName, buildAttachmentPreviewURL)
	attachments := make([]TimelineAttachment, 0, len(images)+len(files))
	for _, raw := range images {
		if path := strings.TrimSpace(raw); path != "" {
			attachments = appendImageTimelineAttachment(attachments, buildAttachmentName, buildAttachmentPreviewURL, path, path, path)
		}
	}
	for _, raw := range files {
		if path := strings.TrimSpace(raw); path != "" {
			attachments = appendFileTimelineAttachment(attachments, buildAttachmentName(path), path)
		}
	}
	return attachments
}

func ComposeUserTimelineTextForTurn(prompt, submitPrompt, injectedHint string, showInjected bool, mergePromptText func(string, string) string) string {
	if !showInjected {
		return prompt
	}
	hint := strings.TrimSpace(injectedHint)
	if hint == "" || strings.Contains(submitPrompt, hint) {
		return submitPrompt
	}
	if mergePromptText != nil {
		return mergePromptText(submitPrompt, hint)
	}
	if strings.TrimSpace(submitPrompt) == "" {
		return hint
	}
	return submitPrompt + "\n" + hint
}

func normalizeAttachmentBuilders(buildAttachmentName func(string) string, buildAttachmentPreviewURL func(string) string) (func(string) string, func(string) string) {
	if buildAttachmentName == nil {
		buildAttachmentName = strings.TrimSpace
	}
	if buildAttachmentPreviewURL == nil {
		buildAttachmentPreviewURL = strings.TrimSpace
	}
	return buildAttachmentName, buildAttachmentPreviewURL
}

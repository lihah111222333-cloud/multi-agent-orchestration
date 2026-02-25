package codexadapter

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func (a *Adapter) prepareTurnStartSubmission(
	threadID string,
	input []TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (TurnStartEntryPrepareResult, error) {
	prompt, images, files := ExtractTurnInputs(input)
	skillPrompt, selectedSkillCount, autoMatchedSkillCount := a.buildTurnSkillPrompt(threadID, prompt, input, selectedSkills, manualSkillSelection)
	submitPrompt := commonadapter.MergePromptText(prompt, skillPrompt)
	return TurnStartEntryPrepareResult{
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
	input []TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (TurnSteerEntryPrepareResult, error) {
	prompt, images, files := ExtractTurnInputs(input)
	skillPrompt, _, _ := a.buildTurnSkillPrompt(threadID, prompt, input, selectedSkills, manualSkillSelection)
	submitPrompt := commonadapter.MergePromptText(prompt, skillPrompt)
	return TurnSteerEntryPrepareResult{
		SubmitPrompt: submitPrompt,
		Images:       images,
		Files:        files,
	}, nil
}

func (a *Adapter) buildTurnSkillPrompt(
	threadID,
	prompt string,
	input []TurnInput,
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

func (a *Adapter) buildForcedOrExplicitMatchedSkillPrompt(agentID, prompt string, input []TurnInput) (string, int) {
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

func (a *Adapter) collectAutoMatchedSkillMatches(agentID, prompt string, input []TurnInput, options AutoSkillMatchOptions) []AutoMatchedSkillMatch {
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
	input []TurnInput,
	options AutoSkillMatchOptions,
) []AutoMatchedSkillMatch {
	return a.collectAutoMatchedSkillMatches(threadID, prompt, input, options)
}

func buildAutoMatchInputs(input []TurnInput) []AutoMatchInput {
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
	onReadSkillError := func(skillName, matchedBy string, readErr error) {
		logger.Warn("turn/start: auto-matched skill unavailable, skip",
			logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
			logger.FieldSkill, skillName,
			"matched_by", matchedBy,
			logger.FieldError, readErr,
		)
	}
	return a.RenderAutoMatchedSkillPrompt(matches, onReadSkillError)
}

func (a *Adapter) appendTurnStartUserTimeline(ctx context.Context, input []TurnInput, opt TurnAppendUserTimelineOptions) {
	if a == nil || a.ctx == nil || a.ctx.UIRuntime() == nil {
		return
	}
	attachments := BuildUserTimelineAttachmentsFromInputs(input)
	if len(attachments) == 0 {
		attachments = BuildUserTimelineAttachments(opt.Images, opt.Files)
	}
	showInjected := a.showInjectedPromptInChat(ctx)
	appendInjectedHint := showInjected && !a.threadTimelineAlreadyShowsInjectedPrompt(opt.ThreadID)
	injectedHint := ""
	if appendInjectedHint {
		injectedHint = a.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen)
	}
	timelineText := ComposeUserTimelineTextForTurn(opt.Prompt, opt.SubmitPrompt, injectedHint, showInjected)
	a.ctx.UIRuntime().AppendUserMessage(opt.ThreadID, timelineText, attachments)
}

func (a *Adapter) threadTimelineAlreadyShowsInjectedPrompt(threadID string) bool {
	if a == nil || a.ctx == nil || a.ctx.UIRuntime() == nil {
		return false
	}
	const marker = "\n已注入"
	for _, item := range a.ctx.UIRuntime().ThreadTimeline(threadID) {
		if item.Kind != "user" {
			continue
		}
		if strings.Contains(item.Text, marker) {
			return true
		}
	}
	return false
}

func ComposeUserTimelineTextForTurn(prompt, submitPrompt, injectedHint string, showInjected bool) string {
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

func ExtractTurnInputs(inputs []TurnInput) (prompt string, images, files []string) {
	var texts []string
	isRemoteImageURL := func(raw string) bool {
		value := strings.ToLower(strings.TrimSpace(raw))
		return strings.HasPrefix(value, "http://") ||
			strings.HasPrefix(value, "https://") ||
			strings.HasPrefix(value, "data:image/")
	}
	for _, inp := range inputs {
		switch strings.ToLower(strings.TrimSpace(inp.Type)) {
		case "text":
			text := util.StripLeadingSystemNoise(inp.Text)
			if strings.TrimSpace(text) != "" {
				texts = append(texts, text)
			}
		case "image":
			if value := strings.TrimSpace(inp.URL); value != "" {
				images = append(images, value)
				continue
			}
			if value := strings.TrimSpace(inp.Path); value != "" {
				images = append(images, value)
			}
		case "localimage":
			if value := strings.TrimSpace(inp.URL); isRemoteImageURL(value) {
				images = append(images, value)
				continue
			}
			if value := strings.TrimSpace(inp.Path); value != "" {
				images = append(images, value)
			}
		case "filecontent":
			if value := strings.TrimSpace(inp.Path); value != "" {
				files = append(files, value)
				continue
			}
			if inline := commonadapter.FileContentInputText(inp.Name, inp.Content); inline != "" {
				texts = append(texts, inline)
			}
		case "mention", "file":
			if value := strings.TrimSpace(inp.Path); value != "" {
				files = append(files, value)
			}
		case "skill":
			// 技能注入统一由 selectedSkills 处理，避免透传输入中的摘要内容。
			continue
		}
	}
	prompt = strings.Join(texts, "\n")
	return
}

func BuildAttachmentName(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if ext, ok := strings.CutPrefix(lower, "data:image/"); ok {
		ext = strings.TrimSpace(ext)
		if idx := strings.Index(ext, ";"); idx >= 0 {
			ext = ext[:idx]
		}
		ext = strings.TrimSpace(ext)
		if ext == "" {
			return "image"
		}
		return "image." + ext
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if parsed, err := url.Parse(value); err == nil {
			base := strings.TrimSpace(filepath.Base(parsed.Path))
			if base != "" && base != "." && base != string(filepath.Separator) {
				return base
			}
		}
		return value
	}
	base := strings.TrimSpace(filepath.Base(value))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return value
	}
	return base
}

func BuildAttachmentPreviewURL(path string) string {
	return util.BuildAttachmentPreviewURL(path)
}

func BuildUserTimelineAttachments(images, files []string) []uistate.TimelineAttachment {
	attachments := make([]uistate.TimelineAttachment, 0, len(images)+len(files))
	for _, raw := range images {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = append(attachments, uistate.TimelineAttachment{
			Kind:       "image",
			Name:       BuildAttachmentName(path),
			Path:       path,
			PreviewURL: BuildAttachmentPreviewURL(path),
		})
	}
	for _, raw := range files {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = append(attachments, uistate.TimelineAttachment{
			Kind: "file",
			Name: BuildAttachmentName(path),
			Path: path,
		})
	}
	return attachments
}

func BuildUserTimelineAttachmentsFromInputs(inputs []TurnInput) []uistate.TimelineAttachment {
	if len(inputs) == 0 {
		return nil
	}
	attachments := make([]uistate.TimelineAttachment, 0, len(inputs))
	for _, input := range inputs {
		kind := strings.ToLower(strings.TrimSpace(input.Type))
		switch kind {
		case "image":
			imageURL := strings.TrimSpace(input.URL)
			if imageURL == "" {
				imageURL = strings.TrimSpace(input.Path)
			}
			if imageURL == "" {
				continue
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind:       "image",
				Name:       BuildAttachmentName(imageURL),
				Path:       imageURL,
				PreviewURL: BuildAttachmentPreviewURL(imageURL),
			})
		case "localimage":
			imagePath := strings.TrimSpace(input.Path)
			preview := strings.TrimSpace(input.URL)
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
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind:       "image",
				Name:       BuildAttachmentName(nameSource),
				Path:       imagePath,
				PreviewURL: BuildAttachmentPreviewURL(preview),
			})
		case "mention", "file":
			path := strings.TrimSpace(input.Path)
			if path == "" {
				continue
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind: "file",
				Name: BuildAttachmentName(path),
				Path: path,
			})
		case "filecontent":
			path := strings.TrimSpace(input.Path)
			if path != "" {
				attachments = append(attachments, uistate.TimelineAttachment{
					Kind: "file",
					Name: BuildAttachmentName(path),
					Path: path,
				})
				continue
			}
			if strings.TrimSpace(input.Content) == "" {
				continue
			}
			name := strings.TrimSpace(input.Name)
			if name == "" {
				name = "inline-file"
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind: "file",
				Name: name,
			})
		}
	}
	return attachments
}

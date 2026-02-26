package codexadapter

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func ensureTurnSteerResultTurnID(result map[string]any, activeTurnID string) map[string]any {
	if result == nil {
		result = map[string]any{}
	}
	if currentID, _ := result["turnId"].(string); strings.TrimSpace(currentID) == "" {
		result["turnId"] = strings.TrimSpace(activeTurnID)
	}
	return result
}

func buildAttachmentName(path string) string {
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

// buildAttachmentPreviewURL preserves compatibility for apiserver helper call sites.
func buildAttachmentPreviewURL(path string) string {
	return util.BuildAttachmentPreviewURL(path)
}

func prepareTurnSubmissionCommonLogic(
	a *Adapter,
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) turnPreparedSubmissionCommon {
	parsed := parseTurnInputs(input)
	skillPrompt, selectedSkillCount, autoMatchedSkillCount := buildTurnSkillPromptLogic(
		a,
		threadID,
		parsed.Prompt,
		input,
		selectedSkills,
		manualSkillSelection,
	)
	return turnPreparedSubmissionCommon{
		parsed:                parsed,
		submitPrompt:          commonadapter.MergePromptText(parsed.Prompt, skillPrompt),
		selectedSkillCount:    selectedSkillCount,
		autoMatchedSkillCount: autoMatchedSkillCount,
	}
}

func prepareTurnStartSubmissionLogic(
	a *Adapter,
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (turnStartPreparedSubmission, error) {
	prepared := prepareTurnSubmissionCommonLogic(a, threadID, input, selectedSkills, manualSkillSelection)
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

func prepareTurnSteerSubmissionLogic(
	a *Adapter,
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (contracts.TurnSteerEntryPrepareResult, error) {
	prepared := prepareTurnSubmissionCommonLogic(a, threadID, input, selectedSkills, manualSkillSelection)
	return contracts.TurnSteerEntryPrepareResult{
		SubmitPrompt: prepared.submitPrompt,
		Images:       prepared.parsed.Images,
		Files:        prepared.parsed.Files,
	}, nil
}

func resolveTurnSteerAlignmentLogic(a *Adapter, req turnSteerRequest) (string, string, error) {
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

func turnSteerFromInputAlignedLogic(a *Adapter, req turnSteerRequest) (map[string]any, error) {
	_, activeTurnID, err := resolveTurnSteerAlignmentLogic(a, req)
	if err != nil {
		return nil, err
	}
	result, err := turnSteerFromInputLogic(a, req)
	if err != nil {
		return nil, err
	}
	return ensureTurnSteerResultTurnID(result, activeTurnID), nil
}

func appendTurnStartUserTimelineLogic(
	a *Adapter,
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
	appendInjectedHint := showInjected && !threadTimelineAlreadyShowsInjectedPromptLogic(a, opt.ThreadID)
	injectedHint := ""
	if appendInjectedHint {
		injectedHint = a.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen)
	}
	timelineText := composeUserTimelineTextForTurn(opt.Prompt, opt.SubmitPrompt, injectedHint, showInjected)
	uiRuntime.AppendUserMessage(opt.ThreadID, timelineText, attachments)
}

func threadTimelineAlreadyShowsInjectedPromptLogic(a *Adapter, threadID string) bool {
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

func buildTurnSkillPromptLogic(
	a *Adapter,
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
	autoSkillPrompt, autoSkillCount := buildForcedOrExplicitMatchedSkillPromptLogic(a, threadID, prompt, input)
	return commonadapter.MergePromptText(selectedSkillPrompt, autoSkillPrompt), selectedSkillCount, autoSkillCount
}

func buildForcedOrExplicitMatchedSkillPromptLogic(
	a *Adapter,
	agentID,
	prompt string,
	input []contracts.TurnInput,
) (string, int) {
	matches := collectAutoMatchedSkillMatchesLogic(a, agentID, prompt, input, contracts.AutoSkillMatchOptions{
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
	return renderAutoMatchedSkillPromptLogic(a, agentID, filtered)
}

func collectAutoMatchedSkillMatchesLogic(
	a *Adapter,
	agentID,
	prompt string,
	input []contracts.TurnInput,
	options contracts.AutoSkillMatchOptions,
) []autoMatchedSkillMatch {
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

func collectAutoMatchedSkillMatchesForThreadLogic(
	a *Adapter,
	threadID string,
	prompt string,
	input []contracts.TurnInput,
	options contracts.AutoSkillMatchOptions,
) []autoMatchedSkillMatch {
	return collectAutoMatchedSkillMatchesLogic(a, threadID, prompt, input, options)
}

func renderAutoMatchedSkillPromptLogic(
	a *Adapter,
	agentID string,
	matches []autoMatchedSkillMatch,
) (string, int) {
	if len(matches) == 0 {
		return "", 0
	}
	return a.RenderAutoMatchedSkillPrompt(agentID, matches)
}

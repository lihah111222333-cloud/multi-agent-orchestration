// methods_turn.go — turn/* / review / fuzzySearch JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/skillutil"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// UserInput 用户输入 (支持多种类型)。
type UserInput struct {
	Type    string `json:"type"`              // text, image, localImage, skill, mention, fileContent
	Text    string `json:"text,omitempty"`    // type=text
	URL     string `json:"url,omitempty"`     // type=image
	Path    string `json:"path,omitempty"`    // type=localImage/mention/fileContent
	Name    string `json:"name,omitempty"`    // type=skill/mention
	Content string `json:"content,omitempty"` // type=skill/fileContent
}

type turnStartParams struct {
	ThreadID             string          `json:"threadId"`
	Input                []UserInput     `json:"input"`
	SelectedSkills       []string        `json:"selectedSkills,omitempty"`
	ManualSkillSelection bool            `json:"manualSkillSelection,omitempty"`
	Cwd                  string          `json:"cwd,omitempty"`
	ApprovalPolicy       string          `json:"approvalPolicy,omitempty"`
	Model                string          `json:"model,omitempty"`
	OutputSchema         json.RawMessage `json:"outputSchema,omitempty"`
}

// turnInfo 通用 turn 信息。
type turnInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// turnStartResponse turn/start 响应。
type turnStartResponse struct {
	Turn turnInfo `json:"turn"`
}

var (
	_ = (*Server).buildConfiguredSkillPrompt
	_ = (*Server).buildAutoMatchedSkillPrompt
	_ = (*Server).waitInterruptSettled
)

func (s *Server) skillInputText(name, content string) string {
	adapter := (*commonadapter.Adapter)(nil)
	if s != nil {
		adapter = s.commonAdapter
	}
	if adapter == nil {
		adapter = commonadapter.New()
	}
	return adapter.SkillInputText(name, content)
}

func collectInputSkillNames(inputs []UserInput) map[string]struct{} {
	return skillutil.CollectInputSkillNames(
		inputs,
		func(input UserInput) string { return input.Type },
		func(input UserInput) string { return input.Name },
	)
}

func collectSkillNameSet(raw []string) map[string]struct{} {
	return commonadapter.CollectSkillNameSet(raw)
}

func (s *Server) mergePromptText(prompt, extra string) string {
	adapter := (*commonadapter.Adapter)(nil)
	if s != nil {
		adapter = s.commonAdapter
	}
	if adapter == nil {
		adapter = commonadapter.New()
	}
	return adapter.MergePromptText(prompt, extra)
}

// composeUserTimelineTextForTurn 组装 UI timeline 中展示的 user 文本。
//
// 默认仅展示用户原始输入；当调试开关开启时，展示提交给后端的文本并附带注入提示词，
// 便于排查“自动注入”是否符合预期。
func (s *Server) composeUserTimelineTextForTurn(prompt, submitPrompt, injectedHint string, showInjected bool) string {
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
	return s.mergePromptText(submitPrompt, hint)
}

func (s *Server) threadTimelineAlreadyShowsInjectedPrompt(threadID string) bool {
	if s == nil || s.uiRuntime == nil {
		return false
	}
	const marker = "\n已注入"
	for _, item := range s.uiRuntime.ThreadTimeline(threadID) {
		if item.Kind != "user" {
			continue
		}
		if strings.Contains(item.Text, marker) {
			return true
		}
	}
	return false
}

func validateLSPUsagePromptHint(hint string) error {
	if len(hint) > maxLSPUsagePromptHintLen {
		return apperrors.Newf("Server.configLSPPromptHintWrite", "hint length exceeds %d", maxLSPUsagePromptHintLen)
	}
	return nil
}

func (s *Server) resolveLSPUsagePromptHint(ctx context.Context) string {
	if s != nil && s.codexAdapter != nil {
		return s.codexAdapter.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen)
	}
	return defaultLSPUsagePromptHint
}

func (s *Server) prependLSPAvailabilityWarning(hint string, dynamicTools []agentcore.DynamicTool) (string, []string) {
	if s != nil && s.codexAdapter != nil {
		return s.codexAdapter.PrependLSPAvailabilityWarning(hint, dynamicTools, s.mergePromptText)
	}
	return hint, nil
}

func (s *Server) resolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []agentcore.DynamicTool) string {
	hint := s.resolveLSPUsagePromptHint(ctx)
	instructions, missing := s.prependLSPAvailabilityWarning(hint, dynamicTools)
	if len(missing) == 0 {
		return instructions
	}
	logger.Warn("lsp hint references unavailable tools during launch",
		"missing_lsp_tools", strings.Join(missing, ","),
	)
	return instructions
}

func (s *Server) buildConfiguredSkillPrompt(agentID string, input []UserInput) (string, int) {
	_ = agentID
	_ = input
	return "", 0
}

func (s *Server) buildSelectedSkillPrompt(selectedSkills []string) (string, int) {
	if s != nil && s.codexAdapter != nil {
		return s.codexAdapter.BuildSelectedSkillPrompt(selectedSkills)
	}
	return "", 0
}

func (s *Server) buildTurnSkillPrompt(threadID, prompt string, input []UserInput, selectedSkills []string, manualSkillSelection bool) (string, int, int) {
	selectedSkillPrompt, selectedSkillCount := s.buildSelectedSkillPrompt(selectedSkills)
	if manualSkillSelection || selectedSkillCount > 0 {
		return selectedSkillPrompt, selectedSkillCount, 0
	}
	autoSkillPrompt, autoSkillCount := s.buildForcedOrExplicitMatchedSkillPrompt(threadID, prompt, input)
	return s.mergePromptText(selectedSkillPrompt, autoSkillPrompt), selectedSkillCount, autoSkillCount
}

func buildAutoMatchInputs(input []UserInput) []contracts.AutoMatchInput {
	if len(input) == 0 {
		return nil
	}
	out := make([]contracts.AutoMatchInput, 0, len(input))
	for _, item := range input {
		out = append(out, contracts.AutoMatchInput{
			Type: item.Type,
			Name: item.Name,
		})
	}
	return out
}

func (s *Server) collectAutoMatchedSkillMatches(agentID, prompt string, input []UserInput, options contracts.AutoSkillMatchOptions) []contracts.AutoMatchedSkillMatch {
	if s.skillSvc == nil {
		return nil
	}
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	allSkills, err := s.skillSvc.ListSkills()
	if err != nil {
		logger.Warn("skills/auto-match: list skills failed",
			logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
			logger.FieldError, err,
		)
		return nil
	}
	if len(allSkills) == 0 {
		return nil
	}
	candidates := make([]contracts.SkillMatchCandidate, 0, len(allSkills))
	for _, skill := range allSkills {
		skillName := strings.TrimSpace(skill.Name)
		if skillName == "" {
			continue
		}
		candidates = append(candidates, contracts.SkillMatchCandidate{
			Name:         skillName,
			ForceWords:   append([]string(nil), skill.ForceWords...),
			TriggerWords: append([]string(nil), skill.TriggerWords...),
		})
	}
	if s == nil || s.codexAdapter == nil {
		return nil
	}
	return s.codexAdapter.CollectAutoMatchedSkillMatches(
		prompt,
		buildAutoMatchInputs(input),
		s.GetAgentSkills(agentID),
		candidates,
		options,
	)
}

func (s *Server) buildAutoMatchedSkillPrompt(agentID, prompt string, input []UserInput) (string, int) {
	matches := s.collectAutoMatchedSkillMatches(agentID, prompt, input, contracts.AutoSkillMatchOptions{
		IncludeConfiguredForce: true,
	})
	return s.renderAutoMatchedSkillPrompt(agentID, matches)
}

func (s *Server) buildForcedOrExplicitMatchedSkillPrompt(agentID, prompt string, input []UserInput) (string, int) {
	matches := s.collectAutoMatchedSkillMatches(agentID, prompt, input, contracts.AutoSkillMatchOptions{
		IncludeConfiguredExplicit: true,
		IncludeConfiguredForce:    true,
	})
	if len(matches) == 0 {
		return "", 0
	}
	filtered := make([]contracts.AutoMatchedSkillMatch, 0, len(matches))
	for _, match := range matches {
		switch match.MatchedBy {
		case "force", "explicit":
			filtered = append(filtered, match)
		}
	}
	return s.renderAutoMatchedSkillPrompt(agentID, filtered)
}

func (s *Server) renderAutoMatchedSkillPrompt(agentID string, matches []contracts.AutoMatchedSkillMatch) (string, int) {
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
	if s != nil && s.codexAdapter != nil {
		return s.codexAdapter.RenderAutoMatchedSkillPrompt(matches, onReadSkillError)
	}
	return "", 0
}

func toCodexTurnInputs(input []UserInput) []contracts.TurnInput {
	if len(input) == 0 {
		return nil
	}
	out := make([]contracts.TurnInput, 0, len(input))
	for _, item := range input {
		out = append(out, contracts.TurnInput{
			Type:    item.Type,
			Text:    item.Text,
			URL:     item.URL,
			Path:    item.Path,
			Name:    item.Name,
			Content: item.Content,
		})
	}
	return out
}

func toUserInputs(input []contracts.TurnInput) []UserInput {
	if len(input) == 0 {
		return nil
	}
	out := make([]UserInput, 0, len(input))
	for _, item := range input {
		out = append(out, UserInput{
			Type:    item.Type,
			Text:    item.Text,
			URL:     item.URL,
			Path:    item.Path,
			Name:    item.Name,
			Content: item.Content,
		})
	}
	return out
}

func (s *Server) prepareTurnStartSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (contracts.TurnStartEntryPrepareResult, error) {
	userInputs := toUserInputs(input)
	prompt, images, files := s.extractInputs(userInputs)
	skillPrompt, selectedSkillCount, autoMatchedSkillCount := s.buildTurnSkillPrompt(threadID, prompt, userInputs, selectedSkills, manualSkillSelection)
	submitPrompt := s.mergePromptText(prompt, skillPrompt)
	return contracts.TurnStartEntryPrepareResult{
		Prompt:                prompt,
		SubmitPrompt:          submitPrompt,
		Images:                images,
		Files:                 files,
		SelectedSkillCount:    selectedSkillCount,
		AutoMatchedSkillCount: autoMatchedSkillCount,
	}, nil
}

// PrepareTurnStartSubmission exposes turn/start preparation for codexadapter context integration.
func (s *Server) PrepareTurnStartSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (contracts.TurnStartEntryPrepareResult, error) {
	return s.prepareTurnStartSubmission(threadID, input, selectedSkills, manualSkillSelection)
}

func (s *Server) appendTurnStartUserTimeline(ctx context.Context, input []contracts.TurnInput, opt contracts.TurnAppendUserTimelineOptions) {
	if s.uiRuntime == nil {
		return
	}
	attachments := buildUserTimelineAttachmentsFromInputs(toUserInputs(input))
	if len(attachments) == 0 {
		attachments = buildUserTimelineAttachments(opt.Images, opt.Files)
	}
	showInjected := s.showInjectedPromptInChat(ctx)
	appendInjectedHint := showInjected && !s.threadTimelineAlreadyShowsInjectedPrompt(opt.ThreadID)
	injectedHint := ""
	if appendInjectedHint {
		injectedHint = s.resolveLSPUsagePromptHint(ctx)
	}
	timelineText := s.composeUserTimelineTextForTurn(opt.Prompt, opt.SubmitPrompt, injectedHint, showInjected)
	s.uiRuntime.AppendUserMessage(opt.ThreadID, timelineText, attachments)
}

// AppendTurnStartUserTimeline exposes turn/start timeline append for codexadapter context integration.
func (s *Server) AppendTurnStartUserTimeline(ctx context.Context, input []contracts.TurnInput, opt contracts.TurnAppendUserTimelineOptions) {
	s.appendTurnStartUserTimeline(ctx, input, opt)
}

func (s *Server) turnStartTyped(ctx context.Context, p turnStartParams) (any, error) {
	startResult, err := s.codexAdapter.TurnStart(ctx, contracts.TurnStartRequest{
		ThreadID:             p.ThreadID,
		Cwd:                  p.Cwd,
		Input:                toCodexTurnInputs(p.Input),
		SelectedSkills:       p.SelectedSkills,
		ManualSkillSelection: p.ManualSkillSelection,
		OutputSchema:         p.OutputSchema,
	})
	if err != nil {
		return nil, err
	}

	return turnStartResponse{
		Turn: turnInfo{ID: startResult.TurnID, Status: "inProgress"},
	}, nil
}

type turnSteerParams struct {
	ThreadID             string      `json:"threadId"`
	Input                []UserInput `json:"input"`
	SelectedSkills       []string    `json:"selectedSkills,omitempty"`
	ManualSkillSelection bool        `json:"manualSkillSelection,omitempty"`
}

func (s *Server) prepareTurnSteerSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (contracts.TurnSteerEntryPrepareResult, error) {
	userInputs := toUserInputs(input)
	prompt, images, files := s.extractInputs(userInputs)
	skillPrompt, _, _ := s.buildTurnSkillPrompt(threadID, prompt, userInputs, selectedSkills, manualSkillSelection)
	submitPrompt := s.mergePromptText(prompt, skillPrompt)
	return contracts.TurnSteerEntryPrepareResult{
		SubmitPrompt: submitPrompt,
		Images:       images,
		Files:        files,
	}, nil
}

// PrepareTurnSteerSubmission exposes turn/steer preparation for codexadapter context integration.
func (s *Server) PrepareTurnSteerSubmission(
	threadID string,
	input []contracts.TurnInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (contracts.TurnSteerEntryPrepareResult, error) {
	return s.prepareTurnSteerSubmission(threadID, input, selectedSkills, manualSkillSelection)
}

func (s *Server) turnSteerTyped(_ context.Context, p turnSteerParams) (any, error) {
	return s.codexAdapter.TurnSteerFromInput(contracts.TurnSteerRequest{
		ThreadID:             p.ThreadID,
		Input:                toCodexTurnInputs(p.Input),
		SelectedSkills:       p.SelectedSkills,
		ManualSkillSelection: p.ManualSkillSelection,
	})
}

func (s *Server) turnInterrupt(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.TurnInterruptFromParams(params)
}

// turnForceComplete 强制完成当前 turn (中断 + 清理跟踪状态)。
func (s *Server) turnForceComplete(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.TurnForceCompleteFromParams(params)
}

func (s *Server) waitInterruptSettled(threadID string, timeout time.Duration) (bool, string, int64) {
	confirmed, afterState, waitedMS, _ := s.codexAdapter.WaitInterruptOutcome(threadID, timeout, true)
	return confirmed, afterState, waitedMS
}

// reviewStartParams review/start 请求参数。
type reviewStartParams struct {
	ThreadID string `json:"threadId"`
	Delivery string `json:"delivery,omitempty"`
}

func (s *Server) reviewStartTyped(_ context.Context, p reviewStartParams) (any, error) {
	return s.codexAdapter.ReviewStart(p.ThreadID, p.Delivery)
}

// ========================================
// fuzzyFileSearch
// ========================================

type fuzzySearchParams struct {
	Query string   `json:"query"`
	Roots []string `json:"roots"`
}

func (s *Server) fuzzyFileSearchTyped(_ context.Context, p fuzzySearchParams) (any, error) {
	if s == nil || s.codexAdapter == nil {
		return map[string]any{"files": []map[string]any{}}, nil
	}
	results := s.codexAdapter.FuzzyFileSearch(p.Query, p.Roots, s.fuzzyMatch)
	return map[string]any{"files": results}, nil
}

// fuzzyMatch 子序列模糊匹配。
func (s *Server) fuzzyMatch(text, pattern string) bool {
	adapter := (*commonadapter.Adapter)(nil)
	if s != nil {
		adapter = s.commonAdapter
	}
	if adapter == nil {
		adapter = commonadapter.New()
	}
	return adapter.FuzzyMatch(text, pattern)
}

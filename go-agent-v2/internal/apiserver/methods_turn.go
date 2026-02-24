// methods_turn.go — turn/* / review / fuzzySearch JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
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
	if len(inputs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if !strings.EqualFold(strings.TrimSpace(input.Type), "skill") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(input.Name))
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
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
	var getPref func(context.Context, string) (any, error)
	if s.prefManager != nil {
		getPref = s.prefManager.Get
	}
	return codexadapter.ResolveLSPUsagePromptHint(ctx, codexadapter.ResolveLSPUsagePromptHintOptions{
		DefaultHint: defaultLSPUsagePromptHint,
		MaxHintLen:  maxLSPUsagePromptHintLen,
		GetPref:     getPref,
	})
}

func collectReferencedLSPToolNames(hint string) []string {
	return commonadapter.CollectReferencedLSPToolNames(hint)
}

func collectDynamicToolNames(dynamicTools []agentcore.DynamicTool) map[string]struct{} {
	if len(dynamicTools) == 0 {
		return nil
	}
	toolNames := make(map[string]struct{}, len(dynamicTools))
	for _, tool := range dynamicTools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		toolNames[name] = struct{}{}
	}
	return toolNames
}

func (s *Server) prependLSPAvailabilityWarning(hint string, dynamicTools []agentcore.DynamicTool) (string, []string) {
	return codexadapter.PrependLSPAvailabilityWarning(codexadapter.PrependLSPAvailabilityWarningOptions{
		Hint:                       hint,
		DynamicToolNames:           collectDynamicToolNames(dynamicTools),
		CollectReferencedToolNames: collectReferencedLSPToolNames,
		MergePromptText:            s.mergePromptText,
	})
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
	if s.skillSvc == nil {
		return "", 0
	}
	return codexadapter.BuildSelectedSkillPrompt(codexadapter.BuildSelectedSkillPromptOptions{
		SelectedSkills:   selectedSkills,
		ReadSkillContent: s.skillSvc.ReadSkillContent,
		SkillInputText:   s.skillInputText,
	})
}

func (s *Server) buildTurnSkillPrompt(threadID, prompt string, input []UserInput, selectedSkills []string, manualSkillSelection bool) (string, int, int) {
	selectedSkillPrompt, selectedSkillCount := s.buildSelectedSkillPrompt(selectedSkills)
	if manualSkillSelection || selectedSkillCount > 0 {
		return selectedSkillPrompt, selectedSkillCount, 0
	}
	autoSkillPrompt, autoSkillCount := s.buildForcedOrExplicitMatchedSkillPrompt(threadID, prompt, input)
	return s.mergePromptText(selectedSkillPrompt, autoSkillPrompt), selectedSkillCount, autoSkillCount
}

func lowerMatchedTerms(text string, candidates []string) []string {
	return commonadapter.LowerMatchedTerms(text, candidates)
}

type autoMatchedSkillMatch = codexadapter.AutoMatchedSkillMatch

type autoSkillMatchOptions = codexadapter.AutoSkillMatchOptions

func buildAutoMatchInputs(input []UserInput) []codexadapter.AutoMatchInput {
	if len(input) == 0 {
		return nil
	}
	out := make([]codexadapter.AutoMatchInput, 0, len(input))
	for _, item := range input {
		out = append(out, codexadapter.AutoMatchInput{
			Type: item.Type,
			Name: item.Name,
		})
	}
	return out
}

func (s *Server) collectAutoMatchedSkillMatches(agentID, prompt string, input []UserInput, options autoSkillMatchOptions) []autoMatchedSkillMatch {
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
	candidates := make([]codexadapter.SkillMatchCandidate, 0, len(allSkills))
	for _, skill := range allSkills {
		skillName := strings.TrimSpace(skill.Name)
		if skillName == "" {
			continue
		}
		candidates = append(candidates, codexadapter.SkillMatchCandidate{
			Name:         skillName,
			ForceWords:   append([]string(nil), skill.ForceWords...),
			TriggerWords: append([]string(nil), skill.TriggerWords...),
		})
	}
	return codexadapter.CollectAutoMatchedSkillMatches(
		prompt,
		buildAutoMatchInputs(input),
		s.GetAgentSkills(agentID),
		candidates,
		codexadapter.AutoSkillMatchOptions(options),
	)
}

func (s *Server) buildAutoMatchedSkillPrompt(agentID, prompt string, input []UserInput) (string, int) {
	matches := s.collectAutoMatchedSkillMatches(agentID, prompt, input, autoSkillMatchOptions{
		IncludeConfiguredForce: true,
	})
	return s.renderAutoMatchedSkillPrompt(agentID, matches)
}

func (s *Server) buildForcedOrExplicitMatchedSkillPrompt(agentID, prompt string, input []UserInput) (string, int) {
	matches := s.collectAutoMatchedSkillMatches(agentID, prompt, input, autoSkillMatchOptions{
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
	return s.renderAutoMatchedSkillPrompt(agentID, filtered)
}

func (s *Server) renderAutoMatchedSkillPrompt(agentID string, matches []autoMatchedSkillMatch) (string, int) {
	if s.skillSvc == nil || len(matches) == 0 {
		return "", 0
	}
	return codexadapter.RenderAutoMatchedSkillPrompt(codexadapter.RenderAutoMatchedSkillPromptOptions{
		Matches:          matches,
		ReadSkillContent: s.skillSvc.ReadSkillContent,
		MergePromptText:  s.mergePromptText,
		SkillInputText:   s.skillInputText,
		OnReadSkillError: func(skillName, matchedBy string, readErr error) {
			logger.Warn("turn/start: auto-matched skill unavailable, skip",
				logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
				logger.FieldSkill, skillName,
				"matched_by", matchedBy,
				logger.FieldError, readErr,
			)
		},
	})
}

// ========================================
// fuzzyFileSearch
// ========================================

type fuzzySearchParams struct {
	Query string   `json:"query"`
	Roots []string `json:"roots"`
}

func (s *Server) fuzzyFileSearchTyped(_ context.Context, p fuzzySearchParams) (any, error) {
	results := codexadapter.FuzzyFileSearch(codexadapter.FuzzyFileSearchOptions{
		Query:      p.Query,
		Roots:      p.Roots,
		FuzzyMatch: s.fuzzyMatch,
	})
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

func normalizeSkillName(raw string) (string, error) {
	return commonadapter.NormalizeSkillName(raw)
}

func normalizeSkillNames(rawNames []string) ([]string, error) {
	return commonadapter.NormalizeSkillNames(rawNames)
}

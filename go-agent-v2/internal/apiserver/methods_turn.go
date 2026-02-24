// methods_turn.go — turn/* / review / fuzzySearch JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
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
	if s.prefManager == nil {
		return defaultLSPUsagePromptHint
	}
	value, err := s.prefManager.Get(ctx, prefKeyLSPUsagePromptHint)
	if err != nil {
		logger.Warn("lsp hint: load preference failed", logger.FieldError, err)
		return defaultLSPUsagePromptHint
	}
	hint := strings.TrimSpace(asString(value))
	if hint == "" {
		return defaultLSPUsagePromptHint
	}
	if err := validateLSPUsagePromptHint(hint); err != nil {
		logger.Warn("lsp hint: invalid preference fallback to default", logger.FieldError, err)
		return defaultLSPUsagePromptHint
	}
	return hint
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
	referenced := collectReferencedLSPToolNames(hint)
	if len(referenced) == 0 {
		return hint, nil
	}
	available := collectDynamicToolNames(dynamicTools)
	missing := make([]string, 0, len(referenced))
	for _, name := range referenced {
		if _, ok := available[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 {
		return hint, nil
	}
	warning := "注意：当前会话未注入以下 LSP 工具（无可用 language server）：" +
		strings.Join(missing, ", ") +
		"。不要调用这些工具，请改用当前可用工具完成任务。"
	return s.mergePromptText(warning, hint), missing
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
	ordered := make([]string, 0, len(selectedSkills))
	seen := make(map[string]struct{}, len(selectedSkills))
	appendName := func(raw string) {
		name := strings.TrimSpace(raw)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		ordered = append(ordered, name)
	}
	for _, name := range selectedSkills {
		appendName(name)
	}
	if len(ordered) == 0 {
		return "", 0
	}

	texts := make([]string, 0, len(ordered))
	for _, skillName := range ordered {
		content, err := s.skillSvc.ReadSkillContent(skillName)
		if err != nil {
			logger.Warn("turn/start: selected skill unavailable, skip",
				logger.FieldSkill, skillName,
				logger.FieldError, err,
			)
			continue
		}
		texts = append(texts, s.skillInputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
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

type autoMatchedSkillMatch struct {
	Name         string
	MatchedBy    string
	MatchedTerms []string
}

type autoSkillMatchOptions struct {
	IncludeConfiguredExplicit bool
	IncludeConfiguredForce    bool
}

func classifyAutoSkillMatch(normalizedPrompt, skillName string, forceWords, triggerWords []string) (string, []string) {
	return commonadapter.ClassifyAutoSkillMatch(normalizedPrompt, skillName, forceWords, triggerWords)
}

func forceMatchedSkillInstruction(matchedTerms []string) string {
	return commonadapter.ForceMatchedSkillInstruction(matchedTerms)
}

func (s *Server) collectAutoMatchedSkillMatches(agentID, prompt string, input []UserInput, options autoSkillMatchOptions) []autoMatchedSkillMatch {
	if s.skillSvc == nil {
		return nil
	}
	normalizedPrompt := strings.ToLower(strings.TrimSpace(prompt))
	if normalizedPrompt == "" {
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

	inputSkillSet := collectInputSkillNames(input)
	configuredSet := collectSkillNameSet(s.GetAgentSkills(agentID))

	matches := make([]autoMatchedSkillMatch, 0, len(allSkills))
	for _, skill := range allSkills {
		skillName := strings.TrimSpace(skill.Name)
		if skillName == "" {
			continue
		}
		skillNameLower := strings.ToLower(skillName)
		if _, exists := inputSkillSet[skillNameLower]; exists {
			continue
		}
		matchedBy, matchedTerms := classifyAutoSkillMatch(normalizedPrompt, skillName, skill.ForceWords, skill.TriggerWords)
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
		matches = append(matches, autoMatchedSkillMatch{
			Name:         skillName,
			MatchedBy:    matchedBy,
			MatchedTerms: matchedTerms,
		})
	}
	return matches
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
	if len(matches) == 0 {
		return "", 0
	}

	texts := make([]string, 0, len(matches))
	for _, match := range matches {
		skillName := strings.TrimSpace(match.Name)
		if skillName == "" {
			continue
		}
		forceInstruction := ""
		if match.MatchedBy == "force" {
			forceInstruction = forceMatchedSkillInstruction(match.MatchedTerms)
		}
		var (
			content string
			readErr error
		)
		content, readErr = s.skillSvc.ReadSkillContent(skillName)
		if readErr != nil {
			logger.Warn("turn/start: auto-matched skill unavailable, skip",
				logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
				logger.FieldSkill, skillName,
				"matched_by", match.MatchedBy,
				logger.FieldError, readErr,
			)
			continue
		}
		if forceInstruction != "" {
			content = s.mergePromptText(forceInstruction, content)
		}
		texts = append(texts, s.skillInputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

// ========================================
// fuzzyFileSearch
// ========================================

type fuzzySearchParams struct {
	Query string   `json:"query"`
	Roots []string `json:"roots"`
}

func (s *Server) fuzzyFileSearchTyped(_ context.Context, p fuzzySearchParams) (any, error) {
	query := strings.ToLower(p.Query)
	results := make([]map[string]any, 0)

	for _, root := range p.Roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				base := filepath.Base(path)
				if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if s.fuzzyMatch(strings.ToLower(rel), query) {
				results = append(results, map[string]any{
					"root":     root,
					"path":     rel,
					"fileName": info.Name(),
				})
				if len(results) >= 100 {
					return filepath.SkipAll
				}
			}
			return nil
		})
	}

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

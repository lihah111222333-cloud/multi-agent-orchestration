// methods_turn.go — turn/* / review / fuzzySearch JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/runner"
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

type activeTurnIDReader interface {
	GetActiveTurnID() string
}

func resolveClientActiveTurnID(client codex.CodexClient) string {
	if client == nil {
		return ""
	}
	reader, ok := client.(activeTurnIDReader)
	if !ok {
		return ""
	}
	return strings.TrimSpace(reader.GetActiveTurnID())
}

func skillInputText(name, content string) string {
	return fmt.Sprintf("[skill:%s] %s", strings.TrimSpace(name), content)
}

func fileContentInputText(name, content string) string {
	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		return ""
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return trimmedContent
	}
	return fmt.Sprintf("[file:%s]\n%s", trimmedName, trimmedContent)
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
	if len(raw) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

func mergePromptText(prompt, extra string) string {
	trimmedExtra := strings.TrimSpace(extra)
	if trimmedExtra == "" {
		return prompt
	}
	trimmedPrompt := strings.TrimSpace(prompt)
	if trimmedPrompt == "" {
		return extra
	}
	return prompt + "\n" + extra
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

func (s *Server) appendLSPUsageHint(ctx context.Context, prompt string) string {
	return mergePromptText(prompt, s.resolveLSPUsagePromptHint(ctx))
}

func (s *Server) appendJsonRenderHint(ctx context.Context, prompt string) string {
	return mergePromptText(prompt, s.resolveJsonRenderPrompt(ctx))
}

func (s *Server) resolveBrowserPrompt(ctx context.Context) string {
	if s.prefManager == nil {
		return defaultBrowserPrompt
	}
	value, err := s.prefManager.Get(ctx, prefKeyBrowserPrompt)
	if err != nil {
		logger.Warn("browser prompt: load preference failed", logger.FieldError, err)
		return defaultBrowserPrompt
	}
	prompt := strings.TrimSpace(asString(value))
	if prompt == "" {
		return defaultBrowserPrompt
	}
	if len(prompt) > maxBrowserPromptLen {
		logger.Warn("browser prompt: invalid preference fallback to default", "length", len(prompt))
		return defaultBrowserPrompt
	}
	return prompt
}

func (s *Server) appendBrowserHint(ctx context.Context, prompt string) string {
	return mergePromptText(prompt, s.resolveBrowserPrompt(ctx))
}

func appendSkillPlaceholders(input []UserInput, skillNames []string) []UserInput {
	if len(skillNames) == 0 {
		return input
	}
	out := make([]UserInput, 0, len(input)+len(skillNames))
	out = append(out, input...)
	for _, name := range skillNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		out = append(out, UserInput{
			Type: "skill",
			Name: trimmed,
		})
	}
	return out
}

func (s *Server) buildConfiguredSkillPrompt(agentID string, input []UserInput) (string, int) {
	if s.skillSvc == nil {
		return "", 0
	}
	configured := s.GetAgentSkills(agentID)
	if len(configured) == 0 {
		return "", 0
	}

	inputSkillSet := collectInputSkillNames(input)
	texts := make([]string, 0, len(configured))
	for _, name := range configured {
		normalizedName := strings.TrimSpace(name)
		if normalizedName == "" {
			continue
		}
		if _, exists := inputSkillSet[strings.ToLower(normalizedName)]; exists {
			continue
		}
		content, err := s.skillSvc.ReadSkillDigestContent(normalizedName)
		if err != nil {
			logger.Warn("turn/start: configured skill unavailable, skip",
				logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
				logger.FieldSkill, normalizedName,
				logger.FieldError, err,
			)
			continue
		}
		texts = append(texts, skillInputText(normalizedName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

func (s *Server) buildSelectedSkillPrompt(selectedSkills []string, input []UserInput) (string, int) {
	if s.skillSvc == nil || len(selectedSkills) == 0 {
		return "", 0
	}
	inputSkillSet := collectInputSkillNames(input)
	texts := make([]string, 0, len(selectedSkills))
	for _, rawName := range selectedSkills {
		skillName := strings.TrimSpace(rawName)
		if skillName == "" {
			continue
		}
		if _, exists := inputSkillSet[strings.ToLower(skillName)]; exists {
			continue
		}
		content, err := s.skillSvc.ReadSkillDigestContent(skillName)
		if err != nil {
			logger.Warn("turn/start: selected skill unavailable, skip",
				logger.FieldSkill, skillName,
				logger.FieldError, err,
			)
			continue
		}
		texts = append(texts, skillInputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

func lowerMatchedTerms(text string, candidates []string) []string {
	if text == "" || len(candidates) == 0 {
		return nil
	}
	terms := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, raw := range candidates {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		lowerCandidate := strings.ToLower(candidate)
		if _, ok := seen[lowerCandidate]; ok {
			continue
		}
		if !strings.Contains(text, lowerCandidate) {
			continue
		}
		seen[lowerCandidate] = struct{}{}
		terms = append(terms, candidate)
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
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

func explicitSkillMentionTerms(normalizedPrompt, skillName string, triggerWords []string) []string {
	trimmedName := strings.TrimSpace(skillName)
	candidates := make([]string, 0, 1+len(triggerWords))
	if trimmedName != "" {
		candidates = append(candidates, "@"+trimmedName)
		candidates = append(candidates, "[skill:"+trimmedName+"]")
	}
	for _, raw := range triggerWords {
		word := strings.TrimSpace(raw)
		if word == "" {
			continue
		}
		lowerWord := strings.ToLower(word)
		if strings.HasPrefix(lowerWord, "@") || strings.HasPrefix(lowerWord, "[skill:") {
			candidates = append(candidates, word)
		}
	}

	terms := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		lowerCandidate := strings.ToLower(strings.TrimSpace(candidate))
		if lowerCandidate == "" {
			continue
		}
		if _, exists := seen[lowerCandidate]; exists {
			continue
		}
		if !strings.Contains(normalizedPrompt, lowerCandidate) {
			continue
		}
		seen[lowerCandidate] = struct{}{}
		terms = append(terms, candidate)
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
}

func classifyAutoSkillMatch(normalizedPrompt, skillName string, forceWords, triggerWords []string) (string, []string) {
	forceTerms := lowerMatchedTerms(normalizedPrompt, forceWords)
	if len(forceTerms) > 0 {
		return "force", forceTerms
	}
	explicitTerms := explicitSkillMentionTerms(normalizedPrompt, skillName, triggerWords)
	if len(explicitTerms) > 0 {
		return "explicit", explicitTerms
	}
	triggerTerms := lowerMatchedTerms(normalizedPrompt, triggerWords)
	if len(triggerTerms) > 0 {
		return "trigger", triggerTerms
	}
	return "", nil
}

func forceMatchedSkillInstruction(matchedTerms []string) string {
	terms := make([]string, 0, len(matchedTerms))
	for _, raw := range matchedTerms {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		terms = append(terms, trimmed)
	}
	if len(terms) == 0 {
		return "执行要求: 本轮必须遵循该技能。"
	}
	return fmt.Sprintf("强制触发词: %s\n执行要求: 本轮必须遵循该技能。", strings.Join(terms, ", "))
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

	configuredSet := collectSkillNameSet(s.GetAgentSkills(agentID))
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
		if forceInstruction != "" {
			if _, configured := configuredSet[strings.ToLower(skillName)]; configured {
				texts = append(texts, skillInputText(skillName, forceInstruction))
				continue
			}
		}
		content, readErr := s.skillSvc.ReadSkillDigestContent(skillName)
		if readErr != nil {
			logger.Warn("turn/start: auto-matched skill unavailable, skip",
				logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
				logger.FieldSkill, skillName,
				logger.FieldError, readErr,
			)
			continue
		}
		if forceInstruction != "" {
			content = mergePromptText(forceInstruction, content)
		}
		texts = append(texts, skillInputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

func (s *Server) turnStartTyped(ctx context.Context, p turnStartParams) (any, error) {
	logger.Info("turn/start: request received",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		logger.FieldCwd, strings.TrimSpace(p.Cwd),
		"input_count", len(p.Input),
		"selected_skills_count", len(p.SelectedSkills),
	)
	proc, err := s.ensureThreadReadyForTurn(ctx, p.ThreadID, p.Cwd)
	if err != nil {
		return nil, err
	}
	logger.Info("turn/start: thread dispatch resolved",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		logger.FieldPort, proc.Client.GetPort(),
		"codex_thread_id", strings.TrimSpace(proc.Client.GetThreadID()),
	)

	selectedSkills, err := normalizeSkillNames(p.SelectedSkills)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.turnStart", "normalize selected skills")
	}

	prompt, images, files := extractInputs(p.Input)
	matchingInput := appendSkillPlaceholders(p.Input, selectedSkills)
	configuredSkillPrompt, configuredSkillCount := s.buildConfiguredSkillPrompt(p.ThreadID, matchingInput)
	selectedSkillPrompt, selectedSkillCount := s.buildSelectedSkillPrompt(selectedSkills, p.Input)
	autoMatchedSkillPrompt := ""
	autoMatchedSkillCount := 0
	if p.ManualSkillSelection {
		autoMatchedSkillPrompt, autoMatchedSkillCount = s.buildForcedOrExplicitMatchedSkillPrompt(p.ThreadID, prompt, matchingInput)
	} else {
		autoMatchedSkillPrompt, autoMatchedSkillCount = s.buildAutoMatchedSkillPrompt(p.ThreadID, prompt, matchingInput)
	}
	submitPrompt := mergePromptText(prompt, configuredSkillPrompt)
	submitPrompt = mergePromptText(submitPrompt, selectedSkillPrompt)
	submitPrompt = mergePromptText(submitPrompt, autoMatchedSkillPrompt)
	submitPrompt = s.appendLSPUsageHint(ctx, submitPrompt)
	submitPrompt = s.appendJsonRenderHint(ctx, submitPrompt)
	submitPrompt = s.appendBrowserHint(ctx, submitPrompt)
	logger.Info("turn/start: input prepared",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		"text_len", len(prompt),
		"images", len(images),
		"files", len(files),
		"configured_skills", configuredSkillCount,
		"selected_skills", selectedSkillCount,
		"manual_skill_selection", p.ManualSkillSelection,
		"auto_matched_skills", autoMatchedSkillCount,
	)
	if err := proc.Client.Submit(submitPrompt, images, files, p.OutputSchema); err != nil {
		return nil, apperrors.Wrap(err, "Server.turnStart", "submit prompt")
	}
	if s.uiRuntime != nil {
		attachments := buildUserTimelineAttachmentsFromInputs(p.Input)
		if len(attachments) == 0 {
			attachments = buildUserTimelineAttachments(images, files)
		}
		s.uiRuntime.AppendUserMessage(p.ThreadID, prompt, attachments)
	}

	resolvedTurnID := resolveClientActiveTurnID(proc.Client)
	if resolvedTurnID == "" {
		logger.Warn("turn/start: active turn id unavailable after submit; tracker will use synthetic id",
			logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		)
	}
	turnID := s.beginTrackedTurn(p.ThreadID, resolvedTurnID)
	return turnStartResponse{
		Turn: turnInfo{ID: turnID, Status: "inProgress"},
	}, nil
}

type turnSteerParams struct {
	ThreadID             string      `json:"threadId"`
	Input                []UserInput `json:"input"`
	SelectedSkills       []string    `json:"selectedSkills,omitempty"`
	ManualSkillSelection bool        `json:"manualSkillSelection,omitempty"`
}

func (s *Server) turnSteerTyped(ctx context.Context, p turnSteerParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		selectedSkills, err := normalizeSkillNames(p.SelectedSkills)
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.turnSteer", "normalize selected skills")
		}
		prompt, images, files := extractInputs(p.Input)
		matchingInput := appendSkillPlaceholders(p.Input, selectedSkills)
		configuredSkillPrompt, _ := s.buildConfiguredSkillPrompt(p.ThreadID, matchingInput)
		selectedSkillPrompt, _ := s.buildSelectedSkillPrompt(selectedSkills, p.Input)
		autoMatchedSkillPrompt := ""
		if p.ManualSkillSelection {
			autoMatchedSkillPrompt, _ = s.buildForcedOrExplicitMatchedSkillPrompt(p.ThreadID, prompt, matchingInput)
		} else {
			autoMatchedSkillPrompt, _ = s.buildAutoMatchedSkillPrompt(p.ThreadID, prompt, matchingInput)
		}
		submitPrompt := mergePromptText(prompt, configuredSkillPrompt)
		submitPrompt = mergePromptText(submitPrompt, selectedSkillPrompt)
		submitPrompt = mergePromptText(submitPrompt, autoMatchedSkillPrompt)
		submitPrompt = s.appendLSPUsageHint(ctx, submitPrompt)
		submitPrompt = s.appendJsonRenderHint(ctx, submitPrompt)
		submitPrompt = s.appendBrowserHint(ctx, submitPrompt)
		if err := proc.Client.Submit(submitPrompt, images, files, nil); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

func (s *Server) turnInterrupt(_ context.Context, params json.RawMessage) (any, error) {
	start := time.Now()
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.turnInterrupt", "unmarshal params")
	}
	beforeState := s.readThreadRuntimeState(p.ThreadID)
	activeTrackedBefore := s.hasActiveTrackedTurn(p.ThreadID)
	activeBefore := isInterruptActiveState(beforeState)
	logger.Info("turn/interrupt: request",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		logger.FieldParamsLen, len(params),
		"state_before", beforeState,
		"active_before", activeBefore,
		"active_tracked_before", activeTrackedBefore,
	)
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand("/interrupt", ""); err != nil {
			if isInterruptNoActiveTurnError(err) {
				if activeBefore || activeTrackedBefore {
					if completion, ok := s.completeTrackedTurn(p.ThreadID, "completed", "interrupt_no_active_turn"); ok {
						s.Notify("turn/completed", completion)
					} else {
						s.Notify("turn/completed", map[string]any{
							"threadId": p.ThreadID,
							"status":   "completed",
							"reason":   "interrupt_no_active_turn",
						})
					}
				}
				logger.Info("turn/interrupt: no active turn",
					logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
					"state_before", beforeState,
					logger.FieldDurationMS, time.Since(start).Milliseconds(),
				)
				return map[string]any{
					"confirmed":     false,
					"mode":          "no_active_turn",
					"interruptSent": false,
					"stateBefore":   beforeState,
					"stateAfter":    beforeState,
				}, nil
			}
			logger.Warn("turn/interrupt: send command failed",
				logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
				logger.FieldError, err,
				logger.FieldDurationMS, time.Since(start).Milliseconds(),
			)
			return nil, err
		}
		logger.Info("turn/interrupt: command sent",
			logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		s.markTrackedTurnInterruptRequested(p.ThreadID)
		confirmed, afterState, waitedMS, observedActive := s.waitInterruptOutcome(
			p.ThreadID,
			6*time.Second,
			activeBefore || activeTrackedBefore,
		)
		mode := interruptSettleMode(confirmed, afterState)
		if !observedActive {
			confirmed = false
			mode = "no_active_turn"
		}
		logger.Info("turn/interrupt: settle",
			logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
			"confirmed", confirmed,
			"mode", mode,
			"active_observed", observedActive,
			"state_before", beforeState,
			"state_after", afterState,
			"waited_ms", waitedMS,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return map[string]any{
			"confirmed":      confirmed,
			"mode":           mode,
			"interruptSent":  true,
			"stateBefore":    beforeState,
			"stateAfter":     afterState,
			"waitedMs":       waitedMS,
			"activeObserved": observedActive,
		}, nil
	})
}

// turnForceComplete 强制完成当前 turn (中断 + 清理跟踪状态)。
func (s *Server) turnForceComplete(_ context.Context, params json.RawMessage) (any, error) {
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.turnForceComplete", "unmarshal params")
	}
	logger.Info("turn/forceComplete: request",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
	)
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		// 尝试发送中断; 忽略 "no active turn" 错误, 但记录其他错误。
		if err := proc.Client.SendCommand("/interrupt", ""); err != nil {
			if isInterruptNoActiveTurnError(err) {
				logger.Info("turn/forceComplete: no active turn (best-effort)",
					logger.FieldAgentID, p.ThreadID)
			} else {
				logger.Warn("turn/forceComplete: interrupt failed (best-effort)",
					logger.FieldAgentID, p.ThreadID, logger.FieldError, err)
			}
		}

		// 无论中断是否成功, 都强制清理 tracked turn 状态。
		if completion, ok := s.completeTrackedTurn(p.ThreadID, "completed", "force_complete"); ok {
			s.Notify("turn/completed", completion)
		} else {
			s.Notify("turn/completed", map[string]any{
				"threadId": p.ThreadID,
				"status":   "completed",
				"reason":   "force_complete",
			})
		}

		return map[string]any{
			"confirmed":      true,
			"forceCompleted": true,
		}, nil
	})
}

func normalizeInterruptState(raw string) string {
	state := strings.ToLower(strings.TrimSpace(raw))
	if state == "" {
		return "idle"
	}
	switch state {
	case "completed", "complete", "done", "success", "succeeded", "ready", "stopped", "ended", "closed":
		return "idle"
	case "failed", "fail":
		return "error"
	default:
		return state
	}
}

func isInterruptActiveState(state string) bool {
	switch normalizeInterruptState(state) {
	case "starting", "thinking", "responding", "running", "editing", "waiting", "syncing":
		return true
	default:
		return false
	}
}

func isInterruptNoActiveTurnError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no active turn") ||
		strings.Contains(message, "nothing to interrupt") ||
		strings.Contains(message, "not interruptible")
}

func (s *Server) readThreadRuntimeState(threadID string) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	if s.uiRuntime == nil {
		if s.hasActiveTrackedTurn(id) {
			return "running"
		}
		return ""
	}
	snapshot := s.uiRuntime.Snapshot()
	state := normalizeInterruptState(snapshot.Statuses[id])
	if state == "idle" && s.hasActiveTrackedTurn(id) {
		return "running"
	}
	return state
}

func (s *Server) waitInterruptSettled(threadID string, timeout time.Duration) (bool, string, int64) {
	confirmed, afterState, waitedMS, _ := s.waitInterruptOutcome(threadID, timeout, true)
	return confirmed, afterState, waitedMS
}

func (s *Server) waitInterruptOutcome(threadID string, timeout time.Duration, activeHint bool) (bool, string, int64, bool) {
	start := time.Now()
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false, "idle", 0, false
	}
	observedActive := activeHint
	if terminalStatus, ok := s.waitTrackedTurnTerminal(id, timeout); ok {
		afterState := normalizeInterruptState(terminalStatus)
		confirmed := strings.EqualFold(terminalStatus, "interrupted")
		return confirmed, afterState, time.Since(start).Milliseconds(), true
	}
	if s.uiRuntime == nil {
		return false, "", time.Since(start).Milliseconds(), observedActive
	}
	deadline := start.Add(timeout)
	lastState := s.readThreadRuntimeState(id)
	if isInterruptActiveState(lastState) {
		observedActive = true
	}
	for {
		if !isInterruptActiveState(lastState) {
			if !observedActive {
				return false, lastState, time.Since(start).Milliseconds(), false
			}
			return true, lastState, time.Since(start).Milliseconds(), true
		}
		observedActive = true
		if time.Now().After(deadline) {
			return false, lastState, time.Since(start).Milliseconds(), true
		}
		time.Sleep(120 * time.Millisecond)
		lastState = s.readThreadRuntimeState(id)
	}
}

func interruptSettleMode(confirmed bool, afterState string) string {
	if confirmed {
		return "interrupt_confirmed"
	}
	switch normalizeInterruptState(afterState) {
	case "error":
		return "interrupt_terminal_failed"
	case "idle":
		return "interrupt_terminal_completed"
	default:
		return "interrupt_timeout"
	}
}

// reviewStartParams review/start 请求参数。
type reviewStartParams struct {
	ThreadID string `json:"threadId"`
	Delivery string `json:"delivery,omitempty"`
}

func (s *Server) reviewStartTyped(_ context.Context, p reviewStartParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand("/review", p.Delivery); err != nil {
			return nil, apperrors.Wrap(err, "Server.reviewStart", "send review command")
		}
		return map[string]any{}, nil
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
			if fuzzyMatch(strings.ToLower(rel), query) {
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
func fuzzyMatch(text, pattern string) bool {
	pi := 0
	for i := 0; i < len(text) && pi < len(pattern); i++ {
		if text[i] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

func normalizeSkillName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", apperrors.New("normalizeSkillName", "skill name is required")
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", apperrors.Newf("normalizeSkillName", "invalid skill name %q", raw)
	}
	return name, nil
}

func normalizeSkillNames(rawNames []string) ([]string, error) {
	if len(rawNames) == 0 {
		return []string{}, nil
	}
	names := make([]string, 0, len(rawNames))
	seen := make(map[string]struct{}, len(rawNames))
	for _, raw := range rawNames {
		name, err := normalizeSkillName(raw)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}

// methods_turn_codex.go — turn/review 的 codex 专属实现。
package apiserver

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

var _ = (*Server).waitInterruptSettled

func resolveClientActiveTurnID(client agentcore.Client) string {
	return codexadapter.ResolveClientActiveTurnID(client)
}

func (s *Server) turnStartTyped(ctx context.Context, p turnStartParams) (any, error) {
	logger.Info("turn/start: request received",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		logger.FieldCwd, strings.TrimSpace(p.Cwd),
		"input_count", len(p.Input),
		"selected_skills_count", len(p.SelectedSkills),
	)
	selectedSkills, err := normalizeSkillNames(p.SelectedSkills)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.turnStart", "normalize selected skills")
	}

	prompt, images, files := s.extractInputs(p.Input)
	skillPrompt, selectedSkillCount, autoMatchedSkillCount := s.buildTurnSkillPrompt(p.ThreadID, prompt, p.Input, selectedSkills, p.ManualSkillSelection)
	submitPrompt := s.mergePromptText(prompt, skillPrompt)
	logger.Info("turn/start: input prepared",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		"text_len", len(prompt),
		"images", len(images),
		"files", len(files),
		"selected_skills_requested", len(selectedSkills),
		"selected_skills_injected", selectedSkillCount,
		"manual_skill_selection", p.ManualSkillSelection,
		"auto_matched_skills", autoMatchedSkillCount,
	)
	startResult, err := s.codexAdapter.StartTurnSubmissionAndTrack(ctx, codexadapter.StartTurnSubmissionOptions{
		ThreadID:             p.ThreadID,
		Cwd:                  p.Cwd,
		SubmitPrompt:         submitPrompt,
		Images:               images,
		Files:                files,
		OutputSchema:         p.OutputSchema,
		EnsureThreadReady:    s.ensureThreadReadyForTurn,
		HasActiveTrackedTurn: s.hasActiveTrackedTurn,
		ResolveActiveTurnID:  resolveClientActiveTurnID,
		BeginTrackedTurn:     s.beginTrackedTurn,
	})
	if err != nil {
		return nil, err
	}

	if s.uiRuntime != nil {
		attachments := buildUserTimelineAttachmentsFromInputs(p.Input)
		if len(attachments) == 0 {
			attachments = buildUserTimelineAttachments(images, files)
		}
		showInjected := s.showInjectedPromptInChat(ctx)
		appendInjectedHint := showInjected && !s.threadTimelineAlreadyShowsInjectedPrompt(p.ThreadID)
		injectedHint := ""
		if appendInjectedHint {
			injectedHint = s.resolveLSPUsagePromptHint(ctx)
		}
		timelineText := s.composeUserTimelineTextForTurn(prompt, submitPrompt, injectedHint, showInjected)
		s.uiRuntime.AppendUserMessage(p.ThreadID, timelineText, attachments)
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

func (s *Server) turnSteerTyped(ctx context.Context, p turnSteerParams) (any, error) {
	selectedSkills, err := normalizeSkillNames(p.SelectedSkills)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.turnSteer", "normalize selected skills")
	}
	prompt, images, files := s.extractInputs(p.Input)
	skillPrompt, _, _ := s.buildTurnSkillPrompt(p.ThreadID, prompt, p.Input, selectedSkills, p.ManualSkillSelection)
	submitPrompt := s.mergePromptText(prompt, skillPrompt)
	return s.codexAdapter.TurnSteer(codexadapter.TurnSteerOptions{
		ThreadID:     p.ThreadID,
		SubmitPrompt: submitPrompt,
		Images:       images,
		Files:        files,
		WithThread:   s.withThread,
	})
}

func (s *Server) turnInterrupt(_ context.Context, params json.RawMessage) (any, error) {
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.turnInterrupt", "unmarshal params")
	}
	return s.codexAdapter.TurnInterrupt(codexadapter.TurnInterruptOptions{
		ThreadID:                          p.ThreadID,
		ParamsLen:                         len(params),
		ReadThreadRuntimeState:            s.readThreadRuntimeState,
		HasActiveTrackedTurn:              s.hasActiveTrackedTurn,
		CancelCodeRuns:                    s.cancelCodeRuns,
		WithThread:                        s.withThread,
		CompleteTrackedTurn:               s.completeTrackedTurn,
		Notify:                            s.Notify,
		MarkTrackedTurnInterruptRequested: s.markTrackedTurnInterruptRequested,
		WaitInterruptOutcome:              s.waitInterruptOutcome,
		IsInterruptNoActiveTurnError:      isInterruptNoActiveTurnError,
		InterruptSettleMode:               interruptSettleMode,
	})
}

// turnForceComplete 强制完成当前 turn (中断 + 清理跟踪状态)。
func (s *Server) turnForceComplete(_ context.Context, params json.RawMessage) (any, error) {
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.turnForceComplete", "unmarshal params")
	}
	return s.codexAdapter.TurnForceComplete(codexadapter.TurnForceCompleteOptions{
		ThreadID:                     p.ThreadID,
		CancelCodeRuns:               s.cancelCodeRuns,
		WithThread:                   s.withThread,
		IsInterruptNoActiveTurnError: isInterruptNoActiveTurnError,
		CompleteTrackedTurn:          s.completeTrackedTurn,
		Notify:                       s.Notify,
	})
}

func normalizeInterruptState(raw string) string {
	return codexadapter.NormalizeInterruptState(raw)
}

func isInterruptActiveState(state string) bool {
	return codexadapter.IsInterruptActiveState(state)
}

func isInterruptNoActiveTurnError(err error) bool {
	return codexadapter.IsInterruptNoActiveTurnError(err)
}

func (s *Server) readThreadRuntimeState(threadID string) string {
	return codexadapter.ReadThreadRuntimeState(threadID, func(id string) string {
		if s.uiRuntime == nil {
			return ""
		}
		snapshot := s.uiRuntime.Snapshot()
		return snapshot.Statuses[id]
	}, s.hasActiveTrackedTurn)
}

func (s *Server) waitInterruptSettled(threadID string, timeout time.Duration) (bool, string, int64) {
	confirmed, afterState, waitedMS, _ := s.waitInterruptOutcome(threadID, timeout, true)
	return confirmed, afterState, waitedMS
}

func (s *Server) waitInterruptOutcome(threadID string, timeout time.Duration, activeHint bool) (bool, string, int64, bool) {
	return codexadapter.WaitInterruptOutcome(threadID, timeout, activeHint, s.waitTrackedTurnTerminal, s.readThreadRuntimeState)
}

func interruptSettleMode(confirmed bool, afterState string) string {
	return codexadapter.InterruptSettleMode(confirmed, afterState)
}

// reviewStartParams review/start 请求参数。
type reviewStartParams struct {
	ThreadID string `json:"threadId"`
	Delivery string `json:"delivery,omitempty"`
}

func (s *Server) reviewStartTyped(_ context.Context, p reviewStartParams) (any, error) {
	return s.codexAdapter.ReviewStart(codexadapter.ReviewStartOptions{
		ThreadID:   p.ThreadID,
		Delivery:   p.Delivery,
		WithThread: s.withThread,
	})
}

// methods_turn_codex.go — turn/review 的 codex 专属实现。
package apiserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

var _ = (*Server).waitInterruptSettled

func resolveClientActiveTurnID(client agentcore.Client) string {
	return codexadapter.ResolveClientActiveTurnID(client)
}

func (s *Server) prepareTurnStartSubmission(
	threadID string,
	input []UserInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (codexadapter.TurnStartEntryPrepareResult, error) {
	prompt, images, files := s.extractInputs(input)
	skillPrompt, selectedSkillCount, autoMatchedSkillCount := s.buildTurnSkillPrompt(threadID, prompt, input, selectedSkills, manualSkillSelection)
	submitPrompt := s.mergePromptText(prompt, skillPrompt)
	return codexadapter.TurnStartEntryPrepareResult{
		Prompt:                prompt,
		SubmitPrompt:          submitPrompt,
		Images:                images,
		Files:                 files,
		SelectedSkillCount:    selectedSkillCount,
		AutoMatchedSkillCount: autoMatchedSkillCount,
	}, nil
}

func (s *Server) appendTurnStartUserTimeline(ctx context.Context, input []UserInput, opt codexadapter.TurnAppendUserTimelineOptions) {
	if s.uiRuntime == nil {
		return
	}
	attachments := buildUserTimelineAttachmentsFromInputs(input)
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

func (s *Server) turnStartTyped(ctx context.Context, p turnStartParams) (any, error) {
	startResult, err := s.codexAdapter.TurnStartEntry(ctx, codexadapter.TurnStartEntryOptions{
		ThreadID:             p.ThreadID,
		Cwd:                  p.Cwd,
		InputCount:           len(p.Input),
		SelectedSkills:       p.SelectedSkills,
		ManualSkillSelection: p.ManualSkillSelection,
		OutputSchema:         p.OutputSchema,
		NormalizeSkillNames:  normalizeSkillNames,
		PrepareSubmission: func(threadID string, selectedSkills []string, manualSkillSelection bool) (codexadapter.TurnStartEntryPrepareResult, error) {
			return s.prepareTurnStartSubmission(threadID, p.Input, selectedSkills, manualSkillSelection)
		},
		EnsureThreadReady:    s.ensureThreadReadyForTurn,
		HasActiveTrackedTurn: s.hasActiveTrackedTurn,
		ResolveActiveTurnID:  resolveClientActiveTurnID,
		BeginTrackedTurn:     s.beginTrackedTurn,
		AppendUserTimeline: func(ctx context.Context, opt codexadapter.TurnAppendUserTimelineOptions) {
			s.appendTurnStartUserTimeline(ctx, p.Input, opt)
		},
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
	input []UserInput,
	selectedSkills []string,
	manualSkillSelection bool,
) (codexadapter.TurnSteerEntryPrepareResult, error) {
	prompt, images, files := s.extractInputs(input)
	skillPrompt, _, _ := s.buildTurnSkillPrompt(threadID, prompt, input, selectedSkills, manualSkillSelection)
	submitPrompt := s.mergePromptText(prompt, skillPrompt)
	return codexadapter.TurnSteerEntryPrepareResult{
		SubmitPrompt: submitPrompt,
		Images:       images,
		Files:        files,
	}, nil
}

func (s *Server) turnSteerTyped(_ context.Context, p turnSteerParams) (any, error) {
	return s.codexAdapter.TurnSteerEntry(codexadapter.TurnSteerEntryOptions{
		ThreadID:             p.ThreadID,
		SelectedSkills:       p.SelectedSkills,
		ManualSkillSelection: p.ManualSkillSelection,
		NormalizeSkillNames:  normalizeSkillNames,
		PrepareSubmission: func(threadID string, selectedSkills []string, manualSkillSelection bool) (codexadapter.TurnSteerEntryPrepareResult, error) {
			return s.prepareTurnSteerSubmission(threadID, p.Input, selectedSkills, manualSkillSelection)
		},
		WithThread: s.withThread,
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

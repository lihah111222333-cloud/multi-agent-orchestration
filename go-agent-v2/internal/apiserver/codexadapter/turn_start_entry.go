package codexadapter

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// StartTurnSubmissionAndTrack 负责 submit 与 turn tracking 主流程。
func (a *Adapter) startTurnSubmissionAndTrack(
	ctx context.Context,
	threadID string,
	cwd string,
	submitPrompt string,
	images []string,
	files []string,
	outputSchema json.RawMessage,
) (string, error) {
	proc, err := a.EnsureThreadReadyForTurn(ctx, threadID, cwd)
	if err != nil {
		return "", err
	}
	logger.Info("turn/start: thread dispatch resolved",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		logger.FieldPort, proc.Client.GetPort(),
		"codex_thread_id", a.GetThreadID(proc),
	)
	submitStart := time.Now()
	logger.Warn("DIAG: turn/start: about to Submit (events may arrive before tracker setup)",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		logger.FieldPort, proc.Client.GetPort(),
		"has_active_tracked_turn", a.HasActiveTrackedTurn(threadID),
	)
	if err := a.Submit(proc, submitPrompt, images, files, outputSchema); err != nil {
		return "", apperrors.Wrap(err, "Server.turnStart", "submit prompt")
	}
	submitElapsed := time.Since(submitStart)
	logger.Warn("DIAG: turn/start: Submit returned",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		"submit_ms", submitElapsed.Milliseconds(),
		"has_active_tracked_turn", a.HasActiveTrackedTurn(threadID),
	)

	resolvedTurnID := ResolveClientActiveTurnID(proc.Client)
	if resolvedTurnID == "" {
		logger.Warn("turn/start: active turn id unavailable after submit; tracker will use synthetic id",
			logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		)
	}
	logger.Warn("DIAG: turn/start: about to beginTrackedTurn",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		"resolved_turn_id", resolvedTurnID,
		"gap_since_submit_ms", time.Since(submitStart).Milliseconds(),
		"has_active_tracked_turn", a.HasActiveTrackedTurn(threadID),
	)
	turnID := a.BeginTrackedTurn(threadID, resolvedTurnID)
	logger.Warn("DIAG: turn/start: beginTrackedTurn completed",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		"turn_id", turnID,
		"total_gap_ms", time.Since(submitStart).Milliseconds(),
	)
	return turnID, nil
}

// TurnStartRequest carries protocol params for turn/start.
type TurnStartRequest = contracts.TurnStartRequest

type TurnStartEntryResult struct {
	TurnID string
}

// TurnStart handles turn/start with constructor-time dependencies.
func (a *Adapter) TurnStart(ctx context.Context, req TurnStartRequest) (TurnStartEntryResult, error) {
	logger.Info("turn/start: request received",
		logger.FieldAgentID, req.ThreadID, logger.FieldThreadID, req.ThreadID,
		logger.FieldCwd, strings.TrimSpace(req.Cwd),
		"input_count", len(req.Input),
		"selected_skills_count", len(req.SelectedSkills),
	)
	selectedSkills, err := commonadapter.NormalizeSkillNames(req.SelectedSkills)
	if err != nil {
		return TurnStartEntryResult{}, apperrors.Wrap(err, "Server.turnStart", "normalize selected skills")
	}
	prepared, err := a.prepareTurnStartSubmission(req.ThreadID, req.Input, selectedSkills, req.ManualSkillSelection)
	if err != nil {
		return TurnStartEntryResult{}, err
	}
	logger.Info("turn/start: input prepared",
		logger.FieldAgentID, req.ThreadID, logger.FieldThreadID, req.ThreadID,
		"text_len", len(prepared.Prompt),
		"images", len(prepared.Images),
		"files", len(prepared.Files),
		"selected_skills_requested", len(selectedSkills),
		"selected_skills_injected", prepared.SelectedSkillCount,
		"manual_skill_selection", req.ManualSkillSelection,
		"auto_matched_skills", prepared.AutoMatchedSkillCount,
	)

	turnID, err := a.startTurnSubmissionAndTrack(
		ctx,
		req.ThreadID,
		req.Cwd,
		prepared.SubmitPrompt,
		prepared.Images,
		prepared.Files,
		req.OutputSchema,
	)
	if err != nil {
		return TurnStartEntryResult{}, err
	}
	a.appendTurnStartUserTimeline(ctx, req.Input, contracts.TurnAppendUserTimelineOptions{
		ThreadID:     req.ThreadID,
		Prompt:       prepared.Prompt,
		SubmitPrompt: prepared.SubmitPrompt,
		Images:       prepared.Images,
		Files:        prepared.Files,
	})
	return TurnStartEntryResult{TurnID: turnID}, nil
}

// TurnSteerRequest carries protocol params for turn/steer.
type TurnSteerRequest = contracts.TurnSteerRequest

// TurnSteerFromInput handles turn/steer with constructor-time dependencies.
func (a *Adapter) TurnSteerFromInput(req TurnSteerRequest) (map[string]any, error) {
	selectedSkills, err := commonadapter.NormalizeSkillNames(req.SelectedSkills)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.turnSteer", "normalize selected skills")
	}
	prepared, err := a.prepareTurnSteerSubmission(req.ThreadID, req.Input, selectedSkills, req.ManualSkillSelection)
	if err != nil {
		return nil, err
	}
	return a.TurnSteer(req.ThreadID, prepared.SubmitPrompt, prepared.Images, prepared.Files)
}

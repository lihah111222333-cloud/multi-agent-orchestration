package codexadapter

import (
	"context"
	"encoding/json"
	"time"

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

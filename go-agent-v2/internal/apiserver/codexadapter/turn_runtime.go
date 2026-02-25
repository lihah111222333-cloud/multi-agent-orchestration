package codexadapter

import (
	"context"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// EnsureThreadReadyForTurn 负责拉起/恢复线程进程，并处理历史会话丢失降级。
func (a *Adapter) EnsureThreadReadyForTurn(ctx context.Context, threadID, cwd string) (*runner.AgentProcess, error) {
	// D11: 总超时 45s，避免 Launch(30s)+Resume(30s) 串行导致前端 turn/start 永不回。
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	if a == nil || a.ctx == nil || a.ctx.Manager == nil {
		return nil, apperrors.New("Server.ensureThreadReady", "thread manager is not initialized")
	}
	manager := a.ctx.Manager
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New("Server.ensureThreadReady", "threadId is required")
	}
	launchCwd := strings.TrimSpace(cwd)
	if launchCwd == "" {
		launchCwd = "."
	}

	if proc := manager.Get(id); proc != nil {
		logger.Info("turn/start: using running process",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			logger.FieldPort, proc.Client.GetPort(),
			"codex_thread_id", a.GetThreadID(proc),
		)
		a.setAgentWorkDir(id, launchCwd)
		a.registerBinding(ctx, id, proc)
		return proc, nil
	}

	hasHistory := a.ThreadExistsInHistory(ctx, id)
	if !hasHistory {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s not found", id)
	}
	resumeCandidates := a.collectResumeCandidates(ctx, id)

	logger.Info("turn/start: restoring historical thread",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"has_history", hasHistory,
		logger.FieldCwd, launchCwd,
		"candidate_count", len(resumeCandidates),
		"candidates", PreviewResumeCandidates(resumeCandidates, 4),
	)

	dynamicTools := a.allDynamicToolSchemas()
	startInstructions := a.resolveStartInstructionsForLaunch(ctx, dynamicTools)

	if err := manager.Launch(ctx, id, id, "", launchCwd, startInstructions, dynamicTools); err != nil {
		// 并发补加载时可能已被其他请求拉起，二次确认后再报错。
		if proc := manager.Get(id); proc != nil {
			a.setAgentWorkDir(id, launchCwd)
			return proc, nil
		}
		return nil, apperrors.Wrapf(err, "Server.ensureThreadReady", "auto-load thread %s", id)
	}

	proc := manager.Get(id)
	if proc == nil {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s loaded but not found", id)
	}
	a.setAgentWorkDir(id, launchCwd)
	logger.Info("turn/start: process launched for restore",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		logger.FieldPort, proc.Client.GetPort(),
		"codex_thread_id_before_resume", a.GetThreadID(proc),
	)
	if len(resumeCandidates) == 0 {
		logger.Warn("turn/start: no valid historical codex thread id, continue with fresh session",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
		)
		proc.MarkSessionLost()
		return proc, nil
	}

	resumed, lastResumeErr, fatalResumeErr := a.tryResumeHistoricalCandidates(ctx, manager, proc, id, launchCwd, resumeCandidates)
	if fatalResumeErr != nil {
		return nil, fatalResumeErr
	}
	if resumed {
		return proc, nil
	}

	// 所有候选的 rollout 都不可用 (非 crash) → fallback 到 fresh session + 通知前端。
	if lastResumeErr != nil {
		logger.Warn("turn/start: all resume candidates exhausted, fallback to fresh session",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			"candidate_count", len(resumeCandidates),
			"last_error", lastResumeErr,
			logger.FieldCwd, launchCwd,
		)
		// proc 在 non-crash 路径中仍然存活，但可能被 mgr 移除。
		if manager.Get(id) == nil {
			_ = a.cancelCodeRuns(id)
			_ = manager.Stop(id)
			if launchErr := manager.Launch(ctx, id, id, "", launchCwd, startInstructions, dynamicTools); launchErr != nil {
				return nil, apperrors.Wrapf(launchErr, "Server.ensureThreadReady", "final re-spawn thread %s", id)
			}
			proc = manager.Get(id)
			if proc == nil {
				return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s final re-spawn failed", id)
			}
		}
		proc.MarkSessionLost()
		a.notifySessionLost(id, lastResumeErr)
		a.registerBinding(ctx, id, proc)
		return proc, nil
	}

	logger.Warn("turn/start: no available historical rollout, continue with fresh session",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"candidate_count", len(resumeCandidates),
		logger.FieldCwd, launchCwd,
	)
	proc.MarkSessionLost()
	a.registerBinding(ctx, id, proc)
	return proc, nil
}


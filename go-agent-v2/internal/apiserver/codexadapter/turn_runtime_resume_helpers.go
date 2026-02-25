package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (a *Adapter) collectResumeCandidates(ctx context.Context, agentID string) []string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil
	}
	resumeCandidates := make([]string, 0, 4)
	if a != nil && a.ctx != nil && a.ctx.BindingStore != nil {
		if binding, err := a.ctx.BindingStore.FindByAgentID(ctx, id); err == nil && binding != nil {
			resumeCandidates = append(resumeCandidates, binding.CodexThreadID)
			logger.Info("turn/start: found DB binding",
				logger.FieldAgentID, id,
				"bound_codex_thread_id", binding.CodexThreadID,
			)
		}
	}
	if len(resumeCandidates) == 0 {
		resumeCandidates = append(resumeCandidates, a.ResolveCodexThreadCandidates(ctx, id, appendUniqueThreadIDFallback, PreviewResumeCandidates)...)
	}
	return resumeCandidates
}

func (a *Adapter) tryResumeHistoricalCandidates(
	ctx context.Context,
	manager *runner.AgentManager,
	proc *runner.AgentProcess,
	agentID string,
	launchCwd string,
	resumeCandidates []string,
) (resumed bool, lastResumeErr error, fatalErr error) {
	id := strings.TrimSpace(agentID)
	if id == "" || proc == nil {
		return false, nil, nil
	}
	for _, resumeThreadID := range resumeCandidates {
		err := a.ResumeThread(proc, agentcore.ResumeThreadRequest{
			ThreadID: resumeThreadID,
			Cwd:      launchCwd,
		})
		if err == nil {
			logger.Info("turn/start: historical thread auto-loaded",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				"codex_thread_id_after_resume", a.GetThreadID(proc),
				logger.FieldCwd, launchCwd,
			)
			a.registerBinding(ctx, id, proc)
			return true, nil, nil
		}

		lastResumeErr = err
		if IsCodexProcessCrashError(err) {
			logger.Error("turn/start: codex crashed during resume, returning error",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)
			_ = a.cancelCodeRuns(id)
			if manager != nil {
				_ = manager.Stop(id)
			}
			a.notifySessionLost(id, err)
			return false, lastResumeErr, apperrors.Wrapf(err, "Server.ensureThreadReady",
				"codex crashed while resuming thread %s (rollout=%s)", id, resumeThreadID)
		}

		if IsHistoricalResumeCandidateError(err) {
			logger.Warn("turn/start: resume candidate unavailable, try next",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)
			continue
		}

		logger.Error("turn/start: unrecognized resume error",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			"resume_thread_id", resumeThreadID,
			logger.FieldError, err,
		)
		return false, lastResumeErr, apperrors.Wrapf(err, "Server.ensureThreadReady",
			"resume failed for thread %s (rollout=%s)", id, resumeThreadID)
	}
	return false, lastResumeErr, nil
}

package codexadapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ThreadStartResult is the normalized thread/start payload.
type ThreadStartResult struct {
	ThreadID       string
	Status         string
	Model          string
	ModelProvider  string
	Cwd            string
	ApprovalPolicy string
}

// ThreadStart launches thread runtime and syncs UI snapshots.
func (a *Adapter) ThreadStart(
	ctx context.Context,
	threadID string,
	cwd string,
	model string,
	modelProvider string,
	approvalPolicy string,
) (ThreadStartResult, error) {
	result := ThreadStartResult{
		ThreadID:       strings.TrimSpace(threadID),
		Status:         "running",
		Model:          model,
		ModelProvider:  modelProvider,
		Cwd:            strings.TrimSpace(cwd),
		ApprovalPolicy: approvalPolicy,
	}
	if result.ThreadID == "" {
		return ThreadStartResult{}, apperrors.New("Server.threadStart", "threadId is required")
	}
	if result.Cwd == "" {
		result.Cwd = "."
	}
	if a == nil || a.ctx == nil || a.ctx.Manager == nil {
		return ThreadStartResult{}, apperrors.New("Server.threadStart", "thread manager is not initialized")
	}
	manager := a.ctx.Manager
	dynamicTools := a.allDynamicToolSchemas()
	startInstructions := a.resolveStartInstructionsForLaunch(ctx, dynamicTools)

	if err := manager.Launch(ctx, result.ThreadID, result.ThreadID, "", result.Cwd, startInstructions, dynamicTools); err != nil {
		return ThreadStartResult{}, apperrors.Wrap(err, "Server.threadStart", "launch thread")
	}
	if proc := manager.Get(result.ThreadID); proc != nil {
		a.registerBinding(ctx, result.ThreadID, proc)
	}
	if runtime := a.ctx.UIRuntime; runtime != nil {
		runtime.ReplaceThreads(toThreadSnapshots(manager.List()))
	}
	return result, nil
}

// ThreadResumeResult is the normalized thread/resume payload.
type ThreadResumeResult struct {
	ThreadID string
	Status   string
	Model    string
}

// ThreadResume resumes a historical codex thread by candidate probing.
func (a *Adapter) ThreadResume(ctx context.Context, threadID, path, cwd, model string) (ThreadResumeResult, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return ThreadResumeResult{}, apperrors.New("Server.threadResume", "threadId is required")
	}
	return withProcess(a, "Server.threadResume", threadID, func(proc *runner.AgentProcess) (ThreadResumeResult, error) {
		resolved := a.ResolveCodexThreadCandidates(ctx, threadID, appendUniqueThreadIDFallback, PreviewResumeCandidates)
		candidates := BuildResumeCandidates(threadID, resolved, NormalizeCodexThreadID)
		logger.Info("thread/resume: resolved candidates",
			logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
			"candidate_count", len(candidates),
			"candidates", PreviewResumeCandidates(candidates, 4),
			"cwd", strings.TrimSpace(cwd),
		)

		_, resumeErr := TryResumeCandidates(candidates, threadID, func(id string) error {
			return a.ResumeThread(proc, agentcore.ResumeThreadRequest{
				ThreadID: id,
				Path:     path,
				Cwd:      cwd,
			})
		}, IsHistoricalResumeCandidateError)
		if resumeErr != nil {
			return ThreadResumeResult{}, apperrors.Wrap(resumeErr, "Server.threadResume", "resume thread")
		}
		return ThreadResumeResult{
			ThreadID: threadID,
			Status:   "resumed",
			Model:    model,
		}, nil
	})
}

// ThreadForkResult is the normalized thread/fork payload.
type ThreadForkResult struct {
	ThreadID   string
	ForkedFrom string
}

// ThreadFork creates a fork from source thread.
func (a *Adapter) ThreadFork(threadID string) (ThreadForkResult, error) {
	sourceThreadID := strings.TrimSpace(threadID)
	return withProcess(a, "Server.threadFork", sourceThreadID,
		func(proc *runner.AgentProcess) (ThreadForkResult, error) {
			resp, forkErr := a.ForkThread(proc, agentcore.ForkThreadRequest{
				SourceThreadID: sourceThreadID,
			})
			if forkErr != nil {
				return ThreadForkResult{}, apperrors.Wrap(forkErr, "Server.threadFork", "fork thread")
			}
			newID := ""
			if resp != nil {
				newID = strings.TrimSpace(resp.ThreadID)
			}
			if newID == "" {
				newID = fmt.Sprintf("thread-%d", a.nowUnixMilli())
			}
			return ThreadForkResult{
				ThreadID:   newID,
				ForkedFrom: sourceThreadID,
			}, nil
		})
}

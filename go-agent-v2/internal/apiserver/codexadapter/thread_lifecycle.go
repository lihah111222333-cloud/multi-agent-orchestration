package codexadapter

import (
	"context"
	"fmt"
	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"regexp"
	"strings"
)

// threadStartResult is the normalized thread/start payload.
type threadStartResult struct {
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
) (threadStartResult, error) {
	id, err := requireThreadID("Server.threadStart", threadID)
	if err != nil {
		return threadStartResult{}, err
	}
	result := threadStartResult{
		ThreadID:       id,
		Status:         "running",
		Model:          model,
		ModelProvider:  modelProvider,
		Cwd:            strings.TrimSpace(cwd),
		ApprovalPolicy: approvalPolicy,
	}
	if result.Cwd == "" {
		result.Cwd = "."
	}
	manager := a.manager()
	if manager == nil {
		return threadStartResult{}, apperrors.New("Server.threadStart", "thread manager is not initialized")
	}
	dynamicTools := a.allDynamicToolSchemas()
	startInstructions := a.resolveStartInstructionsForLaunch(ctx, dynamicTools)

	if err := manager.Launch(ctx, result.ThreadID, result.ThreadID, "", result.Cwd, startInstructions, dynamicTools); err != nil {
		return threadStartResult{}, apperrors.Wrap(err, "Server.threadStart", "launch thread")
	}
	if proc := manager.Get(result.ThreadID); proc != nil {
		a.registerBinding(ctx, result.ThreadID, proc)
	}
	if runtime := a.uiRuntime(); runtime != nil {
		runtime.ReplaceThreads(toThreadSnapshots(manager.List()))
	}
	return result, nil
}

// threadResumeResult is the normalized thread/resume payload.
type threadResumeResult struct {
	ThreadID string
	Status   string
	Model    string
}

// ThreadResume resumes a historical codex thread by candidate probing.
func (a *Adapter) ThreadResume(ctx context.Context, threadID, path, cwd, model string) (threadResumeResult, error) {
	id, err := requireThreadID("Server.threadResume", threadID)
	if err != nil {
		return threadResumeResult{}, err
	}
	return withProcess(a, "Server.threadResume", id, func(proc *runner.AgentProcess) (threadResumeResult, error) {
		resolved := a.ResolveCodexThreadCandidates(ctx, id, appendUniqueThreadIDFallback, PreviewResumeCandidates)
		candidates := BuildResumeCandidates(id, resolved, NormalizeCodexThreadID)
		logger.Info("thread/resume: resolved candidates",
			append(threadLogFields(id),
				"candidate_count", len(candidates),
				"candidates", PreviewResumeCandidates(candidates, 4),
				"cwd", strings.TrimSpace(cwd),
			)...,
		)

		_, resumeErr := TryResumeCandidates(candidates, id, func(id string) error {
			return a.ResumeThread(proc, agentcore.ResumeThreadRequest{
				ThreadID: id,
				Path:     path,
				Cwd:      cwd,
			})
		}, IsHistoricalResumeCandidateError)
		if resumeErr != nil {
			return threadResumeResult{}, apperrors.Wrap(resumeErr, "Server.threadResume", "resume thread")
		}
		return threadResumeResult{
			ThreadID: id,
			Status:   "resumed",
			Model:    model,
		}, nil
	})
}

// threadForkResult is the normalized thread/fork payload.
type threadForkResult struct {
	ThreadID   string
	ForkedFrom string
}

// ThreadFork creates a fork from source thread.
func (a *Adapter) ThreadFork(threadID string) (threadForkResult, error) {
	sourceThreadID := strings.TrimSpace(threadID)
	return withProcess(a, "Server.threadFork", sourceThreadID,
		func(proc *runner.AgentProcess) (threadForkResult, error) {
			resp, forkErr := a.ForkThread(proc, agentcore.ForkThreadRequest{
				SourceThreadID: sourceThreadID,
			})
			if forkErr != nil {
				return threadForkResult{}, apperrors.Wrap(forkErr, "Server.threadFork", "fork thread")
			}
			newID := ""
			if resp != nil {
				newID = strings.TrimSpace(resp.ThreadID)
			}
			if newID == "" {
				newID = fmt.Sprintf("thread-%d", a.nowUnixMilli())
			}
			return threadForkResult{
				ThreadID:   newID,
				ForkedFrom: sourceThreadID,
			}, nil
		})
}

// ThreadRollback sends /undo index command.
func (a *Adapter) ThreadRollback(threadID string, turnIndex int) (map[string]any, error) {
	return a.sendThreadCommand("Server.threadRollback", threadID, "/undo", fmt.Sprintf("%d", turnIndex), "send undo command")
}

// ReviewStart dispatches /review command.
func (a *Adapter) ReviewStart(threadID, delivery string) (map[string]any, error) {
	return a.sendThreadCommand("Server.reviewStart", threadID, "/review", delivery, "send review command")
}

// TurnSteer submits steering prompt to existing thread.
func (a *Adapter) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	return withProcess(a, "Server.turnSteer", threadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			if submitErr := a.Submit(proc, submitPrompt, images, files, nil); submitErr != nil {
				return nil, submitErr
			}
			return map[string]any{}, nil
		})
}

func (a *Adapter) sendThreadCommand(methodName, threadID, command, args, wrapMsg string) (map[string]any, error) {
	return withProcess(a, methodName, threadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			if cmdErr := a.SendCommand(proc, command, args); cmdErr != nil {
				return nil, apperrors.Wrap(cmdErr, methodName, wrapMsg)
			}
			return map[string]any{}, nil
		})
}

// ThreadNameSet sets codex thread name and persists alias.
func (a *Adapter) ThreadNameSet(ctx context.Context, threadID, name string) (map[string]any, error) {
	id, err := requireThreadID("Server.threadNameSet", threadID)
	if err != nil {
		return nil, err
	}
	requestedName := strings.TrimSpace(name)
	persistedAlias := requestedName
	if persistedAlias == id {
		persistedAlias = ""
	}
	renameTarget := requestedName
	if renameTarget == "" {
		renameTarget = id
	}

	var proc *runner.AgentProcess
	if manager := a.manager(); manager != nil {
		proc = manager.Get(id)
	}
	existsInRuntime := a.threadExistsInRuntime(id)
	hasHistory := a.ThreadExistsInHistory(ctx, id)
	if proc == nil && !existsInRuntime && !hasHistory {
		return nil, apperrors.Newf("Server.threadNameSet", "thread %s not found", id)
	}

	if proc != nil && renameTarget != "" {
		if err := a.SendCommand(proc, "/rename", renameTarget); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "send rename command")
		}
	}

	if runtime := a.uiRuntime(); runtime != nil {
		runtime.SetThreadName(id, persistedAlias)
	}
	if err := a.persistThreadAlias(ctx, id, persistedAlias); err != nil {
		logger.Warn("thread/name/set: persist alias failed",
			logger.FieldThreadID, id,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrap(err, "Server.threadNameSet", "persist thread alias")
	}
	return map[string]any{}, nil
}

// ThreadRead fetches codex history list for the target thread.
func (a *Adapter) ThreadRead(_ context.Context, threadID string) (map[string]any, error) {
	return withProcess(a, "Server.threadRead", threadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			threads, listErr := a.ListThreads(proc)
			if listErr != nil {
				return nil, listErr
			}
			return map[string]any{"history": threads}, nil
		})
}

// ThreadResolve resolves thread identity from runtime and history sources.
func (a *Adapter) ThreadResolve(ctx context.Context, threadID string) (map[string]any, error) {
	id, err := requireThreadID("Server.threadResolve", threadID)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"threadId": id,
	}

	var codexThreadID string
	resolveSource := "history"
	if state, port, runtimeCodexThreadID, ok := a.resolveRunningThreadIdentity(id); ok {
		if state != "" {
			result["state"] = state
		}
		if port > 0 {
			result["port"] = port
		}
		codexThreadID = runtimeCodexThreadID
		resolveSource = "running"
	}
	if codexThreadID == "" {
		codexThreadID = a.firstResolvedCodexThreadID(ctx, id)
	}
	if codexThreadID != "" {
		result["codexThreadId"] = codexThreadID
	}
	if IsLikelyCodexThreadID(codexThreadID) {
		result["uuid"] = codexThreadID
	}
	hasHistory := a.ThreadExistsInHistory(ctx, id)
	result["hasHistory"] = hasHistory
	logger.Info("thread/resolve: identity resolved",
		append(threadLogFields(id),
			"source", resolveSource,
			"state", result["state"],
			logger.FieldPort, result["port"],
			"codex_thread_id", codexThreadID,
			"has_history", hasHistory,
		)...,
	)
	return result, nil
}

func (a *Adapter) firstResolvedCodexThreadID(ctx context.Context, threadID string) string {
	candidates := a.ResolveCodexThreadCandidates(ctx, threadID, appendUniqueThreadIDFallback, PreviewResumeCandidates)
	if len(candidates) == 0 {
		return ""
	}
	return strings.TrimSpace(candidates[0])
}

func (a *Adapter) resolveRunningThreadIdentity(threadID string) (state string, port int, codexThreadID string, found bool) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", 0, "", false
	}
	for _, info := range a.runningAgents() {
		if strings.TrimSpace(info.ID) != id {
			continue
		}
		return strings.TrimSpace(string(info.State)), info.Port, strings.TrimSpace(info.ThreadID), true
	}
	return "", 0, "", false
}

func (a *Adapter) threadExistsInRuntime(threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	runtime := a.uiRuntime()
	if runtime == nil {
		return false
	}
	for _, item := range runtime.SnapshotLight().Threads {
		if strings.TrimSpace(item.ID) == id {
			return true
		}
	}
	return false
}

// codexThreadIDPattern matches a lowercase UUID (codex thread ID format).
var codexThreadIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// NormalizeCodexThreadID trims, lowercases, strips "urn:uuid:" prefix,
// and validates against UUID pattern. Returns "" if invalid.
func NormalizeCodexThreadID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	id = strings.TrimPrefix(strings.ToLower(id), "urn:uuid:")
	if !codexThreadIDPattern.MatchString(id) {
		return ""
	}
	return id
}

// IsLikelyCodexThreadID reports whether raw looks like a valid codex thread ID.
func IsLikelyCodexThreadID(raw string) bool {
	return NormalizeCodexThreadID(raw) != ""
}

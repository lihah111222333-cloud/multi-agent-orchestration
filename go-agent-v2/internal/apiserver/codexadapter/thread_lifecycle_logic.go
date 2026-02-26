package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func runThreadStart(
	ctx context.Context,
	threadID string,
	cwd string,
	model string,
	modelProvider string,
	approvalPolicy string,
	manager *runner.AgentManager,
	dynamicTools []agentcore.DynamicTool,
	resolveStartInstructions func(context.Context, []agentcore.DynamicTool) string,
	registerBinding func(context.Context, string, *runner.AgentProcess),
	syncRuntimeThreads func([]runner.AgentInfo),
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
	if manager == nil {
		return threadStartResult{}, apperrors.New("Server.threadStart", "thread manager is not initialized")
	}

	startInstructions := ""
	if resolveStartInstructions != nil {
		startInstructions = resolveStartInstructions(ctx, dynamicTools)
	}
	if err := manager.Launch(ctx, result.ThreadID, result.ThreadID, "", result.Cwd, startInstructions, dynamicTools); err != nil {
		return threadStartResult{}, apperrors.Wrap(err, "Server.threadStart", "launch thread")
	}

	if registerBinding != nil {
		if proc := manager.Get(result.ThreadID); proc != nil {
			registerBinding(ctx, result.ThreadID, proc)
		}
	}
	if syncRuntimeThreads != nil {
		syncRuntimeThreads(manager.List())
	}
	return result, nil
}

func runThreadResume(
	ctx context.Context,
	threadID string,
	path string,
	cwd string,
	model string,
	proc *runner.AgentProcess,
	resolveCandidates func(context.Context, string, func([]string, map[string]struct{}, string) []string, func([]string, int) []string) []string,
	resumeThread func(*runner.AgentProcess, agentcore.ResumeThreadRequest) error,
) (threadResumeResult, error) {
	resolved := []string(nil)
	if resolveCandidates != nil {
		resolved = resolveCandidates(ctx, threadID, appendUniqueThreadIDFallback, PreviewResumeCandidates)
	}
	candidates := BuildResumeCandidates(threadID, resolved, normalizeCodexThreadID)
	logger.Info("thread/resume: resolved candidates",
		append(threadLogFields(threadID),
			"candidate_count", len(candidates),
			"candidates", PreviewResumeCandidates(candidates, 4),
			"cwd", strings.TrimSpace(cwd),
		)...,
	)
	if resumeThread == nil {
		return threadResumeResult{}, apperrors.New("Server.threadResume", "resume handler is not initialized")
	}
	_, resumeErr := TryResumeCandidates(candidates, threadID, func(id string) error {
		return resumeThread(proc, agentcore.ResumeThreadRequest{
			ThreadID: id,
			Path:     path,
			Cwd:      cwd,
		})
	}, IsHistoricalResumeCandidateError)
	if resumeErr != nil {
		return threadResumeResult{}, apperrors.Wrap(resumeErr, "Server.threadResume", "resume thread")
	}
	return threadResumeResult{ThreadID: threadID, Status: "resumed", Model: model}, nil
}

func runThreadFork(
	threadID string,
	proc *runner.AgentProcess,
	forkThread func(*runner.AgentProcess, agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error),
	nowUnixMilli func() int64,
) (threadForkResult, error) {
	sourceThreadID := strings.TrimSpace(threadID)
	if forkThread == nil {
		return threadForkResult{}, apperrors.New("Server.threadFork", "fork handler is not initialized")
	}
	resp, forkErr := forkThread(proc, agentcore.ForkThreadRequest{SourceThreadID: sourceThreadID})
	if forkErr != nil {
		return threadForkResult{}, apperrors.Wrap(forkErr, "Server.threadFork", "fork thread")
	}

	newID := ""
	if resp != nil {
		newID = strings.TrimSpace(resp.ThreadID)
	}
	if newID == "" {
		now := time.Now().UnixMilli()
		if nowUnixMilli != nil {
			now = nowUnixMilli()
		}
		newID = fmt.Sprintf("thread-%d", now)
	}
	return threadForkResult{ThreadID: newID, ForkedFrom: sourceThreadID}, nil
}

func runThreadRealtimeStart(threadID, prompt string) (map[string]any, error) {
	if _, err := requireThreadID("Server.threadRealtimeStart", threadID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, apperrors.New("Server.threadRealtimeStart", "prompt is required")
	}
	return map[string]any{}, nil
}

func runThreadRealtimeAppendAudio(threadID string, audio any) (map[string]any, error) {
	if _, err := requireThreadID("Server.threadRealtimeAppendAudio", threadID); err != nil {
		return nil, err
	}
	if audio == nil {
		return nil, apperrors.New("Server.threadRealtimeAppendAudio", "audio is required")
	}
	return map[string]any{}, nil
}

func runThreadRealtimeAppendText(threadID, text string) (map[string]any, error) {
	if _, err := requireThreadID("Server.threadRealtimeAppendText", threadID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, apperrors.New("Server.threadRealtimeAppendText", "text is required")
	}
	return map[string]any{}, nil
}

func runThreadRealtimeStop(threadID string) (map[string]any, error) {
	if _, err := requireThreadID("Server.threadRealtimeStop", threadID); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func runTurnSteer(
	proc *runner.AgentProcess,
	submit func(*runner.AgentProcess, string, []string, []string, json.RawMessage) error,
	submitPrompt string,
	images []string,
	files []string,
) (map[string]any, error) {
	if submit == nil {
		return nil, apperrors.New("Server.turnSteer", "submit handler is not initialized")
	}
	if submitErr := submit(proc, submitPrompt, images, files, nil); submitErr != nil {
		return nil, submitErr
	}
	return map[string]any{}, nil
}

func runThreadCommand(
	proc *runner.AgentProcess,
	methodName string,
	command string,
	args string,
	wrapMsg string,
	sendCommand func(*runner.AgentProcess, string, string) error,
) (map[string]any, error) {
	if sendCommand == nil {
		return nil, apperrors.New(methodName, "command sender is not initialized")
	}
	if cmdErr := sendCommand(proc, command, args); cmdErr != nil {
		return nil, apperrors.Wrap(cmdErr, methodName, wrapMsg)
	}
	return map[string]any{}, nil
}

func runThreadNameSet(
	ctx context.Context,
	threadID string,
	name string,
	manager *runner.AgentManager,
	threadExistsInRuntime func(string) bool,
	threadExistsInHistory func(context.Context, string) bool,
	sendCommand func(*runner.AgentProcess, string, string) error,
	setRuntimeThreadName func(string, string),
	persistThreadAlias func(context.Context, string, string) error,
) (map[string]any, error) {
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
	if manager != nil {
		proc = manager.Get(id)
	}
	existsInRuntime := threadExistsInRuntime != nil && threadExistsInRuntime(id)
	hasHistory := threadExistsInHistory != nil && threadExistsInHistory(ctx, id)
	if proc == nil && !existsInRuntime && !hasHistory {
		return nil, apperrors.Newf("Server.threadNameSet", "thread %s not found", id)
	}

	if proc != nil && renameTarget != "" {
		if sendCommand == nil {
			return nil, apperrors.New("Server.threadNameSet", "command sender is not initialized")
		}
		if err := sendCommand(proc, "/rename", renameTarget); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "send rename command")
		}
	}

	if setRuntimeThreadName != nil {
		setRuntimeThreadName(id, persistedAlias)
	}
	if persistThreadAlias != nil {
		if err := persistThreadAlias(ctx, id, persistedAlias); err != nil {
			logger.Warn("thread/name/set: persist alias failed",
				logger.FieldThreadID, id,
				logger.FieldError, err,
			)
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "persist thread alias")
		}
	}
	return map[string]any{}, nil
}

func runThreadRead(
	proc *runner.AgentProcess,
	listThreads func(*runner.AgentProcess) ([]agentcore.ThreadInfo, error),
) (map[string]any, error) {
	if listThreads == nil {
		return nil, apperrors.New("Server.threadRead", "thread list handler is not initialized")
	}
	threads, err := listThreads(proc)
	if err != nil {
		return nil, err
	}
	return map[string]any{"history": threads}, nil
}

func runThreadResolve(
	ctx context.Context,
	threadID string,
	resolveRunningThreadIdentity func(string) (string, int, string, bool),
	firstResolvedCodexThreadID func(context.Context, string) string,
	threadExistsInHistory func(context.Context, string) bool,
) (map[string]any, error) {
	id, err := requireThreadID("Server.threadResolve", threadID)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"threadId": id}

	codexThreadID := ""
	resolveSource := "history"
	if resolveRunningThreadIdentity != nil {
		if state, port, runtimeCodexThreadID, ok := resolveRunningThreadIdentity(id); ok {
			if state != "" {
				result["state"] = state
			}
			if port > 0 {
				result["port"] = port
			}
			codexThreadID = runtimeCodexThreadID
			resolveSource = "running"
		}
	}
	if codexThreadID == "" && firstResolvedCodexThreadID != nil {
		codexThreadID = firstResolvedCodexThreadID(ctx, id)
	}
	if codexThreadID != "" {
		result["codexThreadId"] = codexThreadID
	}
	if isLikelyCodexThreadID(codexThreadID) {
		result["uuid"] = codexThreadID
	}
	hasHistory := threadExistsInHistory != nil && threadExistsInHistory(ctx, id)
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

func firstResolvedCodexThreadIDFromCandidates(
	ctx context.Context,
	threadID string,
	resolveCandidates func(context.Context, string, func([]string, map[string]struct{}, string) []string, func([]string, int) []string) []string,
) string {
	if resolveCandidates == nil {
		return ""
	}
	candidates := resolveCandidates(ctx, threadID, appendUniqueThreadIDFallback, PreviewResumeCandidates)
	if len(candidates) == 0 {
		return ""
	}
	return strings.TrimSpace(candidates[0])
}

func resolveRunningThreadIdentityFromAgents(threadID string, agents []runner.AgentInfo) (state string, port int, codexThreadID string, found bool) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", 0, "", false
	}
	for _, info := range agents {
		if strings.TrimSpace(info.ID) != id {
			continue
		}
		return strings.TrimSpace(string(info.State)), info.Port, strings.TrimSpace(info.ThreadID), true
	}
	return "", 0, "", false
}

func threadExistsInRuntimeSnapshots(threadID string, snapshots []uistate.ThreadSnapshot) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	for _, item := range snapshots {
		if strings.TrimSpace(item.ID) == id {
			return true
		}
	}
	return false
}

package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type AgentInfo struct {
	ID, Name, State, ThreadID string
	Port                      int
}

type ThreadSnapshot struct{ ID string }

type ThreadStartResult struct {
	ThreadID, Status, Model, ModelProvider, Cwd, ApprovalPolicy string
}

type ThreadResumeResult struct{ ThreadID, Status, Model string }

type ThreadForkResult struct{ ThreadID, ForkedFrom string }

func RunThreadStart(
	ctx context.Context, threadID, cwd, model, modelProvider, approvalPolicy string, dynamicTools []agentcore.DynamicTool,
	launchThread func(context.Context, string, string, string, string, string, []agentcore.DynamicTool) error,
	getProcess func(string) any, listAgents func() []AgentInfo, resolveStartInstructions func(context.Context, []agentcore.DynamicTool) string,
	registerBinding func(context.Context, string, any), syncRuntimeThreads func([]AgentInfo),
) (ThreadStartResult, error) {
	id, err := common.RequireThreadID("Server.threadStart", threadID)
	if err != nil {
		return ThreadStartResult{}, err
	}
	startCwd := strings.TrimSpace(cwd)
	if startCwd == "" {
		startCwd = "."
	}
	result := ThreadStartResult{
		ThreadID:       id,
		Status:         "running",
		Model:          model,
		ModelProvider:  modelProvider,
		Cwd:            startCwd,
		ApprovalPolicy: approvalPolicy,
	}
	if launchThread == nil {
		return ThreadStartResult{}, apperrors.New("Server.threadStart", "thread launcher is not initialized")
	}
	startInstructions := ""
	if resolveStartInstructions != nil {
		startInstructions = resolveStartInstructions(ctx, dynamicTools)
	}
	if err := launchThread(ctx, result.ThreadID, result.ThreadID, "", result.Cwd, startInstructions, dynamicTools); err != nil {
		return ThreadStartResult{}, apperrors.Wrap(err, "Server.threadStart", "launch thread")
	}
	if registerBinding != nil && getProcess != nil {
		if proc := getProcess(result.ThreadID); proc != nil {
			registerBinding(ctx, result.ThreadID, proc)
		}
	}
	if syncRuntimeThreads != nil && listAgents != nil {
		syncRuntimeThreads(listAgents())
	}
	return result, nil
}

func RunThreadResume(
	ctx context.Context, threadID, path, cwd, model string, proc *codexsdk.AgentProcess,
	resolveCandidates func(context.Context, string, func([]string, map[string]struct{}, string) []string, func([]string, int) []string) []string,
	normalizeThreadID func(string) string, resumeThread func(*codexsdk.AgentProcess, agentcore.ResumeThreadRequest) error,
) (ThreadResumeResult, error) {
	candidates := BuildResumeCandidates(threadID, nil, normalizeThreadID)
	if resolveCandidates != nil {
		candidates = BuildResumeCandidates(threadID, resolveCandidates(ctx, threadID, common.AppendUniqueThreadIDFallback, PreviewResumeCandidates), normalizeThreadID)
	}
	logger.Info("thread/resume: resolved candidates",
		append(common.ThreadLogFields(threadID), "candidate_count", len(candidates), "candidates", PreviewResumeCandidates(candidates, 4), "cwd", strings.TrimSpace(cwd))...,
	)
	if resumeThread == nil {
		return ThreadResumeResult{}, apperrors.New("Server.threadResume", "resume handler is not initialized")
	}
	if _, err := TryResumeCandidates(candidates, threadID, func(id string) error {
		return resumeThread(proc, agentcore.ResumeThreadRequest{ThreadID: id, Path: path, Cwd: cwd})
	}, IsHistoricalResumeCandidateError); err != nil {
		return ThreadResumeResult{}, apperrors.Wrap(err, "Server.threadResume", "resume thread")
	}
	return ThreadResumeResult{ThreadID: threadID, Status: "resumed", Model: model}, nil
}

func RunThreadFork(threadID string, proc *codexsdk.AgentProcess, forkThread func(*codexsdk.AgentProcess, agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error), nowUnixMilli func() int64) (ThreadForkResult, error) {
	sourceThreadID := strings.TrimSpace(threadID)
	if forkThread == nil {
		return ThreadForkResult{}, apperrors.New("Server.threadFork", "fork handler is not initialized")
	}
	resp, forkErr := forkThread(proc, agentcore.ForkThreadRequest{SourceThreadID: sourceThreadID})
	if forkErr != nil {
		return ThreadForkResult{}, apperrors.Wrap(forkErr, "Server.threadFork", "fork thread")
	}
	return ThreadForkResult{ThreadID: fallbackForkThreadID(resp, nowUnixMilli), ForkedFrom: sourceThreadID}, nil
}

func RunThreadRealtimeStart(threadID, prompt string) (map[string]any, error) {
	return runThreadRealtimeAction("Server.threadRealtimeStart", threadID, func() error {
		if strings.TrimSpace(prompt) == "" {
			return apperrors.New("Server.threadRealtimeStart", "prompt is required")
		}
		return nil
	})
}

func RunThreadRealtimeAppendAudio(threadID string, audio any) (map[string]any, error) {
	return runThreadRealtimeAction("Server.threadRealtimeAppendAudio", threadID, func() error {
		if audio == nil {
			return apperrors.New("Server.threadRealtimeAppendAudio", "audio is required")
		}
		return nil
	})
}

func RunThreadRealtimeAppendText(threadID, text string) (map[string]any, error) {
	return runThreadRealtimeAction("Server.threadRealtimeAppendText", threadID, func() error {
		if strings.TrimSpace(text) == "" {
			return apperrors.New("Server.threadRealtimeAppendText", "text is required")
		}
		return nil
	})
}

func RunThreadRealtimeStop(threadID string) (map[string]any, error) {
	return runThreadRealtimeAction("Server.threadRealtimeStop", threadID, nil)
}

func RunTurnSteer(
	proc *codexsdk.AgentProcess, submit func(*codexsdk.AgentProcess, string, []string, []string, json.RawMessage) error, submitPrompt string, images, files []string,
) (map[string]any, error) {
	return runNoContentAction("Server.turnSteer", "submit handler is not initialized", "", submit != nil, func() error {
		return submit(proc, submitPrompt, images, files, nil)
	})
}

func RunThreadCommand(proc *codexsdk.AgentProcess, methodName, command, args, wrapMsg string, sendCommand func(*codexsdk.AgentProcess, string, string) error) (map[string]any, error) {
	return runNoContentAction(methodName, "command sender is not initialized", wrapMsg, sendCommand != nil, func() error {
		return sendCommand(proc, command, args)
	})
}

func RunThreadNameSet(
	ctx context.Context, threadID, name string, getProcess func(string) any,
	threadExistsInRuntime func(string) bool, threadExistsInHistory func(context.Context, string) bool,
	sendCommand func(any, string, string) error, setRuntimeThreadName func(string, string), persistThreadAlias func(context.Context, string, string) error,
) (map[string]any, error) {
	id, err := common.RequireThreadID("Server.threadNameSet", threadID)
	if err != nil {
		return nil, err
	}
	requestedName := strings.TrimSpace(name)
	renameTarget := requestedName
	if renameTarget == "" {
		renameTarget = id
	}
	persistedAlias := requestedName
	if persistedAlias == id {
		persistedAlias = ""
	}
	var proc any
	if getProcess != nil {
		proc = getProcess(id)
	}
	if proc == nil &&
		(threadExistsInRuntime == nil || !threadExistsInRuntime(id)) &&
		(threadExistsInHistory == nil || !threadExistsInHistory(ctx, id)) {
		return nil, apperrors.Newf("Server.threadNameSet", "thread %s not found", id)
	}
	if proc != nil && renameTarget != "" {
		if _, err := runNoContentAction("Server.threadNameSet", "command sender is not initialized", "send rename command", sendCommand != nil, func() error {
			return sendCommand(proc, "/rename", renameTarget)
		}); err != nil {
			return nil, err
		}
	}
	if setRuntimeThreadName != nil {
		setRuntimeThreadName(id, persistedAlias)
	}
	if persistThreadAlias != nil {
		if err := persistThreadAlias(ctx, id, persistedAlias); err != nil {
			logger.Warn("thread/name/set: persist alias failed", logger.FieldThreadID, id, logger.FieldError, err)
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "persist thread alias")
		}
	}
	return map[string]any{}, nil
}

func RunThreadRead(proc *codexsdk.AgentProcess, listThreads func(*codexsdk.AgentProcess) ([]agentcore.ThreadInfo, error)) (map[string]any, error) {
	if listThreads == nil {
		return nil, apperrors.New("Server.threadRead", "thread list handler is not initialized")
	}
	threads, err := listThreads(proc)
	if err != nil {
		return nil, err
	}
	return map[string]any{"history": threads}, nil
}

func RunThreadResolve(
	ctx context.Context, threadID string, resolveRunningThreadIdentity func(string) (string, int, string, bool),
	firstResolvedCodexThreadID func(context.Context, string) string, threadExistsInHistory func(context.Context, string) bool,
) (map[string]any, error) {
	id, err := common.RequireThreadID("Server.threadResolve", threadID)
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
	if IsLikelyCodexThreadID(codexThreadID) {
		result["uuid"] = codexThreadID
	}
	hasHistory := threadExistsInHistory != nil && threadExistsInHistory(ctx, id)
	result["hasHistory"] = hasHistory
	logger.Info("thread/resolve: identity resolved",
		append(common.ThreadLogFields(id), "source", resolveSource, "state", result["state"], logger.FieldPort, result["port"], "codex_thread_id", codexThreadID, "has_history", hasHistory)...,
	)
	return result, nil
}

func FirstResolvedCodexThreadIDFromCandidates(
	ctx context.Context, threadID string,
	resolveCandidates func(context.Context, string, func([]string, map[string]struct{}, string) []string, func([]string, int) []string) []string,
) string {
	if resolveCandidates == nil {
		return ""
	}
	if candidates := resolveCandidates(ctx, threadID, common.AppendUniqueThreadIDFallback, PreviewResumeCandidates); len(candidates) > 0 {
		return strings.TrimSpace(candidates[0])
	}
	return ""
}

func ResolveRunningThreadIdentityFromAgents(threadID string, agents []AgentInfo) (state string, port int, codexThreadID string, found bool) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", 0, "", false
	}
	for _, info := range agents {
		if strings.TrimSpace(info.ID) == id || strings.TrimSpace(info.ThreadID) == id {
			return strings.TrimSpace(info.State), info.Port, strings.TrimSpace(info.ThreadID), true
		}
	}
	return "", 0, "", false
}

func ThreadExistsInRuntimeSnapshots(threadID string, snapshots []ThreadSnapshot) bool {
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

var codexThreadIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func NormalizeCodexThreadID(raw string) string {
	id := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "urn:uuid:")
	if codexThreadIDPattern.MatchString(id) {
		return id
	}
	return ""
}

func IsLikelyCodexThreadID(raw string) bool {
	return NormalizeCodexThreadID(raw) != ""
}

func fallbackForkThreadID(resp *agentcore.ForkThreadResponse, nowUnixMilli func() int64) string {
	if resp != nil {
		if id := strings.TrimSpace(resp.ThreadID); id != "" {
			return id
		}
	}
	now := time.Now().UnixMilli()
	if nowUnixMilli != nil {
		now = nowUnixMilli()
	}
	return fmt.Sprintf("thread-%d", now)
}

func runThreadRealtimeAction(method, threadID string, validate func() error) (map[string]any, error) {
	if _, err := common.RequireThreadID(method, threadID); err != nil {
		return nil, err
	}
	if validate != nil {
		if err := validate(); err != nil {
			return nil, err
		}
	}
	return map[string]any{}, nil
}

func runNoContentAction(method, missingHandlerMsg, wrapMsg string, handlerReady bool, invoke func() error) (map[string]any, error) {
	if !handlerReady {
		return nil, apperrors.New(method, missingHandlerMsg)
	}
	if err := invoke(); err != nil {
		if wrapMsg == "" {
			return nil, err
		}
		return nil, apperrors.Wrap(err, method, wrapMsg)
	}
	return map[string]any{}, nil
}

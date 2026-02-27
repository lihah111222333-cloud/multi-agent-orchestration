package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/common"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type AgentInfo struct {
	ID       string
	Name     string
	State    string
	Port     int
	ThreadID string
}

type ThreadSnapshot struct {
	ID string
}

type ThreadStartResult struct {
	ThreadID       string
	Status         string
	Model          string
	ModelProvider  string
	Cwd            string
	ApprovalPolicy string
}

type ThreadResumeResult struct {
	ThreadID string
	Status   string
	Model    string
}

type ThreadForkResult struct {
	ThreadID   string
	ForkedFrom string
}

func RunThreadStart(
	ctx context.Context,
	threadID string,
	cwd string,
	model string,
	modelProvider string,
	approvalPolicy string,
	dynamicTools []agentcore.DynamicTool,
	launchThread func(context.Context, string, string, string, string, string, []agentcore.DynamicTool) error,
	getProcess func(string) any,
	listAgents func() []AgentInfo,
	resolveStartInstructions func(context.Context, []agentcore.DynamicTool) string,
	registerBinding func(context.Context, string, any),
	syncRuntimeThreads func([]AgentInfo),
) (ThreadStartResult, error) {
	id, err := common.RequireThreadID("Server.threadStart", threadID)
	if err != nil {
		return ThreadStartResult{}, err
	}
	result := ThreadStartResult{
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
	ctx context.Context,
	threadID string,
	path string,
	cwd string,
	model string,
	proc any,
	resolveCandidates func(context.Context, string, func([]string, map[string]struct{}, string) []string, func([]string, int) []string) []string,
	normalizeThreadID func(string) string,
	resumeThread func(any, agentcore.ResumeThreadRequest) error,
) (ThreadResumeResult, error) {
	resolved := []string(nil)
	if resolveCandidates != nil {
		resolved = resolveCandidates(ctx, threadID, common.AppendUniqueThreadIDFallback, PreviewResumeCandidates)
	}
	candidates := BuildResumeCandidates(threadID, resolved, normalizeThreadID)
	logger.Info("thread/resume: resolved candidates",
		append(common.ThreadLogFields(threadID), "candidate_count", len(candidates), "candidates", PreviewResumeCandidates(candidates, 4), "cwd", strings.TrimSpace(cwd))...,
	)
	if resumeThread == nil {
		return ThreadResumeResult{}, apperrors.New("Server.threadResume", "resume handler is not initialized")
	}
	_, resumeErr := TryResumeCandidates(candidates, threadID, func(id string) error {
		return resumeThread(proc, agentcore.ResumeThreadRequest{
			ThreadID: id,
			Path:     path,
			Cwd:      cwd,
		})
	}, IsHistoricalResumeCandidateError)
	if resumeErr != nil {
		return ThreadResumeResult{}, apperrors.Wrap(resumeErr, "Server.threadResume", "resume thread")
	}
	return ThreadResumeResult{ThreadID: threadID, Status: "resumed", Model: model}, nil
}

func RunThreadFork(
	threadID string,
	proc any,
	forkThread func(any, agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error),
	nowUnixMilli func() int64,
) (ThreadForkResult, error) {
	sourceThreadID := strings.TrimSpace(threadID)
	if forkThread == nil {
		return ThreadForkResult{}, apperrors.New("Server.threadFork", "fork handler is not initialized")
	}
	resp, forkErr := forkThread(proc, agentcore.ForkThreadRequest{SourceThreadID: sourceThreadID})
	if forkErr != nil {
		return ThreadForkResult{}, apperrors.Wrap(forkErr, "Server.threadFork", "fork thread")
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
	return ThreadForkResult{ThreadID: newID, ForkedFrom: sourceThreadID}, nil
}

func RunThreadRealtimeStart(threadID, prompt string) (map[string]any, error) {
	if _, err := common.RequireThreadID("Server.threadRealtimeStart", threadID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, apperrors.New("Server.threadRealtimeStart", "prompt is required")
	}
	return map[string]any{}, nil
}

func RunThreadRealtimeAppendAudio(threadID string, audio any) (map[string]any, error) {
	if _, err := common.RequireThreadID("Server.threadRealtimeAppendAudio", threadID); err != nil {
		return nil, err
	}
	if audio == nil {
		return nil, apperrors.New("Server.threadRealtimeAppendAudio", "audio is required")
	}
	return map[string]any{}, nil
}

func RunThreadRealtimeAppendText(threadID, text string) (map[string]any, error) {
	if _, err := common.RequireThreadID("Server.threadRealtimeAppendText", threadID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, apperrors.New("Server.threadRealtimeAppendText", "text is required")
	}
	return map[string]any{}, nil
}

func RunThreadRealtimeStop(threadID string) (map[string]any, error) {
	if _, err := common.RequireThreadID("Server.threadRealtimeStop", threadID); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func RunTurnSteer(
	proc any,
	submit func(any, string, []string, []string, json.RawMessage) error,
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

func RunThreadCommand(
	proc any,
	methodName string,
	command string,
	args string,
	wrapMsg string,
	sendCommand func(any, string, string) error,
) (map[string]any, error) {
	if sendCommand == nil {
		return nil, apperrors.New(methodName, "command sender is not initialized")
	}
	if cmdErr := sendCommand(proc, command, args); cmdErr != nil {
		return nil, apperrors.Wrap(cmdErr, methodName, wrapMsg)
	}
	return map[string]any{}, nil
}

func RunThreadNameSet(
	ctx context.Context,
	threadID string,
	name string,
	getProcess func(string) any,
	threadExistsInRuntime func(string) bool,
	threadExistsInHistory func(context.Context, string) bool,
	sendCommand func(any, string, string) error,
	setRuntimeThreadName func(string, string),
	persistThreadAlias func(context.Context, string, string) error,
) (map[string]any, error) {
	id, err := common.RequireThreadID("Server.threadNameSet", threadID)
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
	var proc any
	if getProcess != nil {
		proc = getProcess(id)
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
			logger.Warn("thread/name/set: persist alias failed", logger.FieldThreadID, id, logger.FieldError, err)
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "persist thread alias")
		}
	}
	return map[string]any{}, nil
}

func RunThreadRead(
	proc any,
	listThreads func(any) ([]agentcore.ThreadInfo, error),
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

func RunThreadResolve(
	ctx context.Context,
	threadID string,
	resolveRunningThreadIdentity func(string) (string, int, string, bool),
	firstResolvedCodexThreadID func(context.Context, string) string,
	threadExistsInHistory func(context.Context, string) bool,
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
	ctx context.Context,
	threadID string,
	resolveCandidates func(context.Context, string, func([]string, map[string]struct{}, string) []string, func([]string, int) []string) []string,
) string {
	if resolveCandidates == nil {
		return ""
	}
	candidates := resolveCandidates(ctx, threadID, common.AppendUniqueThreadIDFallback, PreviewResumeCandidates)
	if len(candidates) == 0 {
		return ""
	}
	return strings.TrimSpace(candidates[0])
}

func ResolveRunningThreadIdentityFromAgents(threadID string, agents []AgentInfo) (state string, port int, codexThreadID string, found bool) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", 0, "", false
	}
	for _, info := range agents {
		if strings.TrimSpace(info.ID) != id {
			continue
		}
		return strings.TrimSpace(info.State), info.Port, strings.TrimSpace(info.ThreadID), true
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

func IsLikelyCodexThreadID(raw string) bool {
	return NormalizeCodexThreadID(raw) != ""
}

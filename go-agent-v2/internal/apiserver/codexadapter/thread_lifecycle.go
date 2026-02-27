package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	lifecycleconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/lifecycle"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// threadStartResult is the normalized thread/start payload.
type threadStartResult = lifecycleconsumer.ThreadStartResult

// ThreadStart launches thread runtime and syncs UI snapshots.
func (a *Adapter) ThreadStart(ctx context.Context, threadID, cwd, model, modelProvider, approvalPolicy string) (threadStartResult, error) {
	return lifecycleconsumer.RunThreadStart(ctx, threadID, cwd, model, modelProvider, approvalPolicy, a.allDynamicToolSchemas(), a.launchThreadRuntime, a.managerProcess, a.lifecycleAgents, a.resolveStartInstructionsForLaunch, a.registerBindingFromAny, a.syncRuntimeThreads)
}

// threadResumeResult is the normalized thread/resume payload.
type threadResumeResult = lifecycleconsumer.ThreadResumeResult

// ThreadResume resumes a historical codex thread by candidate probing.
func (a *Adapter) ThreadResume(ctx context.Context, threadID, path, cwd, model string) (threadResumeResult, error) {
	id, err := requireThreadID("Server.threadResume", threadID)
	if err != nil {
		return threadResumeResult{}, err
	}
	return withProcess(a, "Server.threadResume", id, func(proc *codexsdk.AgentProcess) (threadResumeResult, error) {
		return lifecycleconsumer.RunThreadResume(ctx, id, path, cwd, model, proc, a.ResolveCodexThreadCandidates, lifecycleconsumer.NormalizeCodexThreadID, a.resumeThreadFromAny)
	})
}

// threadForkResult is the normalized thread/fork payload.
type threadForkResult = lifecycleconsumer.ThreadForkResult

// ThreadFork creates a fork from source thread.
func (a *Adapter) ThreadFork(threadID string) (threadForkResult, error) {
	sourceThreadID := strings.TrimSpace(threadID)
	return withProcess(a, "Server.threadFork", sourceThreadID, func(proc *codexsdk.AgentProcess) (threadForkResult, error) {
		return lifecycleconsumer.RunThreadFork(sourceThreadID, proc, func(proc any, req codexsdk.ForkThreadRequest) (*codexsdk.ForkThreadResponse, error) {
			typed, _ := proc.(*codexsdk.AgentProcess)
			return a.ForkThread(typed, req)
		}, a.nowUnixMilli)
	})
}

// ThreadRollback sends /undo count command.
func (a *Adapter) ThreadRollback(threadID string, numTurns int) (map[string]any, error) {
	return a.sendThreadCommand("Server.threadRollback", threadID, "/undo", fmt.Sprintf("%d", numTurns), "send undo command")
}

// ReviewStart dispatches /review command.
func (a *Adapter) ReviewStart(threadID, reviewArgs string) (map[string]any, error) {
	return a.sendThreadCommand("Server.reviewStart", threadID, "/review", reviewArgs, "send review command")
}

// ThreadRealtimeStart validates and starts a realtime thread flow.
func (a *Adapter) ThreadRealtimeStart(threadID, prompt string, _ *string) (map[string]any, error) {
	return lifecycleconsumer.RunThreadRealtimeStart(threadID, prompt)
}

// ThreadRealtimeAppendAudio validates realtime audio append payload.
func (a *Adapter) ThreadRealtimeAppendAudio(threadID string, audio any) (map[string]any, error) {
	return lifecycleconsumer.RunThreadRealtimeAppendAudio(threadID, audio)
}

// ThreadRealtimeAppendText validates realtime text append payload.
func (a *Adapter) ThreadRealtimeAppendText(threadID, text string) (map[string]any, error) {
	return lifecycleconsumer.RunThreadRealtimeAppendText(threadID, text)
}

// ThreadRealtimeStop validates realtime stop payload.
func (a *Adapter) ThreadRealtimeStop(threadID string) (map[string]any, error) {
	return lifecycleconsumer.RunThreadRealtimeStop(threadID)
}

// TurnSteer submits steering prompt to existing thread.
func (a *Adapter) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	return withProcess(a, "Server.turnSteer", threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) {
		return lifecycleconsumer.RunTurnSteer(proc, func(proc any, prompt string, images, files []string, outputSchema json.RawMessage) error {
			typed, _ := proc.(*codexsdk.AgentProcess)
			return a.Submit(typed, prompt, images, files, outputSchema)
		}, submitPrompt, images, files)
	})
}

func (a *Adapter) sendThreadCommand(methodName, threadID, command, args, wrapMsg string) (map[string]any, error) {
	return withProcess(a, methodName, threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) {
		return lifecycleconsumer.RunThreadCommand(proc, methodName, command, args, wrapMsg, func(proc any, command, args string) error {
			typed, _ := proc.(*codexsdk.AgentProcess)
			return a.SendCommand(typed, command, args)
		})
	})
}

// ThreadNameSet sets codex thread name and persists alias.
func (a *Adapter) ThreadNameSet(ctx context.Context, threadID, name string) (map[string]any, error) {
	return lifecycleconsumer.RunThreadNameSet(ctx, threadID, name, a.managerProcess, a.threadExistsInRuntime, a.ThreadExistsInHistory, a.sendCommandFromAny, a.setRuntimeThreadName, a.persistThreadAlias)
}

func (a *Adapter) launchThreadRuntime(ctx context.Context, agentID, name, path, cwd, startInstructions string, dynamicTools []codexsdk.DynamicTool) error {
	manager := a.manager()
	if manager == nil {
		return apperrors.New("Server.threadStart", "thread manager is not initialized")
	}
	return manager.Launch(ctx, agentID, name, path, cwd, startInstructions, dynamicTools)
}

func (a *Adapter) managerProcess(threadID string) any {
	manager := a.manager()
	if manager == nil {
		return nil
	}
	return manager.Get(threadID)
}

func (a *Adapter) lifecycleAgents() []lifecycleconsumer.AgentInfo {
	return toLifecycleAgentInfos(a.runningAgents())
}

func (a *Adapter) registerBindingFromAny(ctx context.Context, threadID string, proc any) {
	if typed, ok := proc.(*codexsdk.AgentProcess); ok {
		a.registerBinding(ctx, threadID, typed)
	}
}

func (a *Adapter) resumeThreadFromAny(proc any, req codexsdk.ResumeThreadRequest) error {
	typed, _ := proc.(*codexsdk.AgentProcess)
	return a.ResumeThread(typed, req)
}

func (a *Adapter) sendCommandFromAny(proc any, command, args string) error {
	typed, _ := proc.(*codexsdk.AgentProcess)
	return a.SendCommand(typed, command, args)
}

// ThreadRead fetches codex history list for the target thread.
func (a *Adapter) ThreadRead(_ context.Context, threadID string) (map[string]any, error) {
	return withProcess(a, "Server.threadRead", threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) {
		return lifecycleconsumer.RunThreadRead(proc, func(proc any) ([]codexsdk.ThreadInfo, error) {
			typed, _ := proc.(*codexsdk.AgentProcess)
			return a.ListThreads(typed)
		})
	})
}

// ThreadResolve resolves thread identity from runtime and history sources.
func (a *Adapter) ThreadResolve(ctx context.Context, threadID string) (map[string]any, error) {
	return lifecycleconsumer.RunThreadResolve(ctx, threadID, a.resolveRunningThreadIdentity, a.firstResolvedCodexThreadID, a.ThreadExistsInHistory)
}

func (a *Adapter) firstResolvedCodexThreadID(ctx context.Context, threadID string) string {
	return lifecycleconsumer.FirstResolvedCodexThreadIDFromCandidates(ctx, threadID, a.ResolveCodexThreadCandidates)
}

func (a *Adapter) resolveRunningThreadIdentity(threadID string) (state string, port int, codexThreadID string, found bool) {
	return lifecycleconsumer.ResolveRunningThreadIdentityFromAgents(threadID, toLifecycleAgentInfos(a.runningAgents()))
}

func (a *Adapter) threadExistsInRuntime(threadID string) bool {
	runtime := a.uiRuntime()
	if runtime == nil {
		return false
	}
	snapshots := runtime.SnapshotLight().Threads
	items := make([]lifecycleconsumer.ThreadSnapshot, 0, len(snapshots))
	for _, item := range snapshots {
		items = append(items, lifecycleconsumer.ThreadSnapshot{ID: item.ID})
	}
	return lifecycleconsumer.ThreadExistsInRuntimeSnapshots(threadID, items)
}

func (a *Adapter) syncRuntimeThreads(items []lifecycleconsumer.AgentInfo) {
	runtime := a.uiRuntime()
	if runtime != nil {
		runtime.ReplaceThreads(toThreadSnapshots(toRunnerAgentInfos(items)))
	}
}

func (a *Adapter) setRuntimeThreadName(threadID, alias string) {
	runtime := a.uiRuntime()
	if runtime != nil {
		runtime.SetThreadName(threadID, alias)
	}
}

func normalizeCodexThreadID(raw string) string {
	return lifecycleconsumer.NormalizeCodexThreadID(raw)
}

func isLikelyCodexThreadID(raw string) bool {
	return lifecycleconsumer.IsLikelyCodexThreadID(raw)
}

func toLifecycleAgentInfos(items []codexsdk.AgentInfo) []lifecycleconsumer.AgentInfo {
	if len(items) == 0 {
		return nil
	}
	out := make([]lifecycleconsumer.AgentInfo, 0, len(items))
	for _, item := range items {
		out = append(out, lifecycleconsumer.AgentInfo{
			ID:       item.ID,
			Name:     item.Name,
			State:    string(item.State),
			Port:     item.Port,
			ThreadID: item.ThreadID,
		})
	}
	return out
}

func toRunnerAgentInfos(items []lifecycleconsumer.AgentInfo) []codexsdk.AgentInfo {
	if len(items) == 0 {
		return nil
	}
	out := make([]codexsdk.AgentInfo, 0, len(items))
	for _, item := range items {
		out = append(out, codexsdk.AgentInfo{
			ID:       item.ID,
			Name:     item.Name,
			State:    codexsdk.AgentState(item.State),
			Port:     item.Port,
			ThreadID: item.ThreadID,
		})
	}
	return out
}

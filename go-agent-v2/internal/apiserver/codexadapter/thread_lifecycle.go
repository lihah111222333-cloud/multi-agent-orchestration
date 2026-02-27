package codexadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	lifecycle "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/lifecycle"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// threadStartResult is the normalized thread/start payload.
type threadStartResult = lifecycle.ThreadStartResult

// ThreadStart launches thread runtime and syncs UI snapshots.
func (a *Adapter) ThreadStart(ctx context.Context, threadID, cwd, model, modelProvider, approvalPolicy string) (threadStartResult, error) {
	return lifecycle.RunThreadStart(ctx, threadID, cwd, model, modelProvider, approvalPolicy, a.allDynamicToolSchemas(), a.launchThreadRuntime, a.managerProcess, a.lifecycleAgents, a.resolveStartInstructionsForLaunch, a.registerBindingFromAny, a.syncRuntimeThreads)
}

// threadResumeResult is the normalized thread/resume payload.
type threadResumeResult = lifecycle.ThreadResumeResult

// ThreadResume resumes a historical codex thread by candidate probing.
func (a *Adapter) ThreadResume(ctx context.Context, threadID, path, cwd, model string) (threadResumeResult, error) {
	id, err := requireThreadID("Server.threadResume", threadID)
	if err != nil {
		return threadResumeResult{}, err
	}
	return withProcess(a, "Server.threadResume", id, func(proc *runner.AgentProcess) (threadResumeResult, error) {
		return lifecycle.RunThreadResume(ctx, id, path, cwd, model, proc, a.ResolveCodexThreadCandidates, lifecycle.NormalizeCodexThreadID, a.resumeThreadFromAny)
	})
}

// threadForkResult is the normalized thread/fork payload.
type threadForkResult = lifecycle.ThreadForkResult

// ThreadFork creates a fork from source thread.
func (a *Adapter) ThreadFork(threadID string) (threadForkResult, error) {
	sourceThreadID := strings.TrimSpace(threadID)
	return withProcess(a, "Server.threadFork", sourceThreadID, func(proc *runner.AgentProcess) (threadForkResult, error) {
		return lifecycle.RunThreadFork(sourceThreadID, proc, func(proc any, req agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error) {
			typed, _ := proc.(*runner.AgentProcess)
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
	return lifecycle.RunThreadRealtimeStart(threadID, prompt)
}

// ThreadRealtimeAppendAudio validates realtime audio append payload.
func (a *Adapter) ThreadRealtimeAppendAudio(threadID string, audio any) (map[string]any, error) {
	return lifecycle.RunThreadRealtimeAppendAudio(threadID, audio)
}

// ThreadRealtimeAppendText validates realtime text append payload.
func (a *Adapter) ThreadRealtimeAppendText(threadID, text string) (map[string]any, error) {
	return lifecycle.RunThreadRealtimeAppendText(threadID, text)
}

// ThreadRealtimeStop validates realtime stop payload.
func (a *Adapter) ThreadRealtimeStop(threadID string) (map[string]any, error) {
	return lifecycle.RunThreadRealtimeStop(threadID)
}

// TurnSteer submits steering prompt to existing thread.
func (a *Adapter) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	return withProcess(a, "Server.turnSteer", threadID, func(proc *runner.AgentProcess) (map[string]any, error) {
		return lifecycle.RunTurnSteer(proc, func(proc any, prompt string, images, files []string, outputSchema json.RawMessage) error {
			typed, _ := proc.(*runner.AgentProcess)
			return a.Submit(typed, prompt, images, files, outputSchema)
		}, submitPrompt, images, files)
	})
}

func (a *Adapter) sendThreadCommand(methodName, threadID, command, args, wrapMsg string) (map[string]any, error) {
	return withProcess(a, methodName, threadID, func(proc *runner.AgentProcess) (map[string]any, error) {
		return lifecycle.RunThreadCommand(proc, methodName, command, args, wrapMsg, func(proc any, command, args string) error {
			typed, _ := proc.(*runner.AgentProcess)
			return a.SendCommand(typed, command, args)
		})
	})
}

// ThreadNameSet sets codex thread name and persists alias.
func (a *Adapter) ThreadNameSet(ctx context.Context, threadID, name string) (map[string]any, error) {
	return lifecycle.RunThreadNameSet(ctx, threadID, name, a.managerProcess, a.threadExistsInRuntime, a.ThreadExistsInHistory, a.sendCommandFromAny, a.setRuntimeThreadName, a.persistThreadAlias)
}

func (a *Adapter) launchThreadRuntime(ctx context.Context, agentID, name, path, cwd, startInstructions string, dynamicTools []agentcore.DynamicTool) error {
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

func (a *Adapter) lifecycleAgents() []lifecycle.AgentInfo {
	return toLifecycleAgentInfos(a.runningAgents())
}

func (a *Adapter) registerBindingFromAny(ctx context.Context, threadID string, proc any) {
	if typed, ok := proc.(*runner.AgentProcess); ok {
		a.registerBinding(ctx, threadID, typed)
	}
}

func (a *Adapter) resumeThreadFromAny(proc any, req agentcore.ResumeThreadRequest) error {
	typed, _ := proc.(*runner.AgentProcess)
	return a.ResumeThread(typed, req)
}

func (a *Adapter) sendCommandFromAny(proc any, command, args string) error {
	typed, _ := proc.(*runner.AgentProcess)
	return a.SendCommand(typed, command, args)
}

// ThreadRead fetches codex history list for the target thread.
func (a *Adapter) ThreadRead(_ context.Context, threadID string) (map[string]any, error) {
	return withProcess(a, "Server.threadRead", threadID, func(proc *runner.AgentProcess) (map[string]any, error) {
		return lifecycle.RunThreadRead(proc, func(proc any) ([]agentcore.ThreadInfo, error) {
			typed, _ := proc.(*runner.AgentProcess)
			return a.ListThreads(typed)
		})
	})
}

// ThreadResolve resolves thread identity from runtime and history sources.
func (a *Adapter) ThreadResolve(ctx context.Context, threadID string) (map[string]any, error) {
	return lifecycle.RunThreadResolve(ctx, threadID, a.resolveRunningThreadIdentity, a.firstResolvedCodexThreadID, a.ThreadExistsInHistory)
}

func (a *Adapter) firstResolvedCodexThreadID(ctx context.Context, threadID string) string {
	return lifecycle.FirstResolvedCodexThreadIDFromCandidates(ctx, threadID, a.ResolveCodexThreadCandidates)
}

func (a *Adapter) resolveRunningThreadIdentity(threadID string) (state string, port int, codexThreadID string, found bool) {
	return lifecycle.ResolveRunningThreadIdentityFromAgents(threadID, toLifecycleAgentInfos(a.runningAgents()))
}

func (a *Adapter) threadExistsInRuntime(threadID string) bool {
	runtime := a.uiRuntime()
	if runtime == nil {
		return false
	}
	snapshots := runtime.SnapshotLight().Threads
	items := make([]lifecycle.ThreadSnapshot, 0, len(snapshots))
	for _, item := range snapshots {
		items = append(items, lifecycle.ThreadSnapshot{ID: item.ID})
	}
	return lifecycle.ThreadExistsInRuntimeSnapshots(threadID, items)
}

func (a *Adapter) syncRuntimeThreads(items []lifecycle.AgentInfo) {
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
	return lifecycle.NormalizeCodexThreadID(raw)
}

func isLikelyCodexThreadID(raw string) bool {
	return lifecycle.IsLikelyCodexThreadID(raw)
}

func toLifecycleAgentInfos(items []runner.AgentInfo) []lifecycle.AgentInfo {
	if len(items) == 0 {
		return nil
	}
	out := make([]lifecycle.AgentInfo, 0, len(items))
	for _, item := range items {
		out = append(out, lifecycle.AgentInfo{
			ID:       item.ID,
			Name:     item.Name,
			State:    string(item.State),
			Port:     item.Port,
			ThreadID: item.ThreadID,
		})
	}
	return out
}

func toRunnerAgentInfos(items []lifecycle.AgentInfo) []runner.AgentInfo {
	if len(items) == 0 {
		return nil
	}
	out := make([]runner.AgentInfo, 0, len(items))
	for _, item := range items {
		out = append(out, runner.AgentInfo{
			ID:       item.ID,
			Name:     item.Name,
			State:    runner.AgentState(item.State),
			Port:     item.Port,
			ThreadID: item.ThreadID,
		})
	}
	return out
}

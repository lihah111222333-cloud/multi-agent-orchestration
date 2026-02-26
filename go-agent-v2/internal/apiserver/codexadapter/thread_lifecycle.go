package codexadapter

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
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
func (a *Adapter) ThreadStart(ctx context.Context, threadID, cwd, model, modelProvider, approvalPolicy string) (threadStartResult, error) {
	return runThreadStart(ctx, threadID, cwd, model, modelProvider, approvalPolicy, a.manager(), a.allDynamicToolSchemas(), a.resolveStartInstructionsForLaunch, a.registerBinding, a.syncRuntimeThreads)
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
		return runThreadResume(ctx, id, path, cwd, model, proc, a.ResolveCodexThreadCandidates, a.ResumeThread)
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
	return withProcess(a, "Server.threadFork", sourceThreadID, func(proc *runner.AgentProcess) (threadForkResult, error) {
		return runThreadFork(sourceThreadID, proc, a.ForkThread, a.nowUnixMilli)
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
	return runThreadRealtimeStart(threadID, prompt)
}

// ThreadRealtimeAppendAudio validates realtime audio append payload.
func (a *Adapter) ThreadRealtimeAppendAudio(threadID string, audio any) (map[string]any, error) {
	return runThreadRealtimeAppendAudio(threadID, audio)
}

// ThreadRealtimeAppendText validates realtime text append payload.
func (a *Adapter) ThreadRealtimeAppendText(threadID, text string) (map[string]any, error) {
	return runThreadRealtimeAppendText(threadID, text)
}

// ThreadRealtimeStop validates realtime stop payload.
func (a *Adapter) ThreadRealtimeStop(threadID string) (map[string]any, error) {
	return runThreadRealtimeStop(threadID)
}

// TurnSteer submits steering prompt to existing thread.
func (a *Adapter) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	return withProcess(a, "Server.turnSteer", threadID, func(proc *runner.AgentProcess) (map[string]any, error) {
		return runTurnSteer(proc, a.Submit, submitPrompt, images, files)
	})
}

func (a *Adapter) sendThreadCommand(methodName, threadID, command, args, wrapMsg string) (map[string]any, error) {
	return withProcess(a, methodName, threadID, func(proc *runner.AgentProcess) (map[string]any, error) {
		return runThreadCommand(proc, methodName, command, args, wrapMsg, a.SendCommand)
	})
}

// ThreadNameSet sets codex thread name and persists alias.
func (a *Adapter) ThreadNameSet(ctx context.Context, threadID, name string) (map[string]any, error) {
	return runThreadNameSet(
		ctx,
		threadID,
		name,
		a.manager(),
		a.threadExistsInRuntime,
		a.ThreadExistsInHistory,
		a.SendCommand,
		a.setRuntimeThreadName,
		a.persistThreadAlias,
	)
}

// ThreadRead fetches codex history list for the target thread.
func (a *Adapter) ThreadRead(_ context.Context, threadID string) (map[string]any, error) {
	return withProcess(a, "Server.threadRead", threadID, func(proc *runner.AgentProcess) (map[string]any, error) {
		return runThreadRead(proc, a.ListThreads)
	})
}

// ThreadResolve resolves thread identity from runtime and history sources.
func (a *Adapter) ThreadResolve(ctx context.Context, threadID string) (map[string]any, error) {
	return runThreadResolve(ctx, threadID, a.resolveRunningThreadIdentity, a.firstResolvedCodexThreadID, a.ThreadExistsInHistory)
}

func (a *Adapter) firstResolvedCodexThreadID(ctx context.Context, threadID string) string {
	return firstResolvedCodexThreadIDFromCandidates(ctx, threadID, a.ResolveCodexThreadCandidates)
}

func (a *Adapter) resolveRunningThreadIdentity(threadID string) (state string, port int, codexThreadID string, found bool) {
	return resolveRunningThreadIdentityFromAgents(threadID, a.runningAgents())
}

func (a *Adapter) threadExistsInRuntime(threadID string) bool {
	runtime := a.uiRuntime()
	if runtime == nil {
		return false
	}
	return threadExistsInRuntimeSnapshots(threadID, runtime.SnapshotLight().Threads)
}

func (a *Adapter) syncRuntimeThreads(items []runner.AgentInfo) {
	runtime := a.uiRuntime()
	if runtime != nil {
		runtime.ReplaceThreads(toThreadSnapshots(items))
	}
}

func (a *Adapter) setRuntimeThreadName(threadID, alias string) {
	runtime := a.uiRuntime()
	if runtime != nil {
		runtime.SetThreadName(threadID, alias)
	}
}

// codexThreadIDPattern matches a lowercase UUID (codex thread ID format).
var codexThreadIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// normalizeCodexThreadID trims, lowercases, strips "urn:uuid:" prefix,
// and validates against UUID pattern. Returns "" if invalid.
func normalizeCodexThreadID(raw string) string {
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

// isLikelyCodexThreadID reports whether raw looks like a valid codex thread ID.
func isLikelyCodexThreadID(raw string) bool {
	return normalizeCodexThreadID(raw) != ""
}

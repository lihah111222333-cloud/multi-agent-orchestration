package codexadapter

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

// ensureThreadReadyForTurn 负责拉起/恢复线程进程，并处理历史会话丢失降级。
func (a *Adapter) ensureThreadReadyForTurn(ctx context.Context, threadID, cwd string) (*runner.AgentProcess, error) {
	return ensureThreadReadyForTurnLogic(a, ctx, threadID, cwd)
}

func (a *Adapter) ensureReadyRunningProcess(
	ctx context.Context,
	manager *runner.AgentManager,
	agentID string,
	launchCwd string,
) (*runner.AgentProcess, bool) {
	return ensureReadyRunningProcessLogic(a, ctx, manager, agentID, launchCwd)
}

func (a *Adapter) ensureReadyLaunchProcess(
	ctx context.Context,
	manager *runner.AgentManager,
	agentID string,
	launchCwd string,
	startInstructions string,
	dynamicTools []agentcore.DynamicTool,
) (*runner.AgentProcess, error) {
	return ensureReadyLaunchProcessLogic(a, ctx, manager, agentID, launchCwd, startInstructions, dynamicTools)
}

func (a *Adapter) ensureReadyNoResumeCandidates(
	agentID string,
	proc *runner.AgentProcess,
) *runner.AgentProcess {
	return ensureReadyNoResumeCandidatesLogic(a, agentID, proc)
}

func (a *Adapter) ensureReadyResumeFallback(
	ctx context.Context,
	manager *runner.AgentManager,
	agentID string,
	launchCwd string,
	proc *runner.AgentProcess,
	lastResumeErr error,
	startInstructions string,
	dynamicTools []agentcore.DynamicTool,
	candidateCount int,
) (*runner.AgentProcess, error) {
	return ensureReadyResumeFallbackLogic(a, ctx, manager, agentID, launchCwd, proc, lastResumeErr, startInstructions, dynamicTools, candidateCount)
}

func (a *Adapter) ensureReadyNoHistoricalRollout(
	ctx context.Context,
	agentID string,
	launchCwd string,
	proc *runner.AgentProcess,
	candidateCount int,
) *runner.AgentProcess {
	return ensureReadyNoHistoricalRolloutLogic(a, ctx, agentID, launchCwd, proc, candidateCount)
}

func (a *Adapter) registerBinding(ctx context.Context, agentID string, proc *runner.AgentProcess) {
	registerBindingLogic(a, ctx, agentID, proc)
}

func (a *Adapter) notifySessionLost(agentID string, lastErr error) {
	notifySessionLostLogic(a, agentID, lastErr)
}

// BuildSessionLostNotification builds "session lost" fallback notification payload.
func BuildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
	detail := ""
	if lastErr != nil {
		detail = lastErr.Error()
	}
	return "ui/state/changed", map[string]any{
		"source":   "session_lost_warning",
		"agent_id": agentID,
		"warning":  "会话历史已丢失 (codex session 文件不存在)，已自动回退到全新会话",
		"detail":   detail,
	}
}

func (a *Adapter) collectResumeCandidates(ctx context.Context, agentID string) []string {
	return collectResumeCandidatesLogic(a, ctx, agentID)
}

func (a *Adapter) tryResumeHistoricalCandidates(
	ctx context.Context,
	manager *runner.AgentManager,
	proc *runner.AgentProcess,
	agentID string,
	launchCwd string,
	resumeCandidates []string,
) (resumed bool, lastResumeErr error, fatalErr error) {
	return tryResumeHistoricalCandidatesLogic(a, ctx, manager, proc, agentID, launchCwd, resumeCandidates)
}

// turnStartRequest carries protocol params for turn/start.
type turnStartRequest = contracts.TurnStartRequest

// turnSteerRequest carries protocol params for turn/steer.
type turnSteerRequest = contracts.TurnSteerRequest

type turnStartEntryResult struct {
	TurnID string
}

// TurnStart handles turn/start with constructor-time dependencies.
func (a *Adapter) TurnStart(ctx context.Context, req turnStartRequest) (turnStartEntryResult, error) {
	return turnStartLogic(a, ctx, req)
}

// TurnSteerFromInput handles turn/steer with constructor-time dependencies.
func (a *Adapter) TurnSteerFromInput(req turnSteerRequest) (map[string]any, error) {
	return turnSteerFromInputLogic(a, req)
}

// StartTurnSubmissionAndTrack handles submit and turn tracker bootstrap.
func (a *Adapter) startTurnSubmissionAndTrack(
	ctx context.Context,
	threadID string,
	cwd string,
	submitPrompt string,
	images []string,
	files []string,
	outputSchema json.RawMessage,
) (string, error) {
	return startTurnSubmissionAndTrackLogic(a, ctx, threadID, cwd, submitPrompt, images, files, outputSchema)
}

func (a *Adapter) resolveProcess(caller, threadID string) (*runner.AgentProcess, error) {
	return resolveProcessLogic(a, caller, threadID)
}

func withProcess[T any](
	a *Adapter,
	caller string,
	threadID string,
	fn func(*runner.AgentProcess) (T, error),
) (T, error) {
	var zero T
	proc, err := a.resolveProcess(caller, threadID)
	if err != nil {
		return zero, err
	}
	return fn(proc)
}

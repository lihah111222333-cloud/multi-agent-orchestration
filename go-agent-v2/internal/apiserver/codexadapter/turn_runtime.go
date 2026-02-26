package codexadapter

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	serviceruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
)

func (a *Adapter) ensureThreadReadyForTurn(ctx context.Context, threadID, cwd string) (*runner.AgentProcess, error) {
	proc, err := serviceruntime.EnsureThreadReadyForTurn(newServiceRuntimeBridge(a), ctx, threadID, cwd)
	if err != nil {
		return nil, err
	}
	return unwrapServiceRuntimeProcess(proc), nil
}

func (a *Adapter) ensureReadyRunningProcess(
	ctx context.Context,
	manager *runner.AgentManager,
	agentID string,
	launchCwd string,
) (*runner.AgentProcess, bool) {
	proc, ok := serviceruntime.EnsureReadyRunningProcess(
		newServiceRuntimeBridge(a),
		ctx,
		&serviceRuntimeManager{manager: manager},
		agentID,
		launchCwd,
	)
	return unwrapServiceRuntimeProcess(proc), ok
}

func (a *Adapter) ensureReadyLaunchProcess(
	ctx context.Context,
	manager *runner.AgentManager,
	agentID string,
	launchCwd string,
	startInstructions string,
	dynamicTools []agentcore.DynamicTool,
) (*runner.AgentProcess, error) {
	proc, err := serviceruntime.EnsureReadyLaunchProcess(
		newServiceRuntimeBridge(a),
		ctx,
		&serviceRuntimeManager{manager: manager},
		agentID,
		launchCwd,
		startInstructions,
		dynamicTools,
	)
	if err != nil {
		return nil, err
	}
	return unwrapServiceRuntimeProcess(proc), nil
}

func (a *Adapter) ensureReadyNoResumeCandidates(agentID string, proc *runner.AgentProcess) *runner.AgentProcess {
	return unwrapServiceRuntimeProcess(serviceruntime.EnsureReadyNoResumeCandidates(newServiceRuntimeBridge(a), agentID, wrapServiceRuntimeProcess(proc)))
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
	resolved, err := serviceruntime.EnsureReadyResumeFallback(
		newServiceRuntimeBridge(a),
		ctx,
		&serviceRuntimeManager{manager: manager},
		agentID,
		launchCwd,
		wrapServiceRuntimeProcess(proc),
		lastResumeErr,
		startInstructions,
		dynamicTools,
		candidateCount,
	)
	if err != nil {
		return nil, err
	}
	return unwrapServiceRuntimeProcess(resolved), nil
}

func (a *Adapter) ensureReadyNoHistoricalRollout(
	ctx context.Context,
	agentID string,
	launchCwd string,
	proc *runner.AgentProcess,
	candidateCount int,
) *runner.AgentProcess {
	resolved := serviceruntime.EnsureReadyNoHistoricalRollout(
		newServiceRuntimeBridge(a),
		ctx,
		agentID,
		launchCwd,
		wrapServiceRuntimeProcess(proc),
		candidateCount,
	)
	return unwrapServiceRuntimeProcess(resolved)
}

func (a *Adapter) registerBinding(ctx context.Context, agentID string, proc *runner.AgentProcess) {
	serviceruntime.RegisterBinding(newServiceRuntimeBridge(a), ctx, agentID, wrapServiceRuntimeProcess(proc))
}

func (a *Adapter) notifySessionLost(agentID string, lastErr error) {
	serviceruntime.NotifySessionLost(newServiceRuntimeBridge(a), agentID, lastErr)
}

func BuildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
	return serviceruntime.BuildSessionLostNotification(agentID, lastErr)
}

func (a *Adapter) collectResumeCandidates(ctx context.Context, agentID string) []string {
	return serviceruntime.CollectResumeCandidates(newServiceRuntimeBridge(a), ctx, agentID)
}

func (a *Adapter) tryResumeHistoricalCandidates(
	ctx context.Context,
	manager *runner.AgentManager,
	proc *runner.AgentProcess,
	agentID string,
	launchCwd string,
	resumeCandidates []string,
) (resumed bool, lastResumeErr error, fatalErr error) {
	return serviceruntime.TryResumeHistoricalCandidates(
		newServiceRuntimeBridge(a),
		ctx,
		&serviceRuntimeManager{manager: manager},
		wrapServiceRuntimeProcess(proc),
		agentID,
		launchCwd,
		resumeCandidates,
	)
}

type turnStartRequest = contracts.TurnStartRequest

type turnSteerRequest = contracts.TurnSteerRequest

type turnStartEntryResult struct {
	TurnID string
}

func (a *Adapter) TurnStart(ctx context.Context, req turnStartRequest) (turnStartEntryResult, error) {
	result, err := serviceruntime.TurnStart(newServiceRuntimeBridge(a), ctx, toRuntimeTurnStartRequest(req))
	if err != nil {
		return turnStartEntryResult{}, err
	}
	return turnStartEntryResult{TurnID: result.TurnID}, nil
}

func (a *Adapter) TurnSteerFromInput(req turnSteerRequest) (map[string]any, error) {
	return serviceruntime.TurnSteerFromInput(newServiceRuntimeBridge(a), toRuntimeTurnSteerRequest(req))
}

func (a *Adapter) TurnSteerFromInputAligned(req turnSteerRequest) (map[string]any, error) {
	return serviceruntime.TurnSteerFromInputAlignedByAdapter(
		newServiceRuntimeBridge(a),
		toRuntimeTurnSteerRequest(req),
		func(runtimeReq serviceruntime.TurnSteerRequest) (map[string]any, error) {
			return a.TurnSteerFromInput(fromRuntimeTurnSteerRequest(runtimeReq))
		},
	)
}

func (a *Adapter) startTurnSubmissionAndTrack(
	ctx context.Context,
	threadID string,
	cwd string,
	submitPrompt string,
	images []string,
	files []string,
	outputSchema json.RawMessage,
) (string, error) {
	return serviceruntime.StartTurnSubmissionAndTrack(
		newServiceRuntimeBridge(a),
		ctx,
		threadID,
		cwd,
		submitPrompt,
		images,
		files,
		outputSchema,
	)
}

func (a *Adapter) resolveProcess(caller, threadID string) (*runner.AgentProcess, error) {
	proc, err := serviceruntime.ResolveProcess(newServiceRuntimeBridge(a), caller, threadID)
	if err != nil {
		return nil, err
	}
	return unwrapServiceRuntimeProcess(proc), nil
}

func withProcess[T any](
	a *Adapter,
	caller string,
	threadID string,
	fn func(*runner.AgentProcess) (T, error),
) (T, error) {
	return serviceruntime.WithProcess(newServiceRuntimeBridge(a), caller, threadID, func(proc serviceruntime.Process) (T, error) {
		return fn(unwrapServiceRuntimeProcess(proc))
	})
}

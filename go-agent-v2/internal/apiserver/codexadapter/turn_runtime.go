package codexadapter

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	consumerruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/runtime"
)

func (a *Adapter) registerBinding(ctx context.Context, agentID string, proc *codexsdk.AgentProcess) {
	consumerruntime.RegisterBinding(ctx, a.runtimeConsumerDeps(), agentID, proc)
}

func BuildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
	return consumerruntime.BuildSessionLostNotification(agentID, lastErr)
}

type turnStartRequest = contracts.TurnStartRequest

type turnSteerRequest = contracts.TurnSteerRequest

type turnStartEntryResult = consumerruntime.TurnStartEntryResult

func (a *Adapter) TurnStart(ctx context.Context, req turnStartRequest) (turnStartEntryResult, error) {
	return consumerruntime.TurnStart(ctx, a.runtimeConsumerDeps(), req)
}

func (a *Adapter) TurnSteerFromInput(req turnSteerRequest) (map[string]any, error) {
	return consumerruntime.TurnSteerFromInput(a.runtimeConsumerDeps(), req)
}

func (a *Adapter) TurnSteerFromInputAligned(req turnSteerRequest) (map[string]any, error) {
	return consumerruntime.TurnSteerFromInputAligned(a.runtimeConsumerDeps(), req)
}

func (a *Adapter) resolveProcess(caller, threadID string) (*codexsdk.AgentProcess, error) {
	return consumerruntime.ResolveProcess(a.runtimeConsumerDeps(), caller, threadID)
}

func withProcess[T any](
	a *Adapter,
	caller string,
	threadID string,
	fn func(*codexsdk.AgentProcess) (T, error),
) (T, error) {
	return consumerruntime.WithProcess(a.runtimeConsumerDeps(), caller, threadID, fn)
}

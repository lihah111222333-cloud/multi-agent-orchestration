package codexadapter

import (
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	interruptconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/interrupt"
)

// resolveClientActiveTurnID extracts active turn ID if client supports it.
func resolveClientActiveTurnID(client codexsdk.Client) string {
	if client == nil {
		return ""
	}
	reader, ok := client.(interface{ GetActiveTurnID() string })
	if !ok {
		return ""
	}
	return strings.TrimSpace(reader.GetActiveTurnID())
}

// readThreadRuntimeState reads normalized runtime state using adapter-owned tracker/runtime state.
func (a *Adapter) readThreadRuntimeState(threadID string) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	return interruptconsumer.ReadThreadRuntimeStateByHooks(id, a.readRuntimeStatus, a.hasActiveTrackedTurn)
}

func (a *Adapter) readRuntimeStatus(threadID string) string {
	uiRuntime := a.uiRuntime()
	if uiRuntime == nil {
		return ""
	}
	snapshot := uiRuntime.Snapshot()
	return snapshot.Statuses[threadID]
}

// waitInterruptOutcome waits for terminal state using adapter-owned tracker/runtime state.
func (a *Adapter) waitInterruptOutcome(threadID string, timeout time.Duration, activeHint bool) (bool, string, int64, bool) {
	return interruptconsumer.WaitInterruptOutcome(threadID, timeout, activeHint, a.waitTrackedTurnTerminal, a.readThreadRuntimeState)
}

func (a *Adapter) sendInterruptCommand(proc *codexsdk.AgentProcess) (bool, error) {
	return interruptconsumer.SendInterruptCommand(proc, func(proc any, command, args string) error {
		typed, _ := proc.(*codexsdk.AgentProcess)
		return a.SendCommand(typed, command, args)
	})
}

func (a *Adapter) notifyTurnCompleted(threadID, status, reason string) {
	interruptconsumer.NotifyTurnCompleted(threadID, status, reason, a.completeTrackedTurnByID, a.notify)
}

func (a *Adapter) withProcessAny(
	methodName string,
	threadID string,
	fn func(any) (any, error),
) (any, error) {
	return withProcess(a, methodName, threadID, func(proc *codexsdk.AgentProcess) (any, error) {
		return fn(proc)
	})
}

// TurnInterrupt executes /interrupt using constructor-injected dependencies.
func (a *Adapter) TurnInterrupt(threadID string) (any, error) {
	return interruptconsumer.TurnInterrupt(threadID, a.readThreadRuntimeState, a.hasActiveTrackedTurn, a.cancelCodeRuns, a.sendInterruptFromAny, a.withProcessAny, a.markTrackedTurnInterruptRequested, a.waitInterruptOutcome, a.notifyTurnCompleted)
}

func (a *Adapter) sendInterruptFromAny(proc any) (bool, error) {
	typed, _ := proc.(*codexsdk.AgentProcess)
	return a.sendInterruptCommand(typed)
}

// TurnForceComplete forcibly finalizes current turn state using constructor-injected dependencies.
func (a *Adapter) TurnForceComplete(threadID string) (any, error) {
	return interruptconsumer.TurnForceComplete(
		threadID,
		a.cancelCodeRuns,
		a.sendInterruptFromAny,
		a.notifyTurnCompleted,
		a.withProcessAny,
	)
}

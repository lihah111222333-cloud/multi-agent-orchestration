package codexadapter

import (
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

// resolveClientActiveTurnID extracts active turn ID if client supports it.
func resolveClientActiveTurnID(client agentcore.Client) string {
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
	return readThreadRuntimeStateByHooks(id, a.readRuntimeStatus, a.hasActiveTrackedTurn)
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
	return waitInterruptOutcome(threadID, timeout, activeHint, a.waitTrackedTurnTerminal, a.readThreadRuntimeState)
}

func (a *Adapter) sendInterruptCommand(proc *runner.AgentProcess) (bool, error) {
	return sendInterruptCommand(proc, a.SendCommand)
}

func (a *Adapter) notifyTurnCompleted(threadID, status, reason string) {
	notifyTurnCompleted(threadID, status, reason, a.completeTrackedTurnByID, a.notify)
}

func (a *Adapter) withProcessAny(
	methodName string,
	threadID string,
	fn func(*runner.AgentProcess) (any, error),
) (any, error) {
	return withProcess(a, methodName, threadID, fn)
}

// TurnInterrupt executes /interrupt using constructor-injected dependencies.
func (a *Adapter) TurnInterrupt(threadID string) (any, error) {
	return turnInterrupt(
		threadID,
		a.readThreadRuntimeState,
		a.hasActiveTrackedTurn,
		a.cancelCodeRuns,
		a.sendInterruptCommand,
		a.withProcessAny,
		a.markTrackedTurnInterruptRequested,
		a.waitInterruptOutcome,
		a.notifyTurnCompleted,
	)
}

// TurnForceComplete forcibly finalizes current turn state using constructor-injected dependencies.
func (a *Adapter) TurnForceComplete(threadID string) (any, error) {
	return turnForceComplete(
		threadID,
		a.cancelCodeRuns,
		a.sendInterruptCommand,
		a.notifyTurnCompleted,
		a.withProcessAny,
	)
}

package codexadapter

import (
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
)

// ResolveClientActiveTurnID extracts active turn ID if client supports it.
func ResolveClientActiveTurnID(client agentcore.Client) string {
	if client == nil {
		return ""
	}
	reader, ok := client.(interface{ GetActiveTurnID() string })
	if !ok {
		return ""
	}
	return strings.TrimSpace(reader.GetActiveTurnID())
}

// NormalizeInterruptState normalizes runtime status names used by interrupt flow.
func NormalizeInterruptState(raw string) string {
	state := strings.ToLower(strings.TrimSpace(raw))
	if state == "" {
		return "idle"
	}
	switch state {
	case "completed", "complete", "done", "success", "succeeded", "ready", "stopped", "ended", "closed":
		return "idle"
	case "failed", "fail":
		return "error"
	default:
		return state
	}
}

// IsInterruptNoActiveTurnError reports whether interrupt failure means no active turn.
func IsInterruptNoActiveTurnError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no active turn") ||
		strings.Contains(message, "nothing to interrupt") ||
		strings.Contains(message, "not interruptible")
}

// IsInterruptActiveState reports whether current state is still active.
func IsInterruptActiveState(state string) bool {
	s := NormalizeInterruptState(state)
	switch s {
	case "inprogress", "in_progress", "running", "streaming", "thinking", "starting", "responding", "editing", "waiting", "syncing":
		return true
	default:
		return false
	}
}

// InterruptSettleMode classifies interrupt settle outcome.
func InterruptSettleMode(confirmed bool, afterState string) string {
	if confirmed {
		return "interrupt_confirmed"
	}
	switch NormalizeInterruptState(afterState) {
	case "error":
		return "interrupt_terminal_failed"
	case "idle":
		return "interrupt_terminal_completed"
	default:
		return "interrupt_timeout"
	}
}

// ReadThreadRuntimeState returns normalized thread state via injected runtime/status hooks.
func ReadThreadRuntimeState(threadID string, readRuntimeStatus func(string) string, hasActiveTrackedTurn func(string) bool) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	if readRuntimeStatus == nil {
		if hasActiveTrackedTurn != nil && hasActiveTrackedTurn(id) {
			return "running"
		}
		return ""
	}
	state := NormalizeInterruptState(readRuntimeStatus(id))
	if state == "idle" && hasActiveTrackedTurn != nil && hasActiveTrackedTurn(id) {
		return "running"
	}
	return state
}

// ReadThreadRuntimeState reads normalized runtime state using adapter-owned tracker/runtime state.
func (a *Adapter) ReadThreadRuntimeState(threadID string) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	return ReadThreadRuntimeState(id, a.readRuntimeStatus, a.HasActiveTrackedTurn)
}

func (a *Adapter) readRuntimeStatus(threadID string) string {
	if a == nil || a.ctx == nil || a.ctx.UIRuntime == nil {
		return ""
	}
	snapshot := a.ctx.UIRuntime.Snapshot()
	return snapshot.Statuses[threadID]
}

// WaitInterruptOutcome waits until interrupt settles based on tracker/runtime state.
func WaitInterruptOutcome(
	threadID string,
	timeout time.Duration,
	activeHint bool,
	waitTrackedTurnTerminal func(string, time.Duration) (string, bool),
	readThreadRuntimeState func(string) string,
) (bool, string, int64, bool) {
	start := time.Now()
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false, "idle", 0, false
	}
	observedActive := activeHint
	if waitTrackedTurnTerminal != nil {
		if terminalStatus, ok := waitTrackedTurnTerminal(id, timeout); ok {
			afterState := NormalizeInterruptState(terminalStatus)
			confirmed := strings.EqualFold(terminalStatus, "interrupted")
			return confirmed, afterState, time.Since(start).Milliseconds(), true
		}
	}
	if readThreadRuntimeState == nil {
		return false, "", time.Since(start).Milliseconds(), observedActive
	}
	deadline := start.Add(timeout)
	lastState := readThreadRuntimeState(id)
	if IsInterruptActiveState(lastState) {
		observedActive = true
	}
	for {
		if !IsInterruptActiveState(lastState) {
			if !observedActive {
				return false, lastState, time.Since(start).Milliseconds(), false
			}
			return true, lastState, time.Since(start).Milliseconds(), true
		}
		observedActive = true
		if time.Now().After(deadline) {
			return false, lastState, time.Since(start).Milliseconds(), true
		}
		time.Sleep(120 * time.Millisecond)
		lastState = readThreadRuntimeState(id)
	}
}

// WaitInterruptOutcome waits for terminal state using adapter-owned tracker/runtime state.
func (a *Adapter) WaitInterruptOutcome(threadID string, timeout time.Duration, activeHint bool) (bool, string, int64, bool) {
	return WaitInterruptOutcome(threadID, timeout, activeHint, a.WaitTrackedTurnTerminal, a.ReadThreadRuntimeState)
}

func (a *Adapter) sendInterruptCommand(proc *runner.AgentProcess) (bool, error) {
	if err := a.SendCommand(proc, "/interrupt", ""); err != nil {
		if IsInterruptNoActiveTurnError(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func (a *Adapter) notifyTurnCompleted(threadID, status, reason string) {
	if completion, ok := a.CompleteTrackedTurnByID(threadID, "", status, reason); ok {
		if a != nil && a.ctx != nil {
			a.ctx.Notify("turn/completed", completion)
		}
		return
	}
	if a != nil && a.ctx != nil {
		a.ctx.Notify("turn/completed", map[string]any{
			"threadId": threadID,
			"status":   status,
			"reason":   reason,
		})
	}
}

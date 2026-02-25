package codexadapter

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
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

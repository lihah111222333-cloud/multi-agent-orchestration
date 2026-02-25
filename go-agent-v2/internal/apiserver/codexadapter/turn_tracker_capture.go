package codexadapter

import (
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func noopRememberTrackedTurnSummary(string, string, string) {}

func emptyTrackedTurnSummary(string, string) string {
	return ""
}

// CaptureAndInjectTurnSummary captures summary at terminal events and injects it into turn/completed.
func CaptureAndInjectTurnSummary(
	threadID string,
	eventType string,
	method string,
	payload map[string]any,
	peekTrackedTurnMeta func(threadID string) (turnID string, startedAt time.Time, interruptRequested bool, ok bool),
	trackedTurnTerminalFromEvent func(eventType, method string, payload map[string]any) (turnID, status, reason string, terminal bool, synthetic bool),
	extractTrackedTurnID func(payload map[string]any) string,
	trackedTurnSummaryFromPayload func(payload map[string]any) string,
	rememberTrackedTurnSummary func(threadID, turnID, summary string),
	lookupTrackedTurnSummary func(threadID, turnID string) string,
	injectTrackedTurnSummary func(payload map[string]any, summary string),
) {
	if payload == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}

	terminalFromEvent := trackedTurnTerminalFromEvent
	if terminalFromEvent == nil {
		terminalFromEvent = TrackedTurnTerminalFromEvent
	}
	extractTurnID := extractTrackedTurnID
	if extractTurnID == nil {
		extractTurnID = ExtractTrackedTurnID
	}
	summaryFromPayload := trackedTurnSummaryFromPayload
	if summaryFromPayload == nil {
		summaryFromPayload = TrackedTurnSummaryFromPayload
	}
	rememberSummary := rememberTrackedTurnSummary
	if rememberSummary == nil {
		rememberSummary = noopRememberTrackedTurnSummary
	}
	lookupSummary := lookupTrackedTurnSummary
	if lookupSummary == nil {
		lookupSummary = emptyTrackedTurnSummary
	}
	injectSummary := injectTrackedTurnSummary
	if injectSummary == nil {
		injectSummary = InjectTrackedTurnSummary
	}

	turnID := extractTurnID(payload)
	resolvedTurnID := turnID
	if resolvedTurnID == "" && peekTrackedTurnMeta != nil {
		if activeTurnID, _, _, ok := peekTrackedTurnMeta(id); ok {
			resolvedTurnID = activeTurnID
		}
	}

	summary := summaryFromPayload(payload)
	if summary != "" {
		_, _, _, terminal, _ := terminalFromEvent(eventType, method, payload)
		methodKey := strings.ToLower(strings.TrimSpace(method))
		eventKey := strings.ToLower(strings.TrimSpace(eventType))
		if terminal || methodKey == "codex/event/task_complete" || eventKey == "codex/event/task_complete" {
			rememberSummary(id, resolvedTurnID, summary)
		}
	}

	if !strings.EqualFold(strings.TrimSpace(method), "turn/completed") {
		return
	}
	if summary == "" {
		summary = lookupSummary(id, resolvedTurnID)
	}
	if summary == "" {
		return
	}
	injectSummary(payload, summary)
	rememberSummary(id, resolvedTurnID, summary)
}

// FinalizeTrackedTurnEvent updates heartbeat and finalizes turn state from an incoming event.
func (a *Adapter) FinalizeTrackedTurnEvent(
	threadID string,
	eventType string,
	method string,
	payload map[string]any,
) {
	if a == nil {
		return
	}

	a.TouchTrackedTurnLastEvent(threadID)

	if IsTerminalEventType(eventType, method) {
		hasActive := false
		hasActive = a.HasActiveTrackedTurn(threadID)
		logger.Warn("DIAG: AgentEventHandler received terminal event",
			logger.FieldThreadID, threadID,
			logger.FieldEventType, eventType,
			logger.FieldMethod, method,
			"has_active_tracked_turn", hasActive,
		)
	}
	a.MaybeFinalizeTrackedTurn(threadID, eventType, method, payload)
}

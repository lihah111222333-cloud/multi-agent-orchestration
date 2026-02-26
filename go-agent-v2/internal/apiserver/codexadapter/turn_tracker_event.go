package codexadapter

import (
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
	"strings"
	"time"
)

// ExtractTrackedString exposes tracked string extraction through adapter APIs.
func (a *Adapter) ExtractTrackedString(payload map[string]any, keys ...string) string {
	return extractTrackedString(payload, keys...)
}

// TrackedTurnTerminalFromEvent exposes terminal event classification through adapter APIs.
func (a *Adapter) TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	return trackedTurnTerminalFromEvent(eventType, method, payload)
}

// TrackedTurnSummaryFromPayload exposes summary extraction through adapter APIs.
func (a *Adapter) TrackedTurnSummaryFromPayload(payload map[string]any) string {
	return trackedTurnSummaryFromPayload(payload)
}

// CaptureAndInjectTurnSummary captures terminal summaries and injects them into completion payloads.
func (a *Adapter) CaptureAndInjectTurnSummary(threadID, eventType, method string, payload map[string]any) {
	if a == nil {
		return
	}
	resolveTurnID := func(id string, source map[string]any) string {
		turnID := extractTrackedTurnID(source)
		if turnID != "" {
			return turnID
		}
		activeTurnID, _, _, ok := a.peekTrackedTurnMeta(id)
		if ok {
			return activeTurnID
		}
		return ""
	}
	captureAndInjectTurnSummary(threadID, eventType, method, payload, resolveTurnID, a.rememberTrackedTurnSummary, a.lookupTrackedTurnSummary)
}

// maybeFinalizeTrackedTurn applies turn-terminal events to tracked-turn state.
func (a *Adapter) maybeFinalizeTrackedTurn(threadID, eventType, method string, payload map[string]any) {
	if a == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}

	turnID, startedAt, interruptRequested, ok := a.peekTrackedTurnMeta(id)
	if !ok {
		return
	}

	eventTurnID, status, reason, terminal, synthetic := trackedTurnTerminalFromEvent(eventType, method, payload)
	if !terminal {
		if shouldLogTrackedTurnStallHint(eventType, method, startedAt) && a.markTrackedTurnStallHint(id, turnID) {
			logger.Warn("turn tracker: active turn not terminal yet at tail event", maybeFinalizeDiagFields(
				id,
				turnID,
				eventTurnID,
				eventType,
				method,
				status,
				reason,
				payload,
				"turn_age_ms", time.Since(startedAt).Milliseconds(),
				"interrupt_requested", interruptRequested,
			)...)
		}
		return
	}

	if strings.TrimSpace(eventTurnID) == "" {
		logger.Warn("turn tracker: terminal event missing turn_id", maybeFinalizeDiagFields(
			id,
			turnID,
			eventTurnID,
			eventType,
			method,
			status,
			reason,
			payload,
		)...)
	}

	completion, completed := a.completeTrackedTurnByID(id, eventTurnID, status, reason)
	if !completed {
		logger.Warn("turn tracker: terminal event failed to close tracked turn", maybeFinalizeDiagFields(
			id,
			turnID,
			eventTurnID,
			eventType,
			method,
			status,
			reason,
			payload,
		)...)
		return
	}

	logger.Info("turn tracker: finalized by event", append(threadLogFields(id),
		"tracked_turn_id", turnID,
		"event_turn_id", eventTurnID,
		logger.FieldStatus, strings.TrimSpace(status),
		"reason", strings.TrimSpace(reason),
		"synthetic", synthetic,
		logger.FieldEventType, strings.TrimSpace(eventType),
		logger.FieldMethod, strings.TrimSpace(method),
	)...)

	summary := trackedTurnSummaryFromPayload(payload)
	if summary == "" {
		summary = a.lookupTrackedTurnSummary(id, util.FirstNonEmpty(eventTurnID, extractTrackedTurnID(payload), turnID))
	}
	if summary != "" {
		injectTrackedTurnSummary(completion, summary)
		a.rememberTrackedTurnSummary(id, util.FirstNonEmpty(extractTrackedTurnID(completion), eventTurnID, extractTrackedTurnID(payload)), summary)
	}

	if synthetic {
		if notify := a.trackerNotify(); notify != nil {
			notify("turn/completed", completion)
		}
		return
	}
	mergeTrackedTurnCompletionPayload(payload, completion)
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
	a.touchTrackedTurnLastEvent(threadID)
	a.maybeFinalizeTrackedTurn(threadID, eventType, method, payload)
}

// rememberTrackedTurnSummary records summary into adapter-owned cache state.
func (a *Adapter) rememberTrackedTurnSummary(threadID, turnID, summary string) {
	if a == nil {
		return
	}
	state := a.trackerHelperState()
	rememberTrackedTurnSummary(state, state.Mu, threadID, turnID, summary)
}

// lookupTrackedTurnSummary reads tracked-turn summary from adapter-owned cache state.
func (a *Adapter) lookupTrackedTurnSummary(threadID, turnID string) string {
	if a == nil {
		return ""
	}
	state := a.trackerHelperState()
	return lookupTrackedTurnSummary(state, state.Mu, threadID, turnID)
}

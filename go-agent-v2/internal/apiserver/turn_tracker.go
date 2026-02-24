package apiserver

import (
	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
)

const (
	defaultTurnWatchdogTimeout   = codexadapter.DefaultTurnWatchdogTimeout
	defaultTrackedTurnSummaryTTL = codexadapter.DefaultTrackedTurnSummaryTTL
	defaultStallThreshold        = codexadapter.DefaultStallThreshold
	defaultStallHeartbeat        = codexadapter.DefaultStallHeartbeat
)

func (s *Server) turnTrackerState() codexadapter.TurnTrackerState {
	return codexadapter.TurnTrackerState{
		ActiveTurns:         &s.activeTurns,
		TurnWatchdogTimeout: &s.turnWatchdogTimeout,
		TurnSummaryCache:    &s.turnSummaryCache,
		TurnSummaryTTL:      &s.turnSummaryTTL,
		StallThreshold:      &s.stallThreshold,
		StallHeartbeat:      &s.stallHeartbeat,
	}
}

func (s *Server) ensureTurnTrackerStateLocked() {
	codexadapter.EnsureTurnTrackerStateLocked(s.turnTrackerState())
}

func trackedTurnSummaryFromPayload(payload map[string]any) string {
	return codexadapter.TrackedTurnSummaryFromPayload(payload)
}

func (s *Server) rememberTrackedTurnSummary(threadID, turnID, summary string) {
	codexadapter.RememberTrackedTurnSummary(s.turnTrackerState(), &s.turnMu, threadID, turnID, summary)
}

func (s *Server) lookupTrackedTurnSummary(threadID, turnID string) string {
	return codexadapter.LookupTrackedTurnSummary(s.turnTrackerState(), &s.turnMu, threadID, turnID)
}

func injectTrackedTurnSummary(payload map[string]any, summary string) {
	codexadapter.InjectTrackedTurnSummary(payload, summary)
}

func (s *Server) captureAndInjectTurnSummary(threadID, eventType, method string, payload map[string]any) {
	codexadapter.CaptureAndInjectTurnSummary(s.captureAndInjectTurnSummaryOptions(threadID, eventType, method, payload))
}

func (s *Server) captureAndInjectTurnSummaryOptions(threadID, eventType, method string, payload map[string]any) codexadapter.CaptureAndInjectTurnSummaryOptions {
	return codexadapter.CaptureAndInjectTurnSummaryOptions{
		ThreadID:                      threadID,
		EventType:                     eventType,
		Method:                        method,
		Payload:                       payload,
		PeekTrackedTurnMeta:           s.peekTrackedTurnMeta,
		TrackedTurnTerminalFromEvent:  trackedTurnTerminalFromEvent,
		ExtractTrackedTurnID:          extractTrackedTurnID,
		TrackedTurnSummaryFromPayload: trackedTurnSummaryFromPayload,
		RememberTrackedTurnSummary:    s.rememberTrackedTurnSummary,
		LookupTrackedTurnSummary:      s.lookupTrackedTurnSummary,
		InjectTrackedTurnSummary:      injectTrackedTurnSummary,
	}
}

func mergeTrackedTurnCompletionPayload(payload, completion map[string]any) {
	codexadapter.MergeTrackedTurnCompletionPayload(payload, completion)
}

func trackedTurnPayloadDiagKV(payload map[string]any) []any {
	return codexadapter.TrackedTurnPayloadDiagKV(payload)
}

// checkTurnStall is called periodically by the stall timer.
// If no events have been received for the configured stall threshold, it pushes
// an alert and auto-interrupts the turn.

// rescheduleStallCheck schedules the next stall check timer.
// Must be called with s.turnMu held.

// handleStallGracePeriod begins the grace period on first stall detection:
// logs a warning, pushes a UI alert, and schedules a final check after the grace period.
// Must be called with s.turnMu released.

// executeStallAutoInterrupt performs the actual auto-interrupt after the grace period expires.
// Must be called with s.turnMu released and turn.stallAutoInterrupted already set.

// touchTrackedTurnLastEvent updates the LastEventAt heartbeat for the turn.
// Call this whenever any event arrives for a tracked turn.

func trackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	return codexadapter.TrackedTurnTerminalFromEvent(eventType, method, payload)
}

func extractTrackedTurnID(payload map[string]any) string {
	return codexadapter.ExtractTrackedTurnID(payload)
}

func extractTrackedTurnStatus(payload map[string]any) string {
	return codexadapter.ExtractTrackedTurnStatus(payload)
}

func extractTrackedTurnReason(payload map[string]any) string {
	return codexadapter.ExtractTrackedTurnReason(payload)
}

func extractTrackedString(payload map[string]any, keys ...string) string {
	return codexadapter.ExtractTrackedString(payload, keys...)
}

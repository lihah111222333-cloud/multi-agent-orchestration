package codexadapter

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
// CaptureAndInjectTurnSummary captures terminal summaries and injects them into completion payloads.
func (a *Adapter) CaptureAndInjectTurnSummary(threadID, eventType, method string, payload map[string]any) {
	captureAndInjectTurnSummaryCore(a.trackerHelperState(), threadID, eventType, method, payload)
}

// maybeFinalizeTrackedTurn applies turn-terminal events to tracked-turn state.
// maybeFinalizeTrackedTurn applies turn-terminal events to tracked-turn state.
func (a *Adapter) maybeFinalizeTrackedTurn(threadID, eventType, method string, payload map[string]any) {
	maybeFinalizeTrackedTurnCore(a.trackerHelperState(), threadID, eventType, method, payload, a.trackerNotify())
}

// FinalizeTrackedTurnEvent updates heartbeat and finalizes turn state from an incoming event.
// FinalizeTrackedTurnEvent updates heartbeat and finalizes turn state from an incoming event.
func (a *Adapter) FinalizeTrackedTurnEvent(
	threadID string,
	eventType string,
	method string,
	payload map[string]any,
) {
	finalizeTrackedTurnEventCore(a.trackerHelperState(), threadID, eventType, method, payload, a.trackerNotify())
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

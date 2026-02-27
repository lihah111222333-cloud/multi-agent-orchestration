package codexadapter

import trackersvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/tracker"

func (a *Adapter) ExtractTrackedString(payload map[string]any, keys ...string) string {
	return trackersvc.ExtractTrackedString(payload, keys...)
}

func (a *Adapter) TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	return trackersvc.TrackedTurnTerminalFromEvent(eventType, method, payload)
}

func (a *Adapter) TrackedTurnSummaryFromPayload(payload map[string]any) string {
	return trackersvc.TrackedTurnSummaryFromPayload(payload)
}

func (a *Adapter) CaptureAndInjectTurnSummary(threadID, eventType, method string, payload map[string]any) {
	state, _ := a.trackerStateAndNotify()
	trackersvc.CaptureAndInjectTurnSummaryCore(state, threadID, eventType, method, payload)
}

func (a *Adapter) FinalizeTrackedTurnEvent(threadID string, eventType string, method string, payload map[string]any) {
	state, notify := a.trackerStateAndNotify()
	trackersvc.FinalizeTrackedTurnEventCore(state, threadID, eventType, method, payload, notify)
}

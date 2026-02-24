package apiserver

import (
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type trackedTurn = codexadapter.TrackedTurn

func (s *Server) beginTrackedTurn(threadID, turnID string) string {
	return s.codexAdapter.BeginTrackedTurn(
		s.activeTurns,
		&s.turnMu,
		s.turnWatchdogTimeout,
		s.stallThreshold,
		threadID,
		turnID,
		codexadapter.BeginTrackedTurnHooks{
			EnsureTurnTrackerLocked: s.ensureTurnTrackerLocked,
			CompleteTrackedTurnByID: s.completeTrackedTurnByID,
			Notify:                  s.Notify,
			CheckTurnStall:          s.checkTurnStall,
		},
	)
}

func (s *Server) hasActiveTrackedTurn(threadID string) bool {
	return s.codexAdapter.HasActiveTrackedTurn(s.activeTurns, &s.turnMu, threadID)
}

func (s *Server) markTrackedTurnInterruptRequested(threadID string) bool {
	return s.codexAdapter.MarkTrackedTurnInterruptRequested(s.activeTurns, &s.turnMu, threadID)
}

func (s *Server) waitTrackedTurnTerminal(threadID string, timeout time.Duration) (string, bool) {
	return s.codexAdapter.WaitTrackedTurnTerminal(s.activeTurns, &s.turnMu, threadID, timeout)
}

func (s *Server) completeTrackedTurn(threadID, status, reason string) (map[string]any, bool) {
	return s.completeTrackedTurnByID(threadID, "", status, reason)
}

func (s *Server) completeTrackedTurnByID(threadID, turnID, status, reason string) (map[string]any, bool) {
	return s.codexAdapter.CompleteTrackedTurnByID(codexadapter.CompleteTrackedTurnByIDOptions{
		ActiveTurns: s.activeTurns,
		TurnMu:      &s.turnMu,
		ThreadID:    threadID,
		TurnID:      turnID,
		Status:      status,
		Reason:      reason,
	})
}

func (s *Server) maybeFinalizeTrackedTurn(threadID, eventType, method string, payload map[string]any) {
	s.codexAdapter.MaybeFinalizeTrackedTurn(codexadapter.MaybeFinalizeTrackedTurnOptions{
		ThreadID:                          threadID,
		EventType:                         eventType,
		Method:                            method,
		Payload:                           payload,
		PeekTrackedTurnMeta:               s.peekTrackedTurnMeta,
		TrackedTurnTerminalFromEvent:      trackedTurnTerminalFromEvent,
		ShouldLogTrackedTurnStallHint:     shouldLogTrackedTurnStallHint,
		MarkTrackedTurnStallHint:          s.markTrackedTurnStallHint,
		TrackedTurnPayloadDiagKV:          trackedTurnPayloadDiagKV,
		CompleteTrackedTurnByID:           s.completeTrackedTurnByID,
		TrackedTurnSummaryFromPayload:     trackedTurnSummaryFromPayload,
		LookupTrackedTurnSummary:          s.lookupTrackedTurnSummary,
		ExtractTrackedTurnID:              extractTrackedTurnID,
		InjectTrackedTurnSummary:          injectTrackedTurnSummary,
		RememberTrackedTurnSummary:        s.rememberTrackedTurnSummary,
		MergeTrackedTurnCompletionPayload: mergeTrackedTurnCompletionPayload,
		Notify:                            s.Notify,
		FirstNonEmpty:                     util.FirstNonEmpty,
	})
}

func (s *Server) captureTrackedTurnEventSummary(threadID, eventType, method string, payload map[string]any) {
	s.captureAndInjectTurnSummary(threadID, eventType, method, payload)
}

func (s *Server) finalizeTrackedTurnEvent(threadID, eventType, method string, payload map[string]any) {
	s.codexAdapter.FinalizeTrackedTurnEvent(codexadapter.FinalizeTrackedTurnEventOptions{
		ThreadID:                  threadID,
		EventType:                 eventType,
		Method:                    method,
		Payload:                   payload,
		TouchTrackedTurnLastEvent: s.touchTrackedTurnLastEvent,
		IsTerminalEventType:       isTerminalEventType,
		HasActiveTrackedTurn:      s.hasActiveTrackedTurn,
		MaybeFinalizeTrackedTurn:  s.maybeFinalizeTrackedTurn,
	})
}

func (s *Server) startApprovalStallHeartbeat(threadID string) func() {
	return codexadapter.StartApprovalStallHeartbeat(threadID, s.stallThreshold, defaultStallThreshold, s.touchTrackedTurnLastEvent)
}

func (s *Server) peekTrackedTurnMeta(threadID string) (string, time.Time, bool, bool) {
	return codexadapter.PeekTrackedTurnMeta(s.activeTurns, &s.turnMu, threadID)
}

func (s *Server) markTrackedTurnStallHint(threadID, turnID string) bool {
	return codexadapter.MarkTrackedTurnStallHint(s.activeTurns, &s.turnMu, threadID, turnID)
}

func shouldLogTrackedTurnStallHint(eventType, method string, startedAt time.Time) bool {
	return codexadapter.ShouldLogTrackedTurnStallHint(eventType, method, startedAt)
}

func (s *Server) checkTurnStall(threadID, turnID string) {
	s.codexAdapter.CheckTurnStall(codexadapter.CheckTurnStallOptions{
		ActiveTurns:            s.activeTurns,
		TurnMu:                 &s.turnMu,
		ThreadID:               threadID,
		TurnID:                 turnID,
		StallThreshold:         s.stallThreshold,
		DefaultStallThreshold:  defaultStallThreshold,
		Reschedule:             s.rescheduleStallCheck,
		HandleStallGracePeriod: s.handleStallGracePeriod,
		ExecuteStallInterrupt:  s.executeStallAutoInterrupt,
	})
}

func (s *Server) rescheduleStallCheck(turn *trackedTurn, threadID, turnID string, silent, threshold time.Duration) {
	codexadapter.RescheduleStallCheck(turn, threadID, turnID, silent, threshold, s.checkTurnStall)
}

func (s *Server) handleStallGracePeriod(threadID, turnID string, silent, threshold time.Duration) {
	var pushAlert func(threadID, category, message string)
	if s.uiRuntime != nil {
		pushAlert = s.uiRuntime.PushAlert
	}
	codexadapter.HandleStallGracePeriod(
		s.activeTurns,
		&s.turnMu,
		threadID,
		turnID,
		silent,
		threshold,
		30*time.Second,
		pushAlert,
		s.checkTurnStall,
	)
}

func (s *Server) executeStallAutoInterrupt(threadID, turnID string, silent, threshold time.Duration) {
	var pushAlert func(string, string, string)
	if s.uiRuntime != nil {
		pushAlert = s.uiRuntime.PushAlert
	}
	s.codexAdapter.ExecuteStallAutoInterrupt(codexadapter.ExecuteStallAutoInterruptOptions{
		ThreadID:                          threadID,
		TurnID:                            turnID,
		Silent:                            silent,
		Threshold:                         threshold,
		PushAlert:                         pushAlert,
		MarkTrackedTurnInterruptRequested: s.markTrackedTurnInterruptRequested,
		CancelCodeRuns:                    s.cancelCodeRuns,
		Manager:                           s.mgr,
		CompleteTrackedTurnByID:           s.completeTrackedTurnByID,
		Notify:                            s.Notify,
	})
}

func (s *Server) touchTrackedTurnLastEvent(threadID string) {
	codexadapter.TouchTrackedTurnLastEvent(s.activeTurns, &s.turnMu, threadID)
}

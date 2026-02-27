package apiserver

import (
	"sort"
	"strings"
	"time"

	trackersvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/tracker"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	defaultOrchestrationReportTTL = tools.DefaultOrchestrationReportTTL
)

func submitPrompt(s *Server, agentID, prompt string, images, files []string) error {
	if s == nil {
		return apperrors.New("Server.submitAgentPrompt", "server is nil")
	}
	if s.submitAgentMessage != nil {
		return s.submitAgentMessage(agentID, prompt, images, files)
	}
	if s.mgr == nil {
		return apperrors.New("Server.submitAgentPrompt", "agent manager not initialized")
	}
	return s.mgr.Submit(agentID, prompt, images, files)
}

func rememberReportRequest(s *Server, senderID, workerID string) {
	if s == nil {
		return
	}
	requester := strings.TrimSpace(senderID)
	target := strings.TrimSpace(workerID)
	if requester == "" || target == "" || strings.EqualFold(requester, target) {
		return
	}

	now := time.Now()

	waiterCount := rememberReportRequesterState(s, target, requester, now)
	if waiterCount == 0 {
		return
	}
	logger.Info("orchestration: report waiter registered",
		"worker", target,
		"requester", requester,
		"waiter_count", waiterCount,
	)
}

func maybeAutoReportOrchestrationCompletion(s *Server, agentID, eventType, method string, payload map[string]any) {
	workerID := strings.TrimSpace(agentID)
	if workerID == "" {
		return
	}

	if s == nil || s.codexAdapter == nil {
		return
	}
	_, status, reason, terminal, _ := trackersvc.TrackedTurnTerminalFromEvent(eventType, method, payload)
	if !terminal {
		return
	}

	requesters := takeOrchestrationReportRequesters(s, workerID)
	if len(requesters) == 0 {
		return
	}

	summary := strings.TrimSpace(trackersvc.TrackedTurnSummaryFromPayload(payload))
	if summary == "" {
		summary = trackersvc.ExtractTrackedString(payload, "uiText", "summary", "text", "message", "output")
	}

	report := tools.BuildOrchestrationCompletionReport(workerID, status, reason, summary)
	for _, requesterID := range requesters {
		if err := submitPrompt(s, requesterID, report, nil, nil); err != nil {
			logger.Warn("orchestration: auto report delivery failed",
				"from", workerID,
				"to", requesterID,
				logger.FieldError, err,
			)
			continue
		}
		logger.Info("orchestration: auto report delivered", "from", workerID, "to", requesterID, logger.FieldStatus, status)
	}
}

func takeOrchestrationReportRequesters(s *Server, workerID string) []string {
	if s == nil {
		return nil
	}
	target := strings.TrimSpace(workerID)
	if target == "" {
		return nil
	}

	requesters := takeReportRequestersState(s, target, time.Now())
	if len(requesters) == 0 {
		return nil
	}
	sort.Strings(requesters)
	logger.Info("orchestration: report waiters drained",
		"worker", target,
		"requester_count", len(requesters),
	)
	return requesters
}

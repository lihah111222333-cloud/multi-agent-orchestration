package apiserver

import (
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/tools"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	defaultOrchestrationReportTTL = tools.DefaultOrchestrationReportTTL
)

func (s *Server) SubmitPrompt(agentID, prompt string, images, files []string) error {
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

func (s *Server) RememberReportRequest(senderID, workerID string) {
	requester := strings.TrimSpace(senderID)
	target := strings.TrimSpace(workerID)
	if requester == "" || target == "" || strings.EqualFold(requester, target) {
		return
	}

	now := time.Now()

	s.orchestrationReportMu.Lock()
	defer s.orchestrationReportMu.Unlock()
	s.ensureOrchestrationReportStateLocked()
	s.pruneOrchestrationReportRequestsLocked(now)

	waiters := s.orchestrationPendingReports[target]
	if waiters == nil {
		waiters = make(map[string]time.Time)
		s.orchestrationPendingReports[target] = waiters
	}
	waiters[requester] = now
	logger.Info("orchestration: report waiter registered",
		"worker", target,
		"requester", requester,
		"waiter_count", len(waiters),
	)
}

func (s *Server) maybeAutoReportOrchestrationCompletion(agentID, eventType, method string, payload map[string]any) {
	workerID := strings.TrimSpace(agentID)
	if workerID == "" {
		return
	}

	if s == nil || s.codexAdapter == nil {
		return
	}
	_, status, reason, terminal, _ := s.codexAdapter.TrackedTurnTerminalFromEvent(eventType, method, payload)
	if !terminal {
		return
	}

	requesters := s.takeOrchestrationReportRequesters(workerID)
	if len(requesters) == 0 {
		return
	}

	summary := strings.TrimSpace(s.codexAdapter.TrackedTurnSummaryFromPayload(payload))
	if summary == "" {
		summary = s.codexAdapter.ExtractTrackedString(payload, "uiText", "summary", "text", "message", "output")
	}

	report := tools.BuildOrchestrationCompletionReport(workerID, status, reason, summary)
	for _, requesterID := range requesters {
		if err := s.SubmitPrompt(requesterID, report, nil, nil); err != nil {
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

func (s *Server) takeOrchestrationReportRequesters(workerID string) []string {
	target := strings.TrimSpace(workerID)
	if target == "" {
		return nil
	}

	now := time.Now()

	s.orchestrationReportMu.Lock()
	defer s.orchestrationReportMu.Unlock()
	s.ensureOrchestrationReportStateLocked()
	s.pruneOrchestrationReportRequestsLocked(now)

	waiters := s.orchestrationPendingReports[target]
	if len(waiters) == 0 {
		return nil
	}
	delete(s.orchestrationPendingReports, target)

	requesters := make([]string, 0, len(waiters))
	for requesterID := range waiters {
		id := strings.TrimSpace(requesterID)
		if id != "" {
			requesters = append(requesters, id)
		}
	}
	sort.Strings(requesters)
	logger.Info("orchestration: report waiters drained",
		"worker", target,
		"requester_count", len(requesters),
	)
	return requesters
}

func (s *Server) ensureOrchestrationReportStateLocked() {
	if s.orchestrationPendingReports == nil {
		s.orchestrationPendingReports = make(map[string]map[string]time.Time)
	}
	if s.orchestrationReportTTL <= 0 {
		s.orchestrationReportTTL = defaultOrchestrationReportTTL
	}
}

func (s *Server) pruneOrchestrationReportRequestsLocked(now time.Time) {
	ttl := s.orchestrationReportTTL
	if ttl <= 0 {
		ttl = defaultOrchestrationReportTTL
		s.orchestrationReportTTL = ttl
	}
	cutoff := now.Add(-ttl)

	for workerID, waiters := range s.orchestrationPendingReports {
		for requesterID, createdAt := range waiters {
			if createdAt.Before(cutoff) {
				delete(waiters, requesterID)
			}
		}
		if len(waiters) == 0 {
			delete(s.orchestrationPendingReports, workerID)
		}
	}
}

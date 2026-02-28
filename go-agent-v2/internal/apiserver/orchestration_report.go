package apiserver

import (
	"sort"
	"strings"
	"time"

	trackersvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/tracker"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
)

const defaultOrchestrationReportTTL = tools.DefaultOrchestrationReportTTL

func submitPrompt(s *Server, agentID, prompt string, images, files []string) error {
	if s == nil { return apperrors.New("Server.submitAgentPrompt", "server is nil") }
	if s.submitAgentMessage != nil { return s.submitAgentMessage(agentID, prompt, images, files) }
	if s.mgr == nil { return apperrors.New("Server.submitAgentPrompt", "agent manager not initialized") }
	return s.mgr.Submit(agentID, prompt, images, files)
}

func rememberReportRequest(s *Server, senderID, workerID string) {
	if s == nil { return }
	requester := strings.TrimSpace(senderID)
	target := strings.TrimSpace(workerID)
	if requester == "" || target == "" || strings.EqualFold(requester, target) { return }
	if waiterCount := rememberReportRequesterState(s, target, requester, time.Now()); waiterCount > 0 {
		logger.Info("orchestration: report waiter registered", "worker", target, "requester", requester, "waiter_count", waiterCount)
	}
}

func maybeAutoReportOrchestrationCompletion(s *Server, agentID, eventType, method string, payload map[string]any) {
	workerID := strings.TrimSpace(agentID)
	if workerID == "" || s == nil || s.codexAdapter == nil {
		return
	}
	_, status, reason, terminal, _ := trackersvc.TrackedTurnTerminalFromEvent(eventType, method, payload)
	if !terminal {
		return
	}

	requesters := takeReportRequestersState(s, workerID, time.Now())
	if len(requesters) == 0 {
		return
	}
	sort.Strings(requesters)
	logger.Info("orchestration: report waiters drained", "worker", workerID, "requester_count", len(requesters))

	summary := strings.TrimSpace(trackersvc.TrackedTurnSummaryFromPayload(payload))
	if summary == "" { summary = trackersvc.ExtractTrackedString(payload, "uiText", "summary", "text", "message", "output") }

	report := tools.BuildOrchestrationCompletionReport(workerID, status, reason, summary)
	for _, requesterID := range requesters {
		if err := submitPrompt(s, requesterID, report, nil, nil); err != nil {
			logger.Warn("orchestration: auto report delivery failed", "from", workerID, "to", requesterID, logger.FieldError, err)
			continue
		}
		logger.Info("orchestration: auto report delivered", "from", workerID, "to", requesterID, logger.FieldStatus, status)
	}
}

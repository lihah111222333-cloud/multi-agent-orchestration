package apiserver

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func AgentEventHandler(s *Server, agentID string) agentcore.EventHandler {
	return func(event agentcore.Event) {
		method := mapEventToMethod(event.Type)

		threadID := ""
		if proc := s.mgr.Get(agentID); proc != nil {
			threadID = s.codexAdapter.GetThreadID(proc)
		}
		logger.Debug("codex event",
			logger.FieldSource, "codex",
			logger.FieldComponent, "event",
			logger.FieldAgentID, agentID,
			logger.FieldThreadID, threadID,
			logger.FieldEventType, event.Type,
		)

		payload := map[string]any{
			"threadId": agentID,
		}

		mergePayloadFields(payload, event.Data)

		if rawTID, _ := payload["threadId"].(string); rawTID != "" && rawTID != agentID {
			payload["codexThreadId"] = rawTID
		}
		payload["threadId"] = agentID
		enrichFileChangePayload(s, agentID, event.Type, method, payload)
		enrichReadCommandPayload(s, event.Type, payload)
		s.codexAdapter.CaptureAndInjectTurnSummary(agentID, event.Type, method, payload)
		if method == "error" {
			willRetry, hasWillRetry := extractBoolFromPayload(payload, "willRetry", "will_retry", "recoverable")
			if !hasWillRetry {
				willRetry = strings.EqualFold(strings.TrimSpace(event.Type), agentcore.EventStreamError)
			}
			payload["willRetry"] = willRetry
			payload["will_retry"] = willRetry
			if _, exists := payload["error"]; !exists {
				payload["error"] = map[string]any{
					"message":           util.ExtractFirstString(payload, "message", "reason"),
					"additionalDetails": util.ExtractFirstString(payload, "additional_details", "additionalDetails"),
				}
			}
		}

		normalized := uistate.NormalizeEventFromPayload(event.Type, method, payload)
		payload["uiType"] = string(normalized.UIType)
		if normalized.Text != "" {
			payload["uiText"] = normalized.Text
		}
		if normalized.Command != "" {
			payload["uiCommand"] = normalized.Command
		}
		if len(normalized.Files) > 0 {
			payload["uiFiles"] = normalized.Files
		}
		if normalized.ExitCode != nil {
			payload["uiExitCode"] = *normalized.ExitCode
		}
		if normalized.UIType == uistate.UITypeTurnStarted {
			emitTurnStartDiffReset(s, agentID, payload)
		}
		if s.uiRuntime != nil {
			s.uiRuntime.ApplyAgentEvent(agentID, normalized, payload)
		}

		s.codexAdapter.FinalizeTrackedTurnEvent(agentID, event.Type, method, payload)
		maybeAutoReportOrchestrationCompletion(s, agentID, event.Type, method, payload)

		switch event.Type {
		case agentcore.EventDynamicToolCall:
			util.SafeGo(func() { handleDynamicToolCall(s, agentID, event) })
			return
		case agentcore.EventExecApprovalRequest, agentcore.EventFileChangeApprovalRequest, approvalMethodSkillRequest:
			approvalMethod := approvalMethodSkillRequest
			if event.Type == agentcore.EventExecApprovalRequest {
				approvalMethod = approvalMethodCommandExecution
			} else if event.Type == agentcore.EventFileChangeApprovalRequest {
				approvalMethod = approvalMethodFileChange
			}
			util.SafeGo(func() { handleApprovalRequest(s, agentID, approvalMethod, payload, event) })
			return
		}

		notify(s, method, payload)
	}
}

func emitTurnStartDiffReset(s *Server, threadID string, payload map[string]any) {
	id := strings.TrimSpace(threadID)
	if s == nil || id == "" {
		return
	}
	resetPayload := map[string]any{
		"threadId": id,
		"diff":     "",
		"uiText":   "",
	}
	if codexThreadID := strings.TrimSpace(util.ExtractFirstString(payload, "codexThreadId")); codexThreadID != "" {
		resetPayload["codexThreadId"] = codexThreadID
	}

	normalized := uistate.NormalizeEventFromPayload(agentcore.EventTurnDiff, "turn/diff/updated", resetPayload)
	resetPayload["uiType"] = string(normalized.UIType)
	if s.uiRuntime != nil {
		s.uiRuntime.ApplyAgentEvent(id, normalized, resetPayload)
	}
	notify(s, "turn/diff/updated", resetPayload)
}

func enrichReadCommandPayload(s *Server, eventType string, payload map[string]any) {
	if s == nil || payload == nil || eventType != agentcore.EventExecCommandBegin {
		return
	}
	cmd := extractCommandBaseName(payload)
	if cmd == "" || !readCommands[cmd] {
		return
	}
	payload["isReadCommand"] = true
	payload["lspHint"] = lspPreferenceHint
	incrementToolCallState(s, "shell_read:"+cmd)
	logger.Info("codex shell: read command detected",
		logger.FieldCommand, cmd,
	)
}

func extractCommandBaseName(payload map[string]any) string {
	raw := strings.TrimSpace(util.ExtractFirstString(payload, "command", "displayCommand", "command_display", "cmd"))
	if raw == "" {
		return ""
	}
	fields := strings.Fields(raw)
	base := fields[0]
	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}
	return base
}

package apiserver

import (
	"context"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	approvalMethodCommandExecution = "item/commandExecution/requestApproval"
	approvalMethodFileChange       = "item/fileChange/requestApproval"
	approvalMethodSkillRequest     = "skill/requestApproval"
)

func approvalStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case interface{ String() string }:
		return typed.String()
	default:
		return ""
	}
}

func extractBoolFromPayload(payload map[string]any, keys ...string) (bool, bool) {
	if payload == nil {
		return false, false
	}
	for _, key := range keys {
		value, ok := payload[key]
		if !ok { continue }
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "y":
				return true, true
			case "false", "0", "no", "n":
				return false, true
			}
		}
	}
	return false, false
}

func extractMapValue(payload map[string]any, keys ...string) (any, bool) {
	if payload == nil {
		return nil, false
	}
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			return value, true
		}
	}
	for existingKey, value := range payload {
		for _, key := range keys {
			if strings.EqualFold(strings.TrimSpace(existingKey), strings.TrimSpace(key)) {
				return value, true
			}
		}
	}
	return nil, false
}

var approvalStringAliases = map[string]string{
	"accept": "accept", "approved": "accept", "approve": "accept",
	"yes": "accept", "y": "accept", "true": "accept", "1": "accept",
	"acceptforsession": "acceptForSession", "accept_for_session": "acceptForSession", "accept-for-session": "acceptForSession",
	"decline": "decline", "denied": "decline", "reject": "decline", "rejected": "decline",
	"no": "decline", "n": "decline", "false": "decline", "0": "decline",
	"cancel": "cancel", "abort": "cancel", "aborted": "cancel",
}

var approvalMethodValidDecisions = map[string]map[string]bool{
	approvalMethodCommandExecution: {"accept": true, "acceptForSession": true, "decline": true, "cancel": true},
	approvalMethodFileChange:       {"accept": true, "acceptForSession": true, "decline": true, "cancel": true},
	approvalMethodSkillRequest:     {"approve": true, "decline": true},
}

func normalizeCommandAmendmentDecision(decisionObj map[string]any) (any, bool) {
	if amendmentRaw, ok := extractMapValue(decisionObj, "acceptWithExecpolicyAmendment"); ok {
		amendmentObj, ok := amendmentRaw.(map[string]any)
		if !ok || amendmentObj == nil {
			return nil, false
		}
		execPolicyAmendment, ok := extractMapValue(amendmentObj, "execpolicy_amendment", "execpolicyAmendment")
		if !ok {
			return nil, false
		}
		return map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": execPolicyAmendment}}, true
	}
	if amendmentRaw, ok := extractMapValue(decisionObj, "applyNetworkPolicyAmendment"); ok {
		amendmentObj, ok := amendmentRaw.(map[string]any)
		if !ok || amendmentObj == nil {
			return nil, false
		}
		networkPolicyAmendment, ok := extractMapValue(amendmentObj, "network_policy_amendment", "networkPolicyAmendment")
		if !ok {
			return nil, false
		}
		return map[string]any{"applyNetworkPolicyAmendment": map[string]any{"network_policy_amendment": networkPolicyAmendment}}, true
	}
	return nil, false
}

func normalizeApprovalDecision(method string, raw any) (any, bool) {
	method = strings.TrimSpace(method)
	if decision, ok := approvalStringAliases[strings.ToLower(strings.TrimSpace(approvalStringValue(raw)))]; ok {
		if method == approvalMethodSkillRequest && decision == "accept" {
			decision = "approve"
		}
		valid := approvalMethodValidDecisions[method]
		if valid == nil || valid[decision] {
			return decision, true
		}
	}
	if decisionObj, ok := raw.(map[string]any); ok && decisionObj != nil {
		if method == approvalMethodCommandExecution || method == "" {
			return normalizeCommandAmendmentDecision(decisionObj)
		}
	}
	if method != approvalMethodCommandExecution && method != approvalMethodFileChange && method != approvalMethodSkillRequest {
		for _, fallbackMethod := range []string{approvalMethodCommandExecution, approvalMethodFileChange, approvalMethodSkillRequest} {
			if decision, ok := normalizeApprovalDecision(fallbackMethod, raw); ok {
				return decision, true
			}
		}
	}
	return nil, false
}

func normalizeApprovalResultPayload(method string, result any) (map[string]any, bool) {
	switch typed := result.(type) {
	case bool:
		return approvalDecisionPayload(method, typed), true
	case string:
		decision, ok := normalizeApprovalDecision(method, typed)
		if !ok {
			return nil, false
		}
		return map[string]any{"decision": decision}, true
	case map[string]any:
		if decisionRaw, ok := extractMapValue(typed, "decision"); ok {
			if decision, ok := normalizeApprovalDecision(method, decisionRaw); ok {
				return map[string]any{"decision": decision}, true
			}
		}
		if approved, ok := extractBoolFromPayload(typed, "approved"); ok {
			return approvalDecisionPayload(method, approved), true
		}
	}
	return nil, false
}

func mergeApprovalDecisionPayload(method string, current map[string]any, result any) map[string]any {
	if normalized, ok := normalizeApprovalResultPayload(method, result); ok { return normalized }
	return current
}

func approvalDecisionPayload(method string, approved bool) map[string]any {
	decision := "accept"
	if !approved {
		decision = "decline"
	} else if strings.TrimSpace(method) == approvalMethodSkillRequest {
		decision = "approve"
	}
	return map[string]any{"decision": decision}
}

func approvalDecisionAllowsSubmit(method string, payload map[string]any) bool {
	if payload == nil {
		return false
	}
	decisionRaw, ok := extractMapValue(payload, "decision")
	if !ok {
		return false
	}
	method = strings.TrimSpace(method)
	if method == approvalMethodCommandExecution || method == "" {
		if decisionObj, ok := decisionRaw.(map[string]any); ok && decisionObj != nil {
			if _, ok := extractMapValue(decisionObj, "acceptWithExecpolicyAmendment"); ok {
				return true
			}
			if networkRaw, ok := extractMapValue(decisionObj, "applyNetworkPolicyAmendment"); ok {
				networkObj, ok := networkRaw.(map[string]any)
				if !ok || networkObj == nil {
					return false
				}
				amendmentRaw, ok := extractMapValue(networkObj, "network_policy_amendment", "networkPolicyAmendment")
				if !ok {
					return false
				}
				amendmentObj, ok := amendmentRaw.(map[string]any)
				if !ok || amendmentObj == nil {
					return false
				}
				action := strings.ToLower(strings.TrimSpace(approvalStringValue(amendmentObj["action"])))
				return action == "allow"
			}
			return false
		}
	}
	decision, ok := normalizeApprovalDecision(method, decisionRaw)
	if !ok {
		return false
	}
	decisionStr, _ := decision.(string)
	if method == approvalMethodSkillRequest {
		return decisionStr == "approve"
	}
	return decisionStr == "accept" || decisionStr == "acceptForSession"
}

func denyApprovalSafely(event agentcore.Event, agentID string) {
	if event.DenyFunc != nil {
		if denyErr := event.DenyFunc(); denyErr != nil {
			logger.Warn("app-server: deny callback failed", logger.FieldAgentID, agentID, logger.FieldError, denyErr)
		}
	}
}

func submitApprovalLegacyDecision(s *Server, agentID, method, decision string, event agentcore.Event, denyOnMissing, logSubmitErr bool) {
	if s == nil || s.mgr == nil {
		if denyOnMissing {
			logger.Error("app-server: approval auto-denied — mgr is nil", logger.FieldAgentID, agentID, logger.FieldMethod, method)
			denyApprovalSafely(event, agentID)
		}
		return
	}
	proc := s.mgr.Get(agentID)
	if proc == nil {
		if denyOnMissing {
			logger.Error("app-server: approval auto-denied — agent gone", logger.FieldAgentID, agentID, logger.FieldMethod, method)
			denyApprovalSafely(event, agentID)
		}
		return
	}
	if err := s.codexAdapter.Submit(proc, decision, nil, nil, nil); err != nil && logSubmitErr {
		logger.Warn("app-server: relay approval to codex failed", logger.FieldAgentID, agentID, logger.FieldError, err)
	}
}

func handleApprovalRequest(s *Server, agentID, method string, payload map[string]any, event agentcore.Event) {
	if s == nil {
		return
	}
	isSubAgent := false
	detectedBy := ""
	agentCount := 0
	firstID := ""
	if s.mgr != nil {
		agentCount = s.mgr.Count()
		firstID = s.mgr.FirstAgentID()
		if agentCount > 1 && firstID != "" && firstID != agentID {
			isSubAgent = true
			detectedBy = "mgr_count"
		}
	}
	if !isSubAgent && s.uiRuntime != nil && !s.uiRuntime.IsMainAgent(agentID) {
		isSubAgent = true
		detectedBy = "uiRuntime_isMain"
	}
	logger.Info("app-server: approval request received", logger.FieldAgentID, agentID, logger.FieldMethod, method, "agent_count", agentCount, "first_agent_id", firstID, "is_sub_agent", isSubAgent, "detected_by", detectedBy)

	if isSubAgent {
		logger.Info("app-server: sub-agent auto-approved", logger.FieldAgentID, agentID, logger.FieldMethod, method, "agent_count", agentCount, "first_agent_id", firstID, "detected_by", detectedBy)
		if event.RespondResultFunc != nil {
			if err := event.RespondResultFunc(approvalDecisionPayload(method, true)); err != nil {
				logger.Warn("app-server: sub-agent auto-approve respond failed", logger.FieldAgentID, agentID, logger.FieldMethod, method, logger.FieldError, err)
				denyApprovalSafely(event, agentID)
			}
		} else {
			submitApprovalLegacyDecision(s, agentID, method, "yes", event, false, false)
		}
		return
	}
	inflightKey := agentID + ":" + method
	if !tryBeginApprovalState(s, inflightKey) {
		logger.Debug("app-server: approval dedup — skipping duplicate in-flight request", logger.FieldAgentID, agentID, logger.FieldMethod, method)
		return
	}
	defer endApprovalState(s, inflightKey)
	stopHeartbeat := s.codexAdapter.StartApprovalStallHeartbeat(agentID)
	defer stopHeartbeat()

	decisionPayload := approvalDecisionPayload(method, false)
	resp, wsErr := sendRequestToAll(s, method, payload)
	if wsErr == nil && resp != nil && resp.Result != nil {
		decisionPayload = mergeApprovalDecisionPayload(method, decisionPayload, resp.Result)
	} else if hasNotifyHookState(s) {
		logger.Info("app-server: approval via Wails mode (no WS client)", logger.FieldAgentID, agentID, logger.FieldMethod, method)

		reqID, ch, cleanup := allocPendingRequest(s)
		defer cleanup()
		if payload == nil { payload = make(map[string]any) }
		payload["requestId"] = reqID
		if s.uiRuntime != nil {
			threadID := strings.TrimSpace(agentID)
			normalized := uistate.NormalizeEventFromPayload(method, method, payload)
			s.uiRuntime.ApplyAgentEvent(threadID, normalized, payload)
			throttledUIStateChanged(s, map[string]any{"source": method, "threadId": threadID})
		}
		broadcastNotification(s, method, payload)
		timer := time.NewTimer(5 * time.Minute)
		defer timer.Stop()
		select {
		case wailsResp := <-ch:
			if wailsResp != nil {
				decisionPayload = mergeApprovalDecisionPayload(method, decisionPayload, wailsResp.Result)
			}
		case <-timer.C:
			logger.Warn("app-server: approval timed out (Wails mode)", logger.FieldAgentID, agentID, logger.FieldMethod, method)
		}
	} else {
		logger.Warn("app-server: approval auto-denied — no WS client and no notifyHook", logger.FieldAgentID, agentID, logger.FieldMethod, method)
	}

	if event.RespondResultFunc != nil {
		if err := event.RespondResultFunc(decisionPayload); err != nil {
			logger.Warn("app-server: respond approval result failed", logger.FieldAgentID, agentID, logger.FieldMethod, method, logger.FieldError, err)
			denyApprovalSafely(event, agentID)
		}
		return
	}
	legacyDecision := "no"
	if approvalDecisionAllowsSubmit(method, decisionPayload) {
		legacyDecision = "yes"
	}
	submitApprovalLegacyDecision(s, agentID, method, legacyDecision, event, true, true)
}

type approvalRespondParams struct {
	RequestID int64 `json:"requestId"`
	Approved  *bool `json:"approved,omitempty"`
	Decision  any   `json:"decision,omitempty"`
}

func approvalRespondStatus(ok bool, status string) map[string]any {
	return map[string]any{"ok": ok, "status": status}
}

func approvalRespondResultPayload(p approvalRespondParams) (map[string]any, bool) {
	result := map[string]any{}
	if p.Decision != nil { result["decision"] = p.Decision }
	if p.Approved != nil { result["approved"] = *p.Approved }
	return result, len(result) > 0
}

func approvalRespondTyped(s *Server, _ context.Context, p approvalRespondParams) (any, error) {
	if s == nil { return approvalRespondStatus(false, "server_not_ready"), nil }
	if p.RequestID <= 0 { return approvalRespondStatus(false, "invalid_request_id"), nil }

	result, ok := approvalRespondResultPayload(p)
	if !ok { return approvalRespondStatus(false, "decision_or_approved_required"), nil }

	if !ResolvePendingRequest(s, p.RequestID, result) { return approvalRespondStatus(false, "not_pending"), nil }

	return approvalRespondStatus(true, "resolved"), nil
}

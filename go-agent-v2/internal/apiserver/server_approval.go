// server_approval.go — 审批事件处理: Server→Client 请求 → 等回复 → 回传 codex。
package apiserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
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
	case fmt.Stringer:
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
		if !ok {
			continue
		}
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

func normalizeApprovalDecisionString(raw string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "accept", "approved", "yes", "y", "true", "1":
		return "accept", true
	case "acceptforsession", "accept_for_session", "accept-for-session":
		return "acceptForSession", true
	case "decline", "denied", "no", "n", "false", "0":
		return "decline", true
	case "cancel", "abort", "aborted":
		return "cancel", true
	default:
		return "", false
	}
}

func normalizeFileChangeApprovalDecision(raw any) (any, bool) {
	decision, ok := normalizeApprovalDecisionString(approvalStringValue(raw))
	if !ok {
		return nil, false
	}
	switch decision {
	case "accept", "acceptForSession", "decline", "cancel":
		return decision, true
	default:
		return nil, false
	}
}

func normalizeCommandApprovalDecision(raw any) (any, bool) {
	if decision, ok := normalizeApprovalDecisionString(approvalStringValue(raw)); ok {
		switch decision {
		case "accept", "acceptForSession", "decline", "cancel":
			return decision, true
		}
	}
	decisionObj, ok := raw.(map[string]any)
	if !ok || decisionObj == nil {
		return nil, false
	}

	if amendmentRaw, ok := extractMapValue(decisionObj, "acceptWithExecpolicyAmendment"); ok {
		amendmentObj, ok := amendmentRaw.(map[string]any)
		if !ok || amendmentObj == nil {
			return nil, false
		}
		execPolicyAmendment, ok := extractMapValue(amendmentObj, "execpolicy_amendment", "execpolicyAmendment")
		if !ok {
			return nil, false
		}
		return map[string]any{
			"acceptWithExecpolicyAmendment": map[string]any{
				"execpolicy_amendment": execPolicyAmendment,
			},
		}, true
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
		return map[string]any{
			"applyNetworkPolicyAmendment": map[string]any{
				"network_policy_amendment": networkPolicyAmendment,
			},
		}, true
	}
	return nil, false
}

func normalizeSkillApprovalDecision(raw any) (any, bool) {
	normalized := strings.ToLower(strings.TrimSpace(approvalStringValue(raw)))
	switch normalized {
	case "approve", "approved", "accept", "yes", "y", "true", "1":
		return "approve", true
	case "decline", "denied", "reject", "rejected", "cancel", "abort", "aborted", "no", "n", "false", "0":
		return "decline", true
	default:
		return nil, false
	}
}

func normalizeApprovalDecision(method string, raw any) (any, bool) {
	switch strings.TrimSpace(method) {
	case approvalMethodCommandExecution:
		return normalizeCommandApprovalDecision(raw)
	case approvalMethodFileChange:
		return normalizeFileChangeApprovalDecision(raw)
	case approvalMethodSkillRequest:
		return normalizeSkillApprovalDecision(raw)
	default:
		if decision, ok := normalizeCommandApprovalDecision(raw); ok {
			return decision, true
		}
		if decision, ok := normalizeFileChangeApprovalDecision(raw); ok {
			return decision, true
		}
		return normalizeSkillApprovalDecision(raw)
	}
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
			decision, ok := normalizeApprovalDecision(method, decisionRaw)
			if ok {
				return map[string]any{"decision": decision}, true
			}
		}
		if approved, ok := extractBoolFromPayload(typed, "approved"); ok {
			return approvalDecisionPayload(method, approved), true
		}
	}
	return nil, false
}

func approvalDecisionPayload(method string, approved bool) map[string]any {
	decision := "decline"
	if approved {
		decision = "accept"
		if strings.TrimSpace(method) == approvalMethodSkillRequest {
			decision = "approve"
		}
	}
	return map[string]any{"decision": decision}
}

func commandDecisionAllowsSubmit(raw any) bool {
	decision, ok := normalizeCommandApprovalDecision(raw)
	if !ok {
		return false
	}
	switch typed := decision.(type) {
	case string:
		return typed == "accept" || typed == "acceptForSession"
	case map[string]any:
		if _, ok := extractMapValue(typed, "acceptWithExecpolicyAmendment"); ok {
			return true
		}
		if networkRaw, ok := extractMapValue(typed, "applyNetworkPolicyAmendment"); ok {
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
	}
	return false
}

func approvalDecisionAllowsSubmit(method string, payload map[string]any) bool {
	if payload == nil {
		return false
	}
	decisionRaw, ok := extractMapValue(payload, "decision")
	if !ok {
		return false
	}
	switch strings.TrimSpace(method) {
	case approvalMethodFileChange:
		decision, ok := normalizeFileChangeApprovalDecision(decisionRaw)
		if !ok {
			return false
		}
		decisionStr, _ := decision.(string)
		return decisionStr == "accept" || decisionStr == "acceptForSession"
	case approvalMethodSkillRequest:
		decision, ok := normalizeSkillApprovalDecision(decisionRaw)
		if !ok {
			return false
		}
		decisionStr, _ := decision.(string)
		return decisionStr == "approve"
	default:
		return commandDecisionAllowsSubmit(decisionRaw)
	}
}

func denyApprovalSafely(event agentcore.Event, agentID string) {
	if event.DenyFunc == nil {
		return
	}
	if denyErr := event.DenyFunc(); denyErr != nil {
		logger.Warn("app-server: deny callback failed", logger.FieldAgentID, agentID, logger.FieldError, denyErr)
	}
}

// handleApprovalRequest 处理审批事件: 双通道模式。
//
// 优先尝试 WebSocket (SendRequestToAll) — 适用于 IDE 客户端。
// 若无 WebSocket 连接, 降级为 Wails 模式:
//  1. AllocPendingRequest 分配 pending ID
//  2. broadcastNotification 推送审批请求 (→ notifyHook → Wails Event → 前端)
//  3. 等待前端 CallAPI("approval/respond") → ResolvePendingRequest 写入 channel
func handleApprovalRequest(s *Server, agentID, method string, payload map[string]any, event agentcore.Event) {
	if s == nil {
		return
	}
	// 去重: 同一 agentID+method 正在处理中 → 跳过重复调用
	inflightKey := agentID + ":" + method
	if !tryBeginApprovalState(s, inflightKey) {
		logger.Debug("app-server: approval dedup — skipping duplicate in-flight request",
			logger.FieldAgentID, agentID, logger.FieldMethod, method)
		return
	}
	defer endApprovalState(s, inflightKey)

	// 心跳委派到 turn tracker: 防止 stall 检测在等待审批期间误杀。
	stopHeartbeat := s.codexAdapter.StartApprovalStallHeartbeat(agentID)
	defer stopHeartbeat()

	decisionPayload := approvalDecisionPayload(method, false)

	// 尝试 WebSocket 通道 (IDE 客户端)
	resp, wsErr := sendRequestToAll(s, method, payload)
	if wsErr == nil && resp != nil && resp.Result != nil {
		if normalized, ok := normalizeApprovalResultPayload(method, resp.Result); ok {
			decisionPayload = normalized
		}
	} else {
		// 降级: Wails 模式 — 通过 broadcastNotification + pending channel
		// 仅在有 notifyHook (Wails 前端) 时才等待, 否则直接跳过 (默认 decline)
		hasHook := hasNotifyHookState(s)

		if hasHook {
			logger.Info("app-server: approval via Wails mode (no WS client)",
				logger.FieldAgentID, agentID, logger.FieldMethod, method)

			reqID, ch, cleanup := allocPendingRequest(s)
			defer cleanup()

			// 注入 requestId, 前端据此回复
			if payload == nil {
				payload = make(map[string]any)
			}
			payload["requestId"] = reqID

			// 同步回灌到 uiRuntime，确保 timeline 审批卡拿到 requestId 可交互。
			if s.uiRuntime != nil {
				threadID := strings.TrimSpace(agentID)
				normalized := uistate.NormalizeEventFromPayload(method, method, payload)
				s.uiRuntime.ApplyAgentEvent(threadID, normalized, payload)
				throttledUIStateChanged(s, map[string]any{
					"source":   method,
					"threadId": threadID,
				})
			}

			// 推送审批请求到前端 (→ notifyHook → Wails Event)
			broadcastNotification(s, method, payload)

			// 等待前端回复 (5 分钟超时)
			timer := time.NewTimer(5 * time.Minute)
			defer timer.Stop()
			select {
			case wailsResp := <-ch:
				if wailsResp != nil {
					if normalized, ok := normalizeApprovalResultPayload(method, wailsResp.Result); ok {
						decisionPayload = normalized
					}
				}
			case <-timer.C:
				logger.Warn("app-server: approval timed out (Wails mode)",
					logger.FieldAgentID, agentID, logger.FieldMethod, method)
			}
		} else {
			// 无前端连接: 无法交互, 自动拒绝
			logger.Warn("app-server: approval auto-denied — no WS client and no notifyHook",
				logger.FieldAgentID, agentID, logger.FieldMethod, method)
		}
	}

	if event.RespondResultFunc != nil {
		if err := event.RespondResultFunc(decisionPayload); err != nil {
			logger.Warn("app-server: respond approval result failed",
				logger.FieldAgentID, agentID,
				logger.FieldMethod, method,
				logger.FieldError, err,
			)
			denyApprovalSafely(event, agentID)
		}
		return
	}

	// 回退兼容路径: 老协议下无 Request-Response 回调时, 仍用 yes/no submit。
	if s.mgr == nil {
		logger.Error("app-server: approval auto-denied — mgr is nil",
			logger.FieldAgentID, agentID, logger.FieldMethod, method)
		denyApprovalSafely(event, agentID)
		return
	}
	proc := s.mgr.Get(agentID)
	if proc == nil {
		logger.Error("app-server: approval auto-denied — agent gone",
			logger.FieldAgentID, agentID, logger.FieldMethod, method)
		denyApprovalSafely(event, agentID)
		return
	}
	legacyDecision := "no"
	if approvalDecisionAllowsSubmit(method, decisionPayload) {
		legacyDecision = "yes"
	}
	if err := s.codexAdapter.Submit(proc, legacyDecision, nil, nil, nil); err != nil {
		logger.Warn("app-server: relay approval to codex failed", logger.FieldAgentID, agentID, logger.FieldError, err)
	}
}

// ========================================
// 审批回复 JSON-RPC 方法 (merged from methods_approval.go)
// ========================================

type approvalRespondParams struct {
	RequestID int64 `json:"requestId"`
	Approved  *bool `json:"approved,omitempty"`
	Decision  any   `json:"decision,omitempty"`
}

func approvalRespondResultPayload(p approvalRespondParams) (map[string]any, bool) {
	result := make(map[string]any, 2)
	hasResult := false
	if p.Decision != nil {
		result["decision"] = p.Decision
		hasResult = true
	}
	if p.Approved != nil {
		result["approved"] = *p.Approved
		hasResult = true
	}
	return result, hasResult
}

func approvalRespondTyped(s *Server, _ context.Context, p approvalRespondParams) (any, error) {
	if s == nil {
		return map[string]any{
			"ok":     false,
			"status": "server_not_ready",
		}, nil
	}
	if p.RequestID <= 0 {
		return map[string]any{
			"ok":     false,
			"status": "invalid_request_id",
		}, nil
	}

	result, ok := approvalRespondResultPayload(p)
	if !ok {
		return map[string]any{
			"ok":     false,
			"status": "decision_or_approved_required",
		}, nil
	}

	if !ResolvePendingRequest(s, p.RequestID, result) {
		return map[string]any{
			"ok":     false,
			"status": "not_pending",
		}, nil
	}

	return map[string]any{
		"ok":     true,
		"status": "resolved",
	}, nil
}

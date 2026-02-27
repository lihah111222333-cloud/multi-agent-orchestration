// server_approval.go — 审批事件处理: Server→Client 请求 → 等回复 → 回传 codex。
package apiserver

import (
	"context"
	"fmt"
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

// ── 审批决策别名表 (表驱动) ──

// approvalStringAliases 将各种用户输入别名归一化为标准决策字符串。
var approvalStringAliases = map[string]string{
	// accept 家族
	"accept": "accept", "approved": "accept", "approve": "accept",
	"yes": "accept", "y": "accept", "true": "accept", "1": "accept",
	// acceptForSession 家族
	"acceptforsession": "acceptForSession", "accept_for_session": "acceptForSession", "accept-for-session": "acceptForSession",
	// decline 家族
	"decline": "decline", "denied": "decline", "reject": "decline", "rejected": "decline",
	"no": "decline", "n": "decline", "false": "decline", "0": "decline",
	// cancel 家族
	"cancel": "cancel", "abort": "cancel", "aborted": "cancel",
}

// approvalMethodAcceptWord 每种审批方法的 "approve" 决策词。
var approvalMethodAcceptWord = map[string]string{
	approvalMethodCommandExecution: "accept",
	approvalMethodFileChange:       "accept",
	approvalMethodSkillRequest:     "approve",
}

// approvalMethodValidDecisions 每种审批方法允许的字符串决策集合。
var approvalMethodValidDecisions = map[string]map[string]bool{
	approvalMethodCommandExecution: {"accept": true, "acceptForSession": true, "decline": true, "cancel": true},
	approvalMethodFileChange:       {"accept": true, "acceptForSession": true, "decline": true, "cancel": true},
	approvalMethodSkillRequest:     {"approve": true, "decline": true},
}

// approvalMethodAllowsSubmit 每种审批方法视为 "通过" 的字符串决策集合。
var approvalMethodAllowsSubmit = map[string]map[string]bool{
	approvalMethodCommandExecution: {"accept": true, "acceptForSession": true},
	approvalMethodFileChange:       {"accept": true, "acceptForSession": true},
	approvalMethodSkillRequest:     {"approve": true},
}

func normalizeApprovalDecisionString(raw string) (string, bool) {
	normalized, ok := approvalStringAliases[strings.ToLower(strings.TrimSpace(raw))]
	return normalized, ok
}

// normalizeStringDecisionForMethod 对字符串决策做方法级归一化:
// skill 方法将 "accept" 重映射为 "approve"，其他方法保持不变。
func normalizeStringDecisionForMethod(method, decision string) (string, bool) {
	// skill 方法: accept → approve
	if method == approvalMethodSkillRequest && decision == "accept" {
		decision = "approve"
	}
	valid := approvalMethodValidDecisions[method]
	if valid == nil {
		// 未知方法: 宽松接受所有标准决策
		return decision, decision != ""
	}
	return decision, valid[decision]
}

// normalizeCommandAmendmentDecision 处理 command 审批特有的嵌套 amendment 对象。
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

func normalizeApprovalDecision(method string, raw any) (any, bool) {
	method = strings.TrimSpace(method)

	// 字符串路径: 查表归一化
	if decision, ok := normalizeApprovalDecisionString(approvalStringValue(raw)); ok {
		decision, ok = normalizeStringDecisionForMethod(method, decision)
		if ok {
			return decision, true
		}
	}

	// 嵌套对象路径: 仅 command 审批支持 amendment 对象
	if decisionObj, ok := raw.(map[string]any); ok && decisionObj != nil {
		if method == approvalMethodCommandExecution || method == "" {
			return normalizeCommandAmendmentDecision(decisionObj)
		}
	}

	// 未知方法 fallback: 依次尝试 command → fileChange → skill
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

func approvalDecisionPayload(method string, approved bool) map[string]any {
	decision := "decline"
	if approved {
		if word, ok := approvalMethodAcceptWord[strings.TrimSpace(method)]; ok {
			decision = word
		} else {
			decision = "accept"
		}
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

	// 嵌套对象路径: command 审批的 amendment 判断
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

	// 字符串路径: 查 allowsSubmit 表
	decision, ok := normalizeApprovalDecision(method, decisionRaw)
	if !ok {
		return false
	}
	decisionStr, _ := decision.(string)
	allowSet := approvalMethodAllowsSubmit[method]
	if allowSet == nil {
		// 未知方法: 默认用 command 规则
		allowSet = approvalMethodAllowsSubmit[approvalMethodCommandExecution]
	}
	return allowSet[decisionStr]
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

	// Fix 1: 子 agent 自动审批 — 多 agent 编排场景中，子 agent 不应阻塞等待人类审批。
	// 双重检查:
	//   (1) AgentManager 计数 — 多 agent 且非首个 → 视为子 agent (立即可用，无时序问题)
	//   (2) uiRuntime.IsMainAgent — 已有明确标记时使用
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

	// L1 诊断日志: 无论是否为子 agent，都记录审批事件到达时的上下文
	logger.Info("app-server: approval request received",
		logger.FieldAgentID, agentID,
		logger.FieldMethod, method,
		"agent_count", agentCount,
		"first_agent_id", firstID,
		"is_sub_agent", isSubAgent,
		"detected_by", detectedBy,
	)

	if isSubAgent {
		logger.Info("app-server: sub-agent auto-approved",
			logger.FieldAgentID, agentID,
			logger.FieldMethod, method,
			"agent_count", agentCount,
			"first_agent_id", firstID,
			"detected_by", detectedBy,
		)
		autoApprovePayload := approvalDecisionPayload(method, true)
		if event.RespondResultFunc != nil {
			if err := event.RespondResultFunc(autoApprovePayload); err != nil {
				logger.Warn("app-server: sub-agent auto-approve respond failed",
					logger.FieldAgentID, agentID, logger.FieldMethod, method, logger.FieldError, err)
				denyApprovalSafely(event, agentID)
			}
		} else if s.mgr != nil {
			if proc := s.mgr.Get(agentID); proc != nil {
				_ = s.codexAdapter.Submit(proc, "yes", nil, nil, nil)
			}
		}
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

// server_approval.go — 审批事件处理: Server→Client 请求 → 等回复 → 回传 codex。
package apiserver

import (
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

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

// extractFirstString 从 payload 中按优先级提取第一个非空字符串字段。
func extractFirstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := payload[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// handleApprovalRequest 处理审批事件: 双通道模式。
//
// 优先尝试 WebSocket (SendRequestToAll) — 适用于 IDE 客户端。
// 若无 WebSocket 连接, 降级为 Wails 模式:
//  1. AllocPendingRequest 分配 pending ID
//  2. broadcastNotification 推送审批请求 (→ notifyHook → Wails Event → 前端)
//  3. 等待前端 CallAPI("approval/respond") → ResolvePendingRequest 写入 channel
func (s *Server) handleApprovalRequest(agentID, method string, payload map[string]any, event codex.Event) {
	// 去重: 同一 agentID+method 正在处理中 → 跳过重复调用
	inflightKey := agentID + ":" + method
	if _, loaded := s.approvalInFlight.LoadOrStore(inflightKey, struct{}{}); loaded {
		logger.Debug("app-server: approval dedup — skipping duplicate in-flight request",
			logger.FieldAgentID, agentID, logger.FieldMethod, method)
		return
	}
	defer s.approvalInFlight.Delete(inflightKey)

	// 心跳: 防止 stall 检测在等待审批期间误杀
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	hbInterval := s.stallThreshold / 6
	if hbInterval <= 0 {
		hbInterval = defaultStallThreshold / 6
	}
	if hbInterval < 10*time.Second {
		hbInterval = 10 * time.Second
	}
	util.SafeGo(func() {
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.touchTrackedTurnLastEvent(agentID)
			case <-heartbeatDone:
				return
			}
		}
	})

	approved := false

	// 尝试 WebSocket 通道 (IDE 客户端)
	resp, wsErr := s.SendRequestToAll(method, payload)
	if wsErr == nil && resp != nil && resp.Result != nil {
		// WebSocket 客户端已回复
		if m, ok := resp.Result.(map[string]any); ok {
			if v, ok := m["approved"]; ok {
				approved, _ = v.(bool)
			}
		}
	} else {
		// 降级: Wails 模式 — 通过 broadcastNotification + pending channel
		// 仅在有 notifyHook (Wails 前端) 时才等待, 否则直接跳过 (approved=false → deny)
		s.notifyHookMu.RLock()
		hasHook := s.notifyHook != nil
		s.notifyHookMu.RUnlock()

		if hasHook {
			logger.Info("app-server: approval via Wails mode (no WS client)",
				logger.FieldAgentID, agentID, logger.FieldMethod, method)

			reqID, ch, cleanup := s.AllocPendingRequest()
			defer cleanup()

			// 注入 requestId, 前端据此回复
			if payload == nil {
				payload = make(map[string]any)
			}
			payload["requestId"] = reqID

			// 推送审批请求到前端 (→ notifyHook → Wails Event)
			s.broadcastNotification(method, payload)

			// 等待前端回复 (5 分钟超时)
			timer := time.NewTimer(5 * time.Minute)
			defer timer.Stop()
			select {
			case wailsResp := <-ch:
				if wailsResp != nil && wailsResp.Result != nil {
					if m, ok := wailsResp.Result.(map[string]any); ok {
						if v, ok := m["approved"]; ok {
							approved, _ = v.(bool)
						}
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

	// 回传给 codex agent
	if s.mgr == nil {
		logger.Error("app-server: approval auto-denied — mgr is nil",
			logger.FieldAgentID, agentID, logger.FieldMethod, method)
		if event.DenyFunc != nil {
			if denyErr := event.DenyFunc(); denyErr != nil {
				logger.Warn("app-server: deny callback failed", logger.FieldAgentID, agentID, logger.FieldError, denyErr)
			}
		}
		return
	}
	proc := s.mgr.Get(agentID)
	if proc == nil {
		logger.Error("app-server: approval auto-denied — agent gone",
			logger.FieldAgentID, agentID, logger.FieldMethod, method)
		if event.DenyFunc != nil {
			if denyErr := event.DenyFunc(); denyErr != nil {
				logger.Warn("app-server: deny callback failed", logger.FieldAgentID, agentID, logger.FieldError, denyErr)
			}
		}
		return
	}
	decision := "no"
	if approved {
		decision = "yes"
	}
	if err := proc.Client.Submit(decision, nil, nil, nil); err != nil {
		logger.Warn("app-server: relay approval to codex failed", logger.FieldAgentID, agentID, logger.FieldError, err)
	}
}

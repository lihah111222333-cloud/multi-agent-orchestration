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

// handleApprovalRequest 处理审批事件: Server→Client 请求 → 等回复 → 回传 codex。
func (s *Server) handleApprovalRequest(agentID, method string, payload map[string]any, event codex.Event) {
	// 心跳: 防止 stall 检测在等待审批期间误杀
	// 使用 stallThreshold/6 而非 stallHeartbeat，确保在 stall 阈值内多次 touch。
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
	resp, err := s.SendRequestToAll(method, payload)
	if err != nil {
		logger.Warn("app-server: approval request failed", logger.FieldAgentID, agentID, logger.FieldError, err)
	} else if resp != nil && resp.Result != nil {
		if m, ok := resp.Result.(map[string]any); ok {
			if v, ok := m["approved"]; ok {
				approved, _ = v.(bool)
			}
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

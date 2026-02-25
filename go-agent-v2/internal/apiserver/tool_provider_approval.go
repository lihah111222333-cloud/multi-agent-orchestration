package apiserver

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// codeRunApprovalNonce 用于生成审批 ID (code_run 执行审批)。
var codeRunApprovalNonce atomic.Int64

type approvalProvider struct {
	s *Server
}

func (p approvalProvider) AwaitApproval(agentID, callID, mode, command string, isDangerous bool) bool {
	if p.s == nil {
		return false
	}
	const method = "item/commandExecution/requestApproval"

	approvalID := callID
	if approvalID == "" {
		approvalID = fmt.Sprintf("coderun-%d", codeRunApprovalNonce.Add(1))
	}

	inflightKey := agentID + ":" + method + ":" + approvalID
	if !p.s.runtimeGuardState.tryBeginApproval(inflightKey) {
		logger.Debug("code-run: approval dedup — skipping",
			logger.FieldAgentID, agentID, logger.FieldCallID, callID)
		return false
	}
	defer p.s.runtimeGuardState.endApproval(inflightKey)

	payload := map[string]any{
		"type":         "code_run_approval",
		"agent_id":     agentID,
		"mode":         mode,
		"command":      executor.TruncateForAudit(command, 2048),
		"is_dangerous": isDangerous,
	}

	return p.waitForFrontendDecision(method, payload)
}

func (p approvalProvider) waitForFrontendDecision(method string, payload map[string]any) bool {
	resp, wsErr := sendRequestToAll(p.s, method, payload)
	if wsErr == nil && resp != nil && resp.Result != nil {
		if m, ok := resp.Result.(map[string]any); ok {
			if approved, ok := m["approved"].(bool); ok {
				return approved
			}
		}
	}

	hasHook := p.s.notifyHookState.hasHook()

	if !hasHook {
		logger.Warn("code-run: approval auto-denied — no frontend", "method", method)
		return false
	}

	reqID, ch, cleanup := allocPendingRequest(p.s)
	defer cleanup()

	if payload == nil {
		payload = make(map[string]any)
	}
	payload["requestId"] = reqID
	broadcastNotification(p.s, method, payload)

	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()

	select {
	case wailsResp := <-ch:
		if wailsResp != nil && wailsResp.Result != nil {
			if m, ok := wailsResp.Result.(map[string]any); ok {
				if approved, ok := m["approved"].(bool); ok {
					return approved
				}
			}
		}
	case <-timer.C:
		logger.Warn("code-run: approval timed out", "method", method)
	}
	return false
}

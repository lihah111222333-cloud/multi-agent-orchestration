// code_run_tools.go — 代码执行运行时状态与审批（生命周期层）。
package apiserver

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/tools"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (s *Server) setAgentWorkDir(agentID, cwd string) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return
	}
	normalized := tools.NormalizeAgentWorkDir(cwd)
	if normalized == "" {
		return
	}
	s.agentWorkDirMu.Lock()
	if s.agentWorkDirs == nil {
		s.agentWorkDirs = make(map[string]string)
	}
	s.agentWorkDirs[id] = normalized
	s.agentWorkDirMu.Unlock()
}

func (s *Server) getAgentWorkDir(agentID string) string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return ""
	}
	s.agentWorkDirMu.RLock()
	cwd := s.agentWorkDirs[id]
	s.agentWorkDirMu.RUnlock()
	return cwd
}

func (s *Server) clearAgentWorkDir(agentID string) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return
	}
	s.agentWorkDirMu.Lock()
	delete(s.agentWorkDirs, id)
	s.agentWorkDirMu.Unlock()
}

func (s *Server) SetAgentWorkDir(agentID, cwd string) {
	s.setAgentWorkDir(agentID, cwd)
}

func (s *Server) GetAgentWorkDir(agentID string) string {
	return s.getAgentWorkDir(agentID)
}

func (s *Server) ClearAgentWorkDir(agentID string) {
	s.clearAgentWorkDir(agentID)
}

// registerCodeRunCancel 注册运行中的 code_run 取消函数, 返回本次 runKey。
func (s *Server) registerCodeRunCancel(agentID, callID string, cancel context.CancelFunc) string {
	if cancel == nil {
		return ""
	}
	id := strings.TrimSpace(agentID)
	if id == "" {
		return ""
	}
	seq := s.codeRunSeq.Add(1)
	key := fmt.Sprintf("%s#%d", strings.TrimSpace(callID), seq)

	s.codeRunMu.Lock()
	if s.activeCodeRuns == nil {
		s.activeCodeRuns = make(map[string]map[string]context.CancelFunc)
	}
	runs := s.activeCodeRuns[id]
	if runs == nil {
		runs = make(map[string]context.CancelFunc)
		s.activeCodeRuns[id] = runs
	}
	runs[key] = cancel
	s.codeRunMu.Unlock()
	return key
}

func (s *Server) unregisterCodeRunCancel(agentID, runKey string) {
	id := strings.TrimSpace(agentID)
	key := strings.TrimSpace(runKey)
	if id == "" || key == "" {
		return
	}
	s.codeRunMu.Lock()
	if runs := s.activeCodeRuns[id]; runs != nil {
		delete(runs, key)
		if len(runs) == 0 {
			delete(s.activeCodeRuns, id)
		}
	}
	s.codeRunMu.Unlock()
}

// cancelCodeRuns 取消指定 agent 当前所有 code_run/code_run_test 执行。
func (s *Server) cancelCodeRuns(agentID string) int {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return 0
	}
	s.codeRunMu.Lock()
	runs := s.activeCodeRuns[id]
	delete(s.activeCodeRuns, id)
	s.codeRunMu.Unlock()
	if len(runs) == 0 {
		return 0
	}
	for _, cancel := range runs {
		cancel()
	}
	return len(runs)
}

func (s *Server) CancelCodeRuns(agentID string) int {
	return s.cancelCodeRuns(agentID)
}

// cancelAllCodeRuns 取消所有 agent 的 code_run/code_run_test 执行。
func (s *Server) cancelAllCodeRuns() int {
	s.codeRunMu.Lock()
	all := s.activeCodeRuns
	s.activeCodeRuns = make(map[string]map[string]context.CancelFunc)
	s.codeRunMu.Unlock()

	total := 0
	for _, runs := range all {
		for _, cancel := range runs {
			cancel()
			total++
		}
	}
	return total
}

var codeRunApprovalNonce atomic.Int64

func (s *Server) awaitCodeRunApproval(agentID, callID, mode, command string, isDangerous bool) bool {
	const method = "item/commandExecution/requestApproval"

	approvalID := callID
	if approvalID == "" {
		approvalID = fmt.Sprintf("coderun-%d", codeRunApprovalNonce.Add(1))
	}

	inflightKey := agentID + ":" + method + ":" + approvalID
	if _, loaded := s.approvalInFlight.LoadOrStore(inflightKey, struct{}{}); loaded {
		logger.Debug("code-run: approval dedup — skipping",
			logger.FieldAgentID, agentID, logger.FieldCallID, callID)
		return false
	}
	defer s.approvalInFlight.Delete(inflightKey)

	payload := map[string]any{
		"type":         "code_run_approval",
		"agent_id":     agentID,
		"mode":         mode,
		"command":      executor.TruncateForAudit(command, 2048),
		"is_dangerous": isDangerous,
	}

	return s.waitForFrontendDecision(method, payload)
}

func (s *Server) AwaitApproval(agentID, callID, mode, command string, isDangerous bool) bool {
	return s.awaitCodeRunApproval(agentID, callID, mode, command, isDangerous)
}

func (s *Server) waitForFrontendDecision(method string, payload map[string]any) bool {
	resp, wsErr := s.SendRequestToAll(method, payload)
	if wsErr == nil && resp != nil && resp.Result != nil {
		if m, ok := resp.Result.(map[string]any); ok {
			if approved, ok := m["approved"].(bool); ok {
				return approved
			}
		}
	}

	s.notifyHookMu.RLock()
	hasHook := s.notifyHook != nil
	s.notifyHookMu.RUnlock()

	if !hasHook {
		logger.Warn("code-run: approval auto-denied — no frontend", "method", method)
		return false
	}

	reqID, ch, cleanup := s.AllocPendingRequest()
	defer cleanup()

	if payload == nil {
		payload = make(map[string]any)
	}
	payload["requestId"] = reqID
	s.broadcastNotification(method, payload)

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

func (s *Server) CodeRunner() *executor.CodeRunner {
	return s.codeRunner
}

func (s *Server) AuditLogStore() *store.AuditLogStore {
	return s.auditLogStore
}

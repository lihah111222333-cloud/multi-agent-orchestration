package apiserver

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tooladapter"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
)

func (s *Server) RegisterRuntimeTool(name string, handler tooladapter.RuntimeToolHandler) {
	setRuntimeTool(s, name, handler)
}

func (s *Server) LookupRuntimeTool(name string) (tooladapter.RuntimeToolHandler, bool) {
	return lookupRuntimeTool(s, name)
}

func (s *Server) IncrementToolCall(name string) int64 {
	return incrementToolCallState(s, name)
}

func (s *Server) RegisterCodeRunCancel(agentID, callID string, cancel context.CancelFunc) string {
	return registerCodeRunCancelState(s, agentID, callID, cancel)
}

func (s *Server) UnregisterCodeRunCancel(agentID, runKey string) {
	unregisterCodeRunCancelState(s, agentID, runKey)
}

func (s *Server) CodeRunner() tools.CodeExecRunner {
	if s == nil {
		return nil
	}
	return adaptCodeExecRunner(s.codeRunner)
}

func (s *Server) AuditLogger() tools.AuditLogger {
	if s == nil {
		return nil
	}
	return adaptAuditLogger(s.auditLogStore)
}

func (s *Server) DAGManager() tools.DAGManager {
	if s == nil {
		return nil
	}
	return adaptDAGManager(s.dagStore)
}

func (s *Server) CommandCardStore() tools.CardStore {
	if s == nil {
		return nil
	}
	return adaptCardStore(s.cmdStore)
}

func (s *Server) PromptTemplateStore() tools.TemplateStore {
	if s == nil {
		return nil
	}
	return adaptTemplateStore(s.promptStore)
}

func (s *Server) SharedFileStore() tools.FileStore {
	if s == nil {
		return nil
	}
	return adaptFileStore(s.fileStore)
}

func (s *Server) WorkspaceOps() tools.WorkspaceOps {
	if s == nil {
		return nil
	}
	return adaptWorkspaceOps(s.workspaceMgr)
}

func (s *Server) NotifyEvent(method string, params any) {
	if s == nil {
		return
	}
	notify(s, method, params)
}

func (s *Server) AgentLauncher() tools.AgentLauncher {
	if s == nil {
		return nil
	}
	return adaptAgentLauncher(s.mgr)
}

func (s *Server) SubmitPrompt(agentID, prompt string, images, files []string) error {
	return submitPrompt(s, agentID, prompt, images, files)
}

func (s *Server) RememberReportRequest(senderID, workerID string) {
	rememberReportRequest(s, senderID, workerID)
}

func (s *Server) NextThreadSeq() int64 {
	return nextThreadSeqState(s)
}

func (s *Server) SaveSubAgent(id, name, cwd string) {
	if s == nil || s.agentThreadStore == nil {
		return
	}
	now := time.Now().UnixMilli()
	if err := s.agentThreadStore.Upsert(context.Background(), store.AgentThread{
		ThreadID: id, Prompt: name, Cwd: cwd, Status: "running",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		logger.Warn("orchestration: save sub-agent failed", logger.FieldAgentID, id, logger.FieldError, err)
	}
}

func (s *Server) DeleteSubAgent(id string) {
	if s == nil || s.agentThreadStore == nil {
		return
	}
	if err := s.agentThreadStore.Delete(context.Background(), id); err != nil {
		logger.Warn("orchestration: delete sub-agent failed", logger.FieldAgentID, id, logger.FieldError, err)
	}
}

func (s *Server) CancelCodeRuns(agentID string) int {
	return cancelCodeRunsState(s, agentID)
}

func (s *Server) SetAgentWorkDir(agentID, cwd string) {
	setAgentWorkDirState(s, agentID, cwd)
}

func (s *Server) ClearAgentWorkDir(agentID string) {
	clearAgentWorkDirState(s, agentID)
}

func (s *Server) GetAgentWorkDir(agentID string) string {
	return getAgentWorkDirState(s, agentID)
}

func (s *Server) AllSchemas() []agentcore.DynamicTool {
	if s == nil {
		return nil
	}
	return tooladapter.AllSchemas(toolAdapterProviders(s))
}

func setRuntimeTool(s *Server, name string, handler tooladapter.RuntimeToolHandler) {
	if s == nil || strings.TrimSpace(name) == "" || handler == nil {
		return
	}
	if s.dynTools == nil {
		s.dynTools = make(map[string]tooladapter.RuntimeToolHandler)
	}
	tooladapter.SetRuntimeTool(s.dynTools, name, handler)
}

func lookupRuntimeTool(s *Server, name string) (tooladapter.RuntimeToolHandler, bool) {
	if s == nil || s.dynTools == nil {
		return nil, false
	}
	return tooladapter.GetRuntimeTool(s.dynTools, name)
}

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
	if !tryBeginApprovalState(p.s, inflightKey) {
		logger.Debug("code-run: approval dedup — skipping",
			logger.FieldAgentID, agentID, logger.FieldCallID, callID)
		return false
	}
	defer endApprovalState(p.s, inflightKey)

	payload := map[string]any{
		"threadId":     agentID,
		"type":         "code_run_approval",
		"agent_id":     agentID,
		"mode":         mode,
		"command":      executor.TruncateForAudit(command, 2048),
		"is_dangerous": isDangerous,
	}

	return p.waitForFrontendDecision(agentID, method, payload)
}

func (p approvalProvider) waitForFrontendDecision(agentID, method string, payload map[string]any) bool {
	resp, wsErr := sendRequestToAll(p.s, method, payload)
	if wsErr == nil && resp != nil && resp.Result != nil {
		if m, ok := resp.Result.(map[string]any); ok {
			if approved, ok := m["approved"].(bool); ok {
				return approved
			}
		}
	}

	hasHook := hasNotifyHookState(p.s)
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

	// 回灌到 uiRuntime，确保 timeline 审批卡拿到 requestId 可交互。
	if p.s.uiRuntime != nil {
		threadID := strings.TrimSpace(agentID)
		if threadID != "" {
			normalized := uistate.NormalizeEventFromPayload(method, method, payload)
			p.s.uiRuntime.ApplyAgentEvent(threadID, normalized, payload)
			throttledUIStateChanged(p.s, map[string]any{
				"source":   method,
				"threadId": threadID,
			})
		}
	}

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

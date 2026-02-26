package apiserver

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/tooladapter"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type runtimeRegistryProvider struct {
	s *Server
}

func (p runtimeRegistryProvider) RegisterRuntimeTool(name string, handler tooladapter.RuntimeToolHandler) {
	setRuntimeTool(p.s, name, handler)
}

type runtimeLookupProvider struct {
	s *Server
}

func (p runtimeLookupProvider) LookupRuntimeTool(name string) (tooladapter.RuntimeToolHandler, bool) {
	return lookupRuntimeTool(p.s, name)
}

type toolCallCounterProvider struct {
	s *Server
}

func (p toolCallCounterProvider) IncrementToolCall(name string) int64 {
	return incrementToolCall(p.s, name)
}

type codeRunTrackerProvider struct {
	s *Server
}

func (p codeRunTrackerProvider) RegisterCodeRunCancel(agentID, callID string, cancel context.CancelFunc) string {
	return registerCodeRunCancelState(p.s, agentID, callID, cancel)
}

func (p codeRunTrackerProvider) UnregisterCodeRunCancel(agentID, runKey string) {
	unregisterCodeRunCancelState(p.s, agentID, runKey)
}

type codeRunProvider struct {
	s *Server
}

func (p codeRunProvider) CodeRunner() *executor.CodeRunner {
	if p.s == nil {
		return nil
	}
	return p.s.codeRunner
}

func (p codeRunProvider) AuditLogStore() *store.AuditLogStore {
	if p.s == nil {
		return nil
	}
	return p.s.auditLogStore
}

type resourceProvider struct {
	s *Server
}

func (p resourceProvider) DAGStore() *store.TaskDAGStore {
	if p.s == nil {
		return nil
	}
	return p.s.dagStore
}

func (p resourceProvider) CommandCardStore() *store.CommandCardStore {
	if p.s == nil {
		return nil
	}
	return p.s.cmdStore
}

func (p resourceProvider) PromptTemplateStore() *store.PromptTemplateStore {
	if p.s == nil {
		return nil
	}
	return p.s.promptStore
}

func (p resourceProvider) SharedFileStore() *store.SharedFileStore {
	if p.s == nil {
		return nil
	}
	return p.s.fileStore
}

func (p resourceProvider) WorkspaceManager() *service.WorkspaceManager {
	if p.s == nil {
		return nil
	}
	return p.s.workspaceMgr
}

func (p resourceProvider) NotifyEvent(method string, params any) {
	if p.s == nil {
		return
	}
	notify(p.s, method, params)
}

type orchestrationProvider struct {
	s *Server
}

func (p orchestrationProvider) Manager() *runner.AgentManager {
	if p.s == nil {
		return nil
	}
	return p.s.mgr
}

func (p orchestrationProvider) WorkspaceManager() *service.WorkspaceManager {
	if p.s == nil {
		return nil
	}
	return p.s.workspaceMgr
}

func (p orchestrationProvider) SubmitPrompt(agentID, prompt string, images, files []string) error {
	return submitPrompt(p.s, agentID, prompt, images, files)
}

func (p orchestrationProvider) RememberReportRequest(senderID, workerID string) {
	rememberReportRequest(p.s, senderID, workerID)
}

func (p orchestrationProvider) NextThreadSeq() int64 {
	return nextThreadSeq(p.s)
}

type agentRuntimeProvider struct {
	s *Server
}

func (p agentRuntimeProvider) CancelCodeRuns(agentID string) int {
	return cancelCodeRunsState(p.s, agentID)
}

func (p agentRuntimeProvider) SetAgentWorkDir(agentID, cwd string) {
	setAgentWorkDirState(p.s, agentID, cwd)
}

func (p agentRuntimeProvider) ClearAgentWorkDir(agentID string) {
	clearAgentWorkDirState(p.s, agentID)
}

func (p agentRuntimeProvider) GetAgentWorkDir(agentID string) string {
	return getAgentWorkDirState(p.s, agentID)
}

type schemaProvider struct {
	s *Server
}

func (p schemaProvider) AllSchemas() []agentcore.DynamicTool {
	return allSchemas(p.s)
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

func incrementToolCall(s *Server, name string) int64 {
	if s == nil {
		return 0
	}
	return incrementToolCallState(s, name)
}

func allSchemas(s *Server) []agentcore.DynamicTool {
	if s == nil {
		return nil
	}
	return tooladapter.AllSchemas(toolAdapterProviders(s))
}

func nextThreadSeq(s *Server) int64 {
	if s == nil {
		return 0
	}
	return nextThreadSeqState(s)
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

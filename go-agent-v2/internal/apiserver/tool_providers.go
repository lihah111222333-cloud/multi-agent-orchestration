// tool_providers.go — tools 包所需的 Provider 接口适配层。
//
// 将 Server 内部状态桥接到 tools 包定义的 Provider 接口,
// 使 tools 包不直接依赖 apiserver.Server。
package apiserver

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/tooladapter"
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
	if p.s == nil {
		return ""
	}
	return p.s.codeRunState.registerCodeRunCancel(agentID, callID, cancel)
}

func (p codeRunTrackerProvider) UnregisterCodeRunCancel(agentID, runKey string) {
	if p.s == nil {
		return
	}
	p.s.codeRunState.unregisterCodeRunCancel(agentID, runKey)
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
	if p.s == nil {
		return 0
	}
	return p.s.codeRunState.cancelCodeRuns(agentID)
}

func (p agentRuntimeProvider) SetAgentWorkDir(agentID, cwd string) {
	if p.s == nil {
		return
	}
	p.s.codeRunState.setAgentWorkDir(agentID, cwd)
}

func (p agentRuntimeProvider) ClearAgentWorkDir(agentID string) {
	if p.s == nil {
		return
	}
	p.s.codeRunState.clearAgentWorkDir(agentID)
}

func (p agentRuntimeProvider) GetAgentWorkDir(agentID string) string {
	if p.s == nil {
		return ""
	}
	return p.s.codeRunState.getAgentWorkDir(agentID)
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

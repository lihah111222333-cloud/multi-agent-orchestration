// resource_tools.go — 资源类动态工具 apiserver 薄委派。
package apiserver

import (
	"encoding/json"
	"fmt"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/tools"
)

func (s *Server) resourceToolset() []tools.Tool {
	return tools.ResourceTools(s)
}

func (s *Server) resourceToolSchemas() []agentcore.DynamicTool {
	return tools.Schemas(s.resourceToolset())
}

func (s *Server) runResourceTool(name string, args json.RawMessage) string {
	tool, ok := tools.FindTool(s.resourceToolset(), name)
	if !ok || tool.Handler == nil {
		return tools.ToolError(fmt.Errorf("resource tool %s unavailable", name))
	}
	return tool.Handler(tools.ToolCallContext{}, args)
}

func (s *Server) resourceTaskCreateDAG(args json.RawMessage) string {
	return s.runResourceTool("task_create_dag", args)
}

func (s *Server) resourceTaskGetDAG(args json.RawMessage) string {
	return s.runResourceTool("task_get_dag", args)
}

func (s *Server) resourceTaskUpdateNode(args json.RawMessage) string {
	return s.runResourceTool("task_update_node", args)
}

func (s *Server) resourceCommandList(args json.RawMessage) string {
	return s.runResourceTool("command_list", args)
}

func (s *Server) resourceCommandGet(args json.RawMessage) string {
	return s.runResourceTool("command_get", args)
}

func (s *Server) resourcePromptList(args json.RawMessage) string {
	return s.runResourceTool("prompt_list", args)
}

func (s *Server) resourcePromptGet(args json.RawMessage) string {
	return s.runResourceTool("prompt_get", args)
}

func (s *Server) resourceSharedFileRead(args json.RawMessage) string {
	return s.runResourceTool("shared_file_read", args)
}

func (s *Server) resourceSharedFileWrite(args json.RawMessage) string {
	return s.runResourceTool("shared_file_write", args)
}

func (s *Server) resourceWorkspaceCreateRun(args json.RawMessage) string {
	return s.runResourceTool("workspace_create_run", args)
}

func (s *Server) resourceWorkspaceGetRun(args json.RawMessage) string {
	return s.runResourceTool("workspace_get_run", args)
}

func (s *Server) resourceWorkspaceListRuns(args json.RawMessage) string {
	return s.runResourceTool("workspace_list_runs", args)
}

func (s *Server) resourceWorkspaceMergeRun(args json.RawMessage) string {
	return s.runResourceTool("workspace_merge_run", args)
}

func (s *Server) resourceWorkspaceAbortRun(args json.RawMessage) string {
	return s.runResourceTool("workspace_abort_run", args)
}

func (s *Server) DAGStore() *store.TaskDAGStore {
	return s.dagStore
}

func (s *Server) CommandCardStore() *store.CommandCardStore {
	return s.cmdStore
}

func (s *Server) PromptTemplateStore() *store.PromptTemplateStore {
	return s.promptStore
}

func (s *Server) SharedFileStore() *store.SharedFileStore {
	return s.fileStore
}

func (s *Server) WorkspaceManager() *service.WorkspaceManager {
	return s.workspaceMgr
}

func (s *Server) NotifyEvent(method string, params any) {
	s.Notify(method, params)
}

// resource_tools.go — 资源类工具 Provider 实现（生命周期层）。
package apiserver

import (
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

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

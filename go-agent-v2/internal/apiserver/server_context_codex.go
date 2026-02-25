package apiserver

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type codexAdapterHooks struct {
	server *Server
}

func (h codexAdapterHooks) readSkillContent(skillName string) (string, error) {
	if h.server == nil || h.server.skillSvc == nil {
		return "", pkgerr.New("Server.skillService", "skill service is not initialized")
	}
	return h.server.skillSvc.ReadSkillContent(skillName)
}

func (h codexAdapterHooks) listSkillNames() ([]string, error) {
	if h.server == nil || h.server.skillSvc == nil {
		return []string{}, nil
	}
	list, err := h.server.skillSvc.ListSkills()
	if err != nil {
		return nil, err
	}
	skillNames := make([]string, 0, len(list))
	for _, item := range list {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		skillNames = append(skillNames, name)
	}
	return skillNames, nil
}

func (h codexAdapterHooks) listSkillMatchCandidates() ([]codexadapter.SkillMatchCandidate, error) {
	return listSkillMatchCandidates(h.server)
}

func (h codexAdapterHooks) getAgentSkills(agentID string) []string {
	return getAgentSkills(h.server, agentID)
}

func (h codexAdapterHooks) allSchemas() []agentcore.DynamicTool {
	return allSchemas(h.server)
}

func (h codexAdapterHooks) notify(method string, params any) {
	notify(h.server, method, params)
}

func (h codexAdapterHooks) setAgentWorkDir(agentID, cwd string) {
	setAgentWorkDirState(h.server, agentID, cwd)
}

func (h codexAdapterHooks) cancelCodeRuns(agentID string) int {
	return cancelCodeRunsState(h.server, agentID)
}

func newCodexAdapter(s *Server) *codexadapter.Adapter {
	if s == nil {
		return nil
	}
	hooks := codexAdapterHooks{server: s}
	return codexadapter.New(codexadapter.Deps{
		Manager:                  s.mgr,
		Store:                    s.prefManager,
		BindingStore:             s.bindingStore,
		AgentStatusStore:         s.agentStatusStore,
		UIRuntime:                s.uiRuntime,
		AllSchemas:               hooks.allSchemas,
		SetAgentWorkDir:          hooks.setAgentWorkDir,
		CancelCodeRuns:           hooks.cancelCodeRuns,
		ReadSkillContent:         hooks.readSkillContent,
		ListSkillNames:           hooks.listSkillNames,
		ListSkillMatchCandidates: hooks.listSkillMatchCandidates,
		GetAgentSkills:           hooks.getAgentSkills,
		Notify:                   hooks.notify,
	})
}

func registerCodeRunCancelState(s *Server, agentID, callID string, cancel context.CancelFunc) string {
	if s == nil {
		return ""
	}
	return s.codeRunState.registerCodeRunCancel(agentID, callID, cancel)
}

func unregisterCodeRunCancelState(s *Server, agentID, runKey string) {
	if s == nil {
		return
	}
	s.codeRunState.unregisterCodeRunCancel(agentID, runKey)
}

func cancelCodeRunsState(s *Server, agentID string) int {
	if s == nil {
		return 0
	}
	return s.codeRunState.cancelCodeRuns(agentID)
}

func setAgentWorkDirState(s *Server, agentID, cwd string) {
	if s == nil {
		return
	}
	s.codeRunState.setAgentWorkDir(agentID, cwd)
}

func clearAgentWorkDirState(s *Server, agentID string) {
	if s == nil {
		return
	}
	s.codeRunState.clearAgentWorkDir(agentID)
}

func getAgentWorkDirState(s *Server, agentID string) string {
	if s == nil {
		return ""
	}
	return s.codeRunState.getAgentWorkDir(agentID)
}

func cancelAllCodeRuns(s *Server) int {
	if s == nil {
		return 0
	}
	return s.codeRunState.cancelAllCodeRuns()
}

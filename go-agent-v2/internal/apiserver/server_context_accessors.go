package apiserver

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

func readSkillContent(s *Server, skillName string) (string, error) {
	if s == nil || s.skillSvc == nil {
		return "", pkgerr.New("Server.skillService", "skill service is not initialized")
	}
	return s.skillSvc.ReadSkillContent(skillName)
}

func listSkillNames(s *Server) ([]string, error) {
	if s == nil || s.skillSvc == nil {
		return []string{}, nil
	}
	list, err := s.skillSvc.ListSkills()
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

func newCodexAdapter(s *Server) *codexadapter.Adapter {
	if s == nil {
		return nil
	}
	return codexadapter.New(codexadapter.Deps{
		Manager: func() *runner.AgentManager {
			return s.mgr
		},
		Store: func() *uistate.PreferenceManager {
			return s.prefManager
		},
		BindingStore: func() *store.AgentCodexBindingStore {
			return s.bindingStore
		},
		AgentStatusStore: func() *store.AgentStatusStore {
			return s.agentStatusStore
		},
		UIRuntime: func() *uistate.RuntimeManager {
			return s.uiRuntime
		},
		AllSchemas: func() []agentcore.DynamicTool {
			return allSchemas(s)
		},
		SetAgentWorkDir: func(agentID, cwd string) {
			s.codeRunState.setAgentWorkDir(agentID, cwd)
		},
		CancelCodeRuns: func(agentID string) int {
			return s.codeRunState.cancelCodeRuns(agentID)
		},
		ReadSkillContent: func(skillName string) (string, error) {
			return readSkillContent(s, skillName)
		},
		ListSkillNames: func() ([]string, error) {
			return listSkillNames(s)
		},
		ListSkillMatchCandidates: func() ([]codexadapter.SkillMatchCandidate, error) {
			return listSkillMatchCandidates(s)
		},
		GetAgentSkills: func(agentID string) []string {
			return getAgentSkills(s, agentID)
		},
		Notify: func(method string, params any) {
			notify(s, method, params)
		},
	})
}

func cancelAllCodeRuns(s *Server) int {
	if s == nil {
		return 0
	}
	return s.codeRunState.cancelAllCodeRuns()
}

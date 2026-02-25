package apiserver

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/service"
	skillsruntime "github.com/multi-agent/go-agent-v2/internal/skills"
)

type skillsLocalReadParams = skillsruntime.SkillsLocalReadParams
type skillsLocalImportDirParams = skillsruntime.SkillsLocalImportDirParams
type skillsLocalDeleteParams = skillsruntime.SkillsLocalDeleteParams
type skillsConfigWriteParams = skillsruntime.SkillsConfigWriteParams
type skillsSummaryWriteParams = skillsruntime.SkillsSummaryWriteParams
type skillsMatchPreviewParams = skillsruntime.SkillsMatchPreviewParams
type skillsConfigReadParams = skillsruntime.SkillsConfigReadParams
type skillsRemoteReadParams = skillsruntime.SkillsRemoteReadParams
type skillsRemoteWriteParams = skillsruntime.SkillsRemoteWriteParams

func newSkillsManager(s *Server) *skillsruntime.Manager {
	if s == nil {
		return skillsruntime.NewManager(nil, nil)
	}

	provider := skillsruntime.SkillServiceProviderFunc(func() *service.SkillService {
		return s.skillSvc
	})
	collector := skillsruntime.AutoMatchCollectorFunc(func(
		threadID string,
		prompt string,
		input []skillsruntime.UserInput,
		options skillsruntime.AutoSkillMatchOptions,
	) []skillsruntime.AutoMatchedSkillMatch {
		if s == nil || s.codexAdapter == nil {
			return nil
		}
		turnInput := make([]codexadapter.TurnInput, 0, len(input))
		for _, item := range input {
			turnInput = append(turnInput, codexadapter.TurnInput{
				Type:    item.Type,
				Text:    item.Text,
				URL:     item.URL,
				Path:    item.Path,
				Name:    item.Name,
				Content: item.Content,
			})
		}
		matches := s.codexAdapter.CollectAutoMatchedSkillMatchesForThread(threadID, prompt, turnInput, codexadapter.AutoSkillMatchOptions{
			IncludeConfiguredExplicit: options.IncludeConfiguredExplicit,
			IncludeConfiguredForce:    options.IncludeConfiguredForce,
		})
		out := make([]skillsruntime.AutoMatchedSkillMatch, 0, len(matches))
		for _, match := range matches {
			out = append(out, skillsruntime.AutoMatchedSkillMatch{
				Name:         match.Name,
				MatchedBy:    match.MatchedBy,
				MatchedTerms: append([]string(nil), match.MatchedTerms...),
			})
		}
		return out
	})
	return skillsruntime.NewManager(provider, collector)
}

func skillsManagerDelegate(s *Server) *skillsruntime.Manager {
	if s != nil && s.skillsMgr != nil {
		return s.skillsMgr
	}
	return newSkillsManager(s)
}

func skillsList(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	return skillsManagerDelegate(s).SkillsList(ctx)
}

func appList(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	return skillsManagerDelegate(s).AppList(ctx)
}

func skillsLocalReadTyped(s *Server, ctx context.Context, p skillsLocalReadParams) (any, error) {
	return skillsManagerDelegate(s).SkillsLocalRead(ctx, skillsruntime.SkillsLocalReadParams(p))
}

func skillsLocalImportDirTyped(s *Server, ctx context.Context, p skillsLocalImportDirParams) (any, error) {
	return skillsManagerDelegate(s).SkillsLocalImportDir(ctx, skillsruntime.SkillsLocalImportDirParams(p))
}

func skillsLocalDeleteTyped(s *Server, ctx context.Context, p skillsLocalDeleteParams) (any, error) {
	return skillsManagerDelegate(s).SkillsLocalDelete(ctx, skillsruntime.SkillsLocalDeleteParams(p))
}

func skillsMatchPreviewTyped(s *Server, ctx context.Context, p skillsMatchPreviewParams) (any, error) {
	return skillsManagerDelegate(s).SkillsMatchPreview(ctx, skillsruntime.SkillsMatchPreviewParams(p))
}

func skillsConfigReadTyped(s *Server, ctx context.Context, p skillsConfigReadParams) (any, error) {
	return skillsManagerDelegate(s).SkillsConfigRead(ctx, skillsruntime.SkillsConfigReadParams(p))
}

func skillsConfigWriteTyped(s *Server, ctx context.Context, p skillsConfigWriteParams) (any, error) {
	return skillsManagerDelegate(s).SkillsConfigWrite(ctx, skillsruntime.SkillsConfigWriteParams(p))
}

func skillsSummaryWriteTyped(s *Server, ctx context.Context, p skillsSummaryWriteParams) (any, error) {
	return skillsManagerDelegate(s).SkillsSummaryWrite(ctx, skillsruntime.SkillsSummaryWriteParams(p))
}

func skillsRemoteReadTyped(s *Server, ctx context.Context, p skillsRemoteReadParams) (any, error) {
	return skillsManagerDelegate(s).SkillsRemoteRead(ctx, skillsruntime.SkillsRemoteReadParams(p))
}

func skillsRemoteWriteTyped(s *Server, ctx context.Context, p skillsRemoteWriteParams) (any, error) {
	return skillsManagerDelegate(s).SkillsRemoteWrite(ctx, skillsruntime.SkillsRemoteWriteParams(p))
}

// getAgentSkills 返回指定 agent 配置的技能列表。
func getAgentSkills(s *Server, agentID string) []string {
	return skillsManagerDelegate(s).GetAgentSkills(agentID)
}

// listSkillMatchCandidates returns normalized candidates for adapter auto-match.
func listSkillMatchCandidates(s *Server) ([]codexadapter.SkillMatchCandidate, error) {
	if s == nil || s.skillSvc == nil {
		return nil, nil
	}
	allSkills, err := s.skillSvc.ListSkills()
	if err != nil {
		return nil, err
	}
	candidates := make([]codexadapter.SkillMatchCandidate, 0, len(allSkills))
	for _, skill := range allSkills {
		skillName := strings.TrimSpace(skill.Name)
		if skillName == "" {
			continue
		}
		candidates = append(candidates, codexadapter.SkillMatchCandidate{
			Name:         skillName,
			ForceWords:   append([]string(nil), skill.ForceWords...),
			TriggerWords: append([]string(nil), skill.TriggerWords...),
		})
	}
	return candidates, nil
}

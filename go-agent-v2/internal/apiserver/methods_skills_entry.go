package apiserver

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
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
		legacyInput := make([]UserInput, 0, len(input))
		for _, item := range input {
			legacyInput = append(legacyInput, UserInput{
				Type:    item.Type,
				Text:    item.Text,
				URL:     item.URL,
				Path:    item.Path,
				Name:    item.Name,
				Content: item.Content,
			})
		}
		matches := s.collectAutoMatchedSkillMatches(threadID, prompt, legacyInput, contracts.AutoSkillMatchOptions{
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

func (s *Server) skillsManagerDelegate() *skillsruntime.Manager {
	if s != nil && s.skillsMgr != nil {
		return s.skillsMgr
	}
	return newSkillsManager(s)
}

func (s *Server) skillsList(ctx context.Context, _ json.RawMessage) (any, error) {
	return s.skillsManagerDelegate().SkillsList(ctx)
}

func (s *Server) appList(ctx context.Context, _ json.RawMessage) (any, error) {
	return s.skillsManagerDelegate().AppList(ctx)
}

func (s *Server) skillsLocalReadTyped(ctx context.Context, p skillsLocalReadParams) (any, error) {
	return s.skillsManagerDelegate().SkillsLocalRead(ctx, skillsruntime.SkillsLocalReadParams(p))
}

func (s *Server) skillsLocalImportDirTyped(ctx context.Context, p skillsLocalImportDirParams) (any, error) {
	return s.skillsManagerDelegate().SkillsLocalImportDir(ctx, skillsruntime.SkillsLocalImportDirParams(p))
}

func (s *Server) skillsLocalDeleteTyped(ctx context.Context, p skillsLocalDeleteParams) (any, error) {
	return s.skillsManagerDelegate().SkillsLocalDelete(ctx, skillsruntime.SkillsLocalDeleteParams(p))
}

func (s *Server) skillsMatchPreviewTyped(ctx context.Context, p skillsMatchPreviewParams) (any, error) {
	return s.skillsManagerDelegate().SkillsMatchPreview(ctx, skillsruntime.SkillsMatchPreviewParams(p))
}

func (s *Server) skillsConfigReadTyped(ctx context.Context, p skillsConfigReadParams) (any, error) {
	return s.skillsManagerDelegate().SkillsConfigRead(ctx, skillsruntime.SkillsConfigReadParams(p))
}

func (s *Server) skillsConfigWriteTyped(ctx context.Context, p skillsConfigWriteParams) (any, error) {
	return s.skillsManagerDelegate().SkillsConfigWrite(ctx, skillsruntime.SkillsConfigWriteParams(p))
}

func (s *Server) skillsSummaryWriteTyped(ctx context.Context, p skillsSummaryWriteParams) (any, error) {
	return s.skillsManagerDelegate().SkillsSummaryWrite(ctx, skillsruntime.SkillsSummaryWriteParams(p))
}

func (s *Server) skillsRemoteReadTyped(ctx context.Context, p skillsRemoteReadParams) (any, error) {
	return s.skillsManagerDelegate().SkillsRemoteRead(ctx, skillsruntime.SkillsRemoteReadParams(p))
}

func (s *Server) skillsRemoteWriteTyped(ctx context.Context, p skillsRemoteWriteParams) (any, error) {
	return s.skillsManagerDelegate().SkillsRemoteWrite(ctx, skillsruntime.SkillsRemoteWriteParams(p))
}

// GetAgentSkills 返回指定 agent 配置的技能列表。
func (s *Server) GetAgentSkills(agentID string) []string {
	return s.skillsManagerDelegate().GetAgentSkills(agentID)
}

package skills

import (
	"sync"

	"github.com/multi-agent/go-agent-v2/internal/service"
)

type SkillServiceProvider interface {
	SkillService() *service.SkillService
}

type SkillServiceProviderFunc func() *service.SkillService

func (fn SkillServiceProviderFunc) SkillService() *service.SkillService {
	if fn == nil {
		return nil
	}
	return fn()
}

type AutoSkillMatchOptions struct {
	IncludeConfiguredExplicit bool
	IncludeConfiguredForce    bool
}

type AutoMatchedSkillMatch struct {
	Name         string
	MatchedBy    string
	MatchedTerms []string
}

type AutoMatchCollector interface {
	CollectAutoMatchedSkillMatches(
		threadID string,
		prompt string,
		input []UserInput,
		options AutoSkillMatchOptions,
	) []AutoMatchedSkillMatch
}

type AutoMatchCollectorFunc func(
	threadID string,
	prompt string,
	input []UserInput,
	options AutoSkillMatchOptions,
) []AutoMatchedSkillMatch

func (fn AutoMatchCollectorFunc) CollectAutoMatchedSkillMatches(
	threadID string,
	prompt string,
	input []UserInput,
	options AutoSkillMatchOptions,
) []AutoMatchedSkillMatch {
	if fn == nil {
		return nil
	}
	return fn(threadID, prompt, input, options)
}

type Manager struct {
	skillServiceProvider SkillServiceProvider
	autoMatchCollector   AutoMatchCollector

	mu          sync.RWMutex
	agentSkills map[string][]string
}

func NewManager(provider SkillServiceProvider, collector AutoMatchCollector) *Manager {
	return &Manager{
		skillServiceProvider: provider,
		autoMatchCollector:   collector,
		agentSkills:          make(map[string][]string),
	}
}

func (m *Manager) skillService() *service.SkillService {
	if m == nil || m.skillServiceProvider == nil {
		return nil
	}
	return m.skillServiceProvider.SkillService()
}

func (m *Manager) autoMatcher() AutoMatchCollector {
	if m == nil {
		return nil
	}
	return m.autoMatchCollector
}

func (m *Manager) GetAgentSkills(agentID string) []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.agentSkills[agentID]...)
}

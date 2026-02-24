package skills

import (
	"sync"

	"github.com/multi-agent/go-agent-v2/internal/service"
)

// SkillServiceProvider provides access to SkillService without binding to apiserver.Server.
type SkillServiceProvider interface {
	SkillService() *service.SkillService
}

// SkillServiceProviderFunc adapts function to SkillServiceProvider.
type SkillServiceProviderFunc func() *service.SkillService

func (fn SkillServiceProviderFunc) SkillService() *service.SkillService {
	if fn == nil {
		return nil
	}
	return fn()
}

// AutoSkillMatchOptions controls inclusion behavior for auto-matched skills.
type AutoSkillMatchOptions struct {
	IncludeConfiguredExplicit bool
	IncludeConfiguredForce    bool
}

// AutoMatchedSkillMatch is a transport shape for auto-match results.
type AutoMatchedSkillMatch struct {
	Name         string
	MatchedBy    string
	MatchedTerms []string
}

// AutoMatchCollector bridges auto-match collection owned by apiserver helpers.
type AutoMatchCollector interface {
	CollectAutoMatchedSkillMatches(
		threadID string,
		prompt string,
		input []UserInput,
		options AutoSkillMatchOptions,
	) []AutoMatchedSkillMatch
}

// AutoMatchCollectorFunc adapts function to AutoMatchCollector.
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

// Manager owns skills JSON-RPC business logic.
type Manager struct {
	skillServiceProvider SkillServiceProvider
	autoMatchCollector   AutoMatchCollector

	mu          sync.RWMutex
	agentSkills map[string][]string
}

// NewManager builds a skills manager.
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

// GetAgentSkills returns current configured skill list for an agent.
func (m *Manager) GetAgentSkills(agentID string) []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	values := m.agentSkills[agentID]
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

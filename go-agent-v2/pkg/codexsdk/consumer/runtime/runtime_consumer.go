package runtime

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

type Deps struct {
	Manager      *runner.AgentManager
	BindingStore *store.AgentCodexBindingStore
	UIRuntime    *uistate.RuntimeManager

	BuildSelectedSkillPrompt     func(selectedSkills []string) (string, int)
	ListSkillMatchCandidates     func() ([]contracts.SkillMatchCandidate, error)
	ListAgentSkills              func(agentID string) []string
	CollectAutoMatchedSkillMatch func(
		prompt string,
		inputs []contracts.AutoMatchInput,
		configuredSkillNames []string,
		candidates []contracts.SkillMatchCandidate,
		options contracts.AutoSkillMatchOptions,
	) []contracts.AutoMatchedSkillMatch
	RenderAutoMatchedSkillPrompt func(agentID string, matches []contracts.AutoMatchedSkillMatch) (string, int)
	ActiveTrackedTurnID          func(threadID string) (string, bool)
	ShowInjectedPromptInChat     func(ctx context.Context) bool
	ResolveLSPUsagePromptHint    func(ctx context.Context, defaultHint string, maxHintLen int) string

	ThreadExistsInHistory          func(ctx context.Context, threadID string) bool
	AllDynamicToolSchemas          func() []agentcore.DynamicTool
	ResolveStartInstructions       func(ctx context.Context, dynamicTools []agentcore.DynamicTool) string
	SetAgentWorkDir                func(agentID string, cwd string)
	GetThreadID                    func(proc *runner.AgentProcess) string
	CancelCodeRuns                 func(agentID string) int
	ResolveCodexThreadCandidates   func(ctx context.Context, agentID string) []string
	ResumeThread                   func(proc *runner.AgentProcess, req agentcore.ResumeThreadRequest) error
	IsCodexProcessCrashError       func(err error) bool
	IsHistoricalResumeCandidateErr func(err error) bool
	PreviewResumeCandidates        func(candidates []string, limit int) []string
	Notify                         func(method string, payload any)
	Submit                         func(proc *runner.AgentProcess, prompt string, images, files []string, outputSchema json.RawMessage) error
	ResolveClientActiveTurnID      func(proc *runner.AgentProcess) string
	BeginTrackedTurn               func(threadID string, resolvedTurnID string) string
	TurnSteer                      func(threadID string, submitPrompt string, images, files []string) (map[string]any, error)
}

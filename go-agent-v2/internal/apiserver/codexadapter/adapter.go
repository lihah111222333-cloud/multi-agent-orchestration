package codexadapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	consumerruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/runtime"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	defaultLSPUsagePromptHint = ""
	maxLSPUsagePromptHintLen  = 16000
)

// Deps defines codex adapter runtime dependencies.
type Deps struct {
	Manager                  *runner.AgentManager
	Store                    *uistate.PreferenceManager
	BindingStore             *store.AgentCodexBindingStore
	AgentStatusStore         *store.AgentStatusStore
	UIRuntime                *uistate.RuntimeManager
	AllSchemas               func() []agentcore.DynamicTool
	NowUnixMilli             func() int64
	SetAgentWorkDir          func(agentID, cwd string)
	CancelCodeRuns           func(agentID string) int
	ReadSkillContent         func(skillName string) (string, error)
	ListSkillNames           func() ([]string, error)
	ListSkillMatchCandidates func() ([]contracts.SkillMatchCandidate, error)
	GetAgentSkills           func(agentID string) []string
	Notify                   func(method string, params any)
}

func defaultAllSchemaProvider() []agentcore.DynamicTool { return nil }

func defaultNowUnixMilliProvider() int64 { return time.Now().UnixMilli() }

func defaultSetAgentWorkDirProvider(string, string) {}

func defaultCancelCodeRunsProvider(string) int { return 0 }

func defaultReadSkillContentProvider(string) (string, error) {
	return "", appErrors.New("codexadapter.readSkillContent", "server context is not configured")
}

func defaultListSkillNamesProvider() ([]string, error) {
	return nil, appErrors.New("codexadapter.listSkillNames", "server context is not configured")
}

func defaultListSkillMatchCandidatesProvider() ([]contracts.SkillMatchCandidate, error) {
	return nil, appErrors.New("codexadapter.listSkillMatchCandidates", "server context is not configured")
}

func defaultGetAgentSkillsProvider(string) []string { return nil }

func defaultNotifyProvider(string, any) {}

func normalizeDeps(deps Deps) *Deps {
	d := deps
	if d.AllSchemas == nil {
		d.AllSchemas = defaultAllSchemaProvider
	}
	if d.NowUnixMilli == nil {
		d.NowUnixMilli = defaultNowUnixMilliProvider
	}
	if d.SetAgentWorkDir == nil {
		d.SetAgentWorkDir = defaultSetAgentWorkDirProvider
	}
	if d.CancelCodeRuns == nil {
		d.CancelCodeRuns = defaultCancelCodeRunsProvider
	}
	if d.ReadSkillContent == nil {
		d.ReadSkillContent = defaultReadSkillContentProvider
	}
	if d.ListSkillNames == nil {
		d.ListSkillNames = defaultListSkillNamesProvider
	}
	if d.ListSkillMatchCandidates == nil {
		d.ListSkillMatchCandidates = defaultListSkillMatchCandidatesProvider
	}
	if d.GetAgentSkills == nil {
		d.GetAgentSkills = defaultGetAgentSkillsProvider
	}
	if d.Notify == nil {
		d.Notify = defaultNotifyProvider
	}
	return &d
}

// Adapter 封装对 proc.Client 的直接访问。
type Adapter struct {
	ctx *Deps

	tracker                turnTrackerState
	trackerMu              sync.Mutex
	trackerActiveTurns     map[string]*trackedTurn
	trackerWatchdogTimeout time.Duration
	trackerSummaryCache    map[string]trackedTurnSummaryCacheEntry
	trackerSummaryTTL      time.Duration
	trackerStallThreshold  time.Duration
	trackerStallHeartbeat  time.Duration
}

// New 创建 codex 适配器。
func New(deps Deps) *Adapter {
	adapter := &Adapter{ctx: normalizeDeps(deps)}
	adapter.initTrackerState()
	return adapter
}

func (a *Adapter) initTrackerState() {
	initializeTrackerState(a)
}

func initializeTrackerState(a *Adapter) {
	if a == nil {
		return
	}
	a.trackerActiveTurns = make(map[string]*trackedTurn)
	a.trackerWatchdogTimeout = DefaultTurnWatchdogTimeout
	a.trackerSummaryCache = make(map[string]trackedTurnSummaryCacheEntry)
	a.trackerSummaryTTL = DefaultTrackedTurnSummaryTTL
	a.trackerStallThreshold = defaultStallThreshold
	a.trackerStallHeartbeat = defaultStallHeartbeat

	a.tracker = turnTrackerState{
		Mu:                  &a.trackerMu,
		ActiveTurns:         &a.trackerActiveTurns,
		TurnWatchdogTimeout: &a.trackerWatchdogTimeout,
		TurnSummaryCache:    &a.trackerSummaryCache,
		TurnSummaryTTL:      &a.trackerSummaryTTL,
		stallThreshold:      &a.trackerStallThreshold,
		stallHeartbeat:      &a.trackerStallHeartbeat,
	}
}

// Context returns configured dependency hooks.
func (a *Adapter) Context() *Deps {
	if a == nil {
		return nil
	}
	return a.ctx
}

func (a *Adapter) store() *uistate.PreferenceManager {
	if a == nil || a.ctx == nil {
		return nil
	}
	return a.ctx.Store
}

func (a *Adapter) manager() *runner.AgentManager {
	if a == nil || a.ctx == nil {
		return nil
	}
	return a.ctx.Manager
}

func (a *Adapter) bindingStore() *store.AgentCodexBindingStore {
	if a == nil || a.ctx == nil {
		return nil
	}
	return a.ctx.BindingStore
}

func (a *Adapter) statusStore() *store.AgentStatusStore {
	if a == nil || a.ctx == nil {
		return nil
	}
	return a.ctx.AgentStatusStore
}

func (a *Adapter) uiRuntime() *uistate.RuntimeManager {
	if a == nil || a.ctx == nil {
		return nil
	}
	return a.ctx.UIRuntime
}

func requireThreadID(caller, threadID string) (string, error) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", appErrors.New(caller, "threadId is required")
	}
	return id, nil
}

func threadLogFields(threadID string) []any {
	id := strings.TrimSpace(threadID)
	return []any{
		logger.FieldAgentID, id,
		logger.FieldThreadID, id,
	}
}

// errNoProcess is returned when the agent process or its client is nil.
var errNoProcess = errors.New("codexadapter: agent process not found")

func withClientE[T any](proc *runner.AgentProcess, fn func(agentcore.Client) (T, error)) (T, error) {
	var zero T
	if proc == nil || proc.Client == nil {
		return zero, errNoProcess
	}
	return fn(proc.Client)
}

func withClient(proc *runner.AgentProcess, fn func(agentcore.Client) error) error {
	_, err := withClientE(proc, func(c agentcore.Client) (struct{}, error) {
		return struct{}{}, fn(c)
	})
	return err
}

// Submit sends user input to codex.
func (a *Adapter) Submit(proc *runner.AgentProcess, prompt string, images, files []string, outputSchema json.RawMessage) error {
	return withClient(proc, func(c agentcore.Client) error {
		return c.Submit(prompt, images, files, outputSchema)
	})
}

// SendCommand sends slash command to codex.
func (a *Adapter) SendCommand(proc *runner.AgentProcess, command string, args string) error {
	return withClient(proc, func(c agentcore.Client) error {
		return c.SendCommand(command, args)
	})
}

// GetThreadID returns the current codex thread id.
func (a *Adapter) GetThreadID(proc *runner.AgentProcess) string {
	if proc == nil || proc.Client == nil {
		return ""
	}
	return strings.TrimSpace(proc.Client.GetThreadID())
}

// ResumeThread resumes historical codex thread.
func (a *Adapter) ResumeThread(proc *runner.AgentProcess, req agentcore.ResumeThreadRequest) error {
	return withClient(proc, func(c agentcore.Client) error {
		return c.ResumeThread(req)
	})
}

// ListThreads returns codex threads.
func (a *Adapter) ListThreads(proc *runner.AgentProcess) ([]agentcore.ThreadInfo, error) {
	return withClientE(proc, func(c agentcore.Client) ([]agentcore.ThreadInfo, error) {
		return c.ListThreads()
	})
}

// ForkThread creates a forked thread from source thread.
func (a *Adapter) ForkThread(proc *runner.AgentProcess, req agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error) {
	return withClientE(proc, func(c agentcore.Client) (*agentcore.ForkThreadResponse, error) {
		return c.ForkThread(req)
	})
}

// RespondError returns dynamic tool call error to codex.
func (a *Adapter) RespondError(proc *runner.AgentProcess, id int64, code int, message string) error {
	return withClient(proc, func(c agentcore.Client) error {
		return c.RespondError(id, code, message)
	})
}

// SendDynamicToolResult returns dynamic tool call result to codex.
func (a *Adapter) SendDynamicToolResult(proc *runner.AgentProcess, callID, output string, requestID *int64) error {
	return withClient(proc, func(c agentcore.Client) error {
		return c.SendDynamicToolResult(callID, output, requestID)
	})
}

func (a *Adapter) allDynamicToolSchemas() []agentcore.DynamicTool {
	if a == nil || a.ctx == nil {
		return nil
	}
	return a.ctx.AllSchemas()
}

func (a *Adapter) resolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []agentcore.DynamicTool) string {
	hint := a.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen)
	startInstructions, warnings := a.PrependLSPAvailabilityWarning(hint, dynamicTools, commonadapter.MergePromptText)
	if len(warnings) > 0 {
		logger.Warn("codexadapter: start instructions warnings: " + strings.Join(warnings, "; "))
	}
	return startInstructions
}

func (a *Adapter) setAgentWorkDir(agentID string, cwd string) {
	if a == nil || a.ctx == nil {
		return
	}
	a.ctx.SetAgentWorkDir(agentID, cwd)
}

func (a *Adapter) cancelCodeRuns(agentID string) int {
	if a == nil || a.ctx == nil {
		return 0
	}
	return a.ctx.CancelCodeRuns(agentID)
}

func (a *Adapter) nowUnixMilli() int64 {
	if a == nil || a.ctx == nil {
		return time.Now().UnixMilli()
	}
	return a.ctx.NowUnixMilli()
}

func (a *Adapter) readSkillContent(skillName string) (string, error) {
	if strings.TrimSpace(skillName) == "" {
		return "", appErrors.New("codexadapter.readSkillContent", "skill name is required")
	}
	if a == nil || a.ctx == nil {
		return "", appErrors.New("codexadapter.readSkillContent", "server context is not configured")
	}
	return a.ctx.ReadSkillContent(skillName)
}

func (a *Adapter) listSkillNames() ([]string, error) {
	if a == nil || a.ctx == nil {
		return nil, appErrors.New("codexadapter.listSkillNames", "server context is not configured")
	}
	return a.ctx.ListSkillNames()
}

func (a *Adapter) listSkillMatchCandidates() ([]contracts.SkillMatchCandidate, error) {
	if a == nil || a.ctx == nil {
		return nil, appErrors.New("codexadapter.listSkillMatchCandidates", "server context is not configured")
	}
	return a.ctx.ListSkillMatchCandidates()
}

func (a *Adapter) listAgentSkills(agentID string) []string {
	if a == nil || a.ctx == nil {
		return nil
	}
	return a.ctx.GetAgentSkills(agentID)
}

func (a *Adapter) notifier() func(string, any) {
	if a == nil || a.ctx == nil || a.ctx.Notify == nil {
		return nil
	}
	return a.ctx.Notify
}

func (a *Adapter) notify(method string, payload any) {
	if strings.TrimSpace(method) == "" {
		return
	}
	if notify := a.notifier(); notify != nil {
		notify(method, payload)
	}
}

func (a *Adapter) runtimeConsumerDeps() consumerruntime.Deps {
	if a == nil {
		return consumerruntime.Deps{}
	}
	return consumerruntime.Deps{
		Manager:      a.manager(),
		BindingStore: a.bindingStore(),
		UIRuntime:    a.uiRuntime(),

		BuildSelectedSkillPrompt: a.BuildSelectedSkillPrompt,
		ListSkillMatchCandidates: a.listSkillMatchCandidates,
		ListAgentSkills:          a.listAgentSkills,
		CollectAutoMatchedSkillMatch: func(
			prompt string,
			inputs []contracts.AutoMatchInput,
			configuredSkillNames []string,
			candidates []contracts.SkillMatchCandidate,
			options contracts.AutoSkillMatchOptions,
		) []contracts.AutoMatchedSkillMatch {
			return a.CollectAutoMatchedSkillMatches(prompt, inputs, configuredSkillNames, candidates, options)
		},
		RenderAutoMatchedSkillPrompt: a.RenderAutoMatchedSkillPrompt,
		ActiveTrackedTurnID:          a.activeTrackedTurnID,
		ShowInjectedPromptInChat:     a.showInjectedPromptInChat,
		ResolveLSPUsagePromptHint:    a.ResolveLSPUsagePromptHint,

		ThreadExistsInHistory: a.ThreadExistsInHistory,
		AllDynamicToolSchemas: a.allDynamicToolSchemas,
		ResolveStartInstructions: func(ctx context.Context, dynamicTools []agentcore.DynamicTool) string {
			return a.resolveStartInstructionsForLaunch(ctx, dynamicTools)
		},
		SetAgentWorkDir: a.setAgentWorkDir,
		GetThreadID:     a.GetThreadID,
		CancelCodeRuns:  a.cancelCodeRuns,
		ResolveCodexThreadCandidates: func(ctx context.Context, agentID string) []string {
			return a.ResolveCodexThreadCandidates(ctx, agentID, appendUniqueThreadIDFallback, PreviewResumeCandidates)
		},
		ResumeThread: func(proc *runner.AgentProcess, req agentcore.ResumeThreadRequest) error {
			return a.ResumeThread(proc, req)
		},
		IsCodexProcessCrashError:       IsCodexProcessCrashError,
		IsHistoricalResumeCandidateErr: IsHistoricalResumeCandidateError,
		PreviewResumeCandidates:        PreviewResumeCandidates,
		Notify:                         a.notify,
		Submit:                         a.Submit,
		ResolveClientActiveTurnID: func(proc *runner.AgentProcess) string {
			if proc == nil {
				return ""
			}
			return resolveClientActiveTurnID(proc.Client)
		},
		BeginTrackedTurn: a.beginTrackedTurn,
		TurnSteer:        a.TurnSteer,
	}
}

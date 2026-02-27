package runtime

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	serviceruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type TurnStartEntryResult = agentcore.TurnStartEntryResult

type Deps struct {
	Manager      *runner.AgentManager
	BindingStore *store.AgentCodexBindingStore
	UIRuntime    *uistate.RuntimeManager

	BuildSelectedSkillPrompt     func(selectedSkills []string) (string, int)
	ListSkillMatchCandidates     func() ([]agentcore.SkillMatchCandidate, error)
	ListAgentSkills              func(agentID string) []string
	CollectAutoMatchedSkillMatch func(
		prompt string,
		inputs []agentcore.AutoMatchInput,
		configuredSkillNames []string,
		candidates []agentcore.SkillMatchCandidate,
		options agentcore.AutoSkillMatchOptions,
	) []agentcore.AutoMatchedSkillMatch
	RenderAutoMatchedSkillPrompt func(agentID string, matches []agentcore.AutoMatchedSkillMatch) (string, int)
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

// toServiceRuntimeAdapter builds a RuntimeAdapter from consumer Deps.
// Since all DTO types are now shared via agentcore aliases, no field-by-field
// conversion is needed — types pass through transparently.
func toServiceRuntimeAdapter(deps Deps) serviceruntime.RuntimeAdapter {
	d := deps

	var getThreadID func(agentcore.Process) string
	if d.GetThreadID != nil {
		getThreadID = func(proc agentcore.Process) string {
			return d.GetThreadID(unwrapProcess(proc))
		}
	}

	var resumeThread func(agentcore.Process, serviceruntime.ResumeThreadRequest) error
	if d.ResumeThread != nil {
		resumeThread = func(proc agentcore.Process, req serviceruntime.ResumeThreadRequest) error {
			return d.ResumeThread(unwrapProcess(proc), agentcore.ResumeThreadRequest{ThreadID: req.ThreadID, Cwd: req.Cwd})
		}
	}

	var submit func(agentcore.Process, string, []string, []string, json.RawMessage) error
	if d.Submit != nil {
		submit = func(proc agentcore.Process, prompt string, images, files []string, outputSchema json.RawMessage) error {
			return d.Submit(unwrapProcess(proc), prompt, images, files, outputSchema)
		}
	}

	var resolveClientActiveTurnID func(agentcore.Process) string
	if d.ResolveClientActiveTurnID != nil {
		resolveClientActiveTurnID = func(proc agentcore.Process) string {
			return d.ResolveClientActiveTurnID(unwrapProcess(proc))
		}
	}

	return serviceruntime.RuntimeAdapter{
		PrepareAdapter: serviceruntime.PrepareAdapter{
			MergePromptText:                commonadapter.MergePromptText,
			FileContentInputText:           commonadapter.FileContentInputText,
			BuildAttachmentName:            BuildAttachmentName,
			BuildAttachmentPreviewURL:      BuildAttachmentPreviewURL,
			BuildSelectedSkillPrompt:       d.BuildSelectedSkillPrompt,
			ListSkillMatchCandidates:       d.ListSkillMatchCandidates,
			ListAgentSkills:                d.ListAgentSkills,
			CollectAutoMatchedSkillMatches: d.CollectAutoMatchedSkillMatch,
			RenderAutoMatchedSkillPrompt:   d.RenderAutoMatchedSkillPrompt,
			ActiveTrackedTurnID:            d.ActiveTrackedTurnID,
			ShowInjectedPromptInChat:       d.ShowInjectedPromptInChat,
			ResolveLSPUsagePromptHint:      d.ResolveLSPUsagePromptHint,
			UIRuntime:                      wrapUIRuntime(d.UIRuntime),
		},
		Manager:                           wrapManager(d.Manager),
		ThreadExistsInHistory:             d.ThreadExistsInHistory,
		AllDynamicToolSchemas:             d.AllDynamicToolSchemas,
		ResolveStartInstructionsForLaunch: d.ResolveStartInstructions,
		SetAgentWorkDir:                   d.SetAgentWorkDir,
		GetThreadID:                       getThreadID,
		CancelCodeRuns:                    d.CancelCodeRuns,
		BindingStore:                      wrapBindingStore(d.BindingStore),
		ResolveCodexThreadCandidates:      d.ResolveCodexThreadCandidates,
		ResumeThread:                      resumeThread,
		IsCodexProcessCrashError:          d.IsCodexProcessCrashError,
		IsHistoricalResumeCandidateError:  d.IsHistoricalResumeCandidateErr,
		PreviewResumeCandidates:           d.PreviewResumeCandidates,
		Notify:                            d.Notify,
		NormalizeSkillNames:               commonadapter.NormalizeSkillNames,
		Submit:                            submit,
		ResolveClientActiveTurnID:         resolveClientActiveTurnID,
		BeginTrackedTurn:                  d.BeginTrackedTurn,
		TurnSteer:                         d.TurnSteer,
	}
}

// ── Thin wrappers for internal types that need to implement agentcore interfaces ──

// unwrapProcess extracts the concrete *runner.AgentProcess from an agentcore.Process.
func unwrapProcess(proc agentcore.Process) *runner.AgentProcess {
	typed, ok := proc.(*runnerProcess)
	if !ok {
		return nil
	}
	return typed.proc
}

// runnerProcess adapts *runner.AgentProcess to agentcore.Process.
type runnerProcess struct{ proc *runner.AgentProcess }

func (p *runnerProcess) Port() int {
	if p == nil || p.proc == nil || p.proc.Client == nil {
		return 0
	}
	return p.proc.Client.GetPort()
}
func (p *runnerProcess) IsAlive() bool {
	if p == nil || p.proc == nil {
		return false
	}
	return p.proc.IsAlive()
}

func wrapProcess(proc *runner.AgentProcess) agentcore.Process {
	if proc == nil {
		return nil
	}
	return &runnerProcess{proc: proc}
}

// runnerManager adapts *runner.AgentManager to agentcore.Manager.
type runnerManager struct{ manager *runner.AgentManager }

func (m *runnerManager) Get(agentID string) agentcore.Process {
	if m == nil || m.manager == nil {
		return nil
	}
	return wrapProcess(m.manager.Get(agentID))
}
func (m *runnerManager) Launch(ctx context.Context, agentID, alias, profile, cwd, startInstructions string, dynamicTools []agentcore.DynamicTool) error {
	if m == nil || m.manager == nil {
		return appErrors.New("Server.ensureThreadReady", "thread manager is not initialized")
	}
	return m.manager.Launch(ctx, agentID, alias, profile, cwd, startInstructions, dynamicTools)
}
func (m *runnerManager) Stop(agentID string) error {
	if m == nil || m.manager == nil {
		return nil
	}
	return m.manager.Stop(agentID)
}

func wrapManager(mgr *runner.AgentManager) func() agentcore.Manager {
	return func() agentcore.Manager {
		if mgr == nil {
			return nil
		}
		return &runnerManager{manager: mgr}
	}
}

// bindingStoreAdapter adapts *store.AgentCodexBindingStore to agentcore.BindingStore.
type bindingStoreAdapter struct{ store *store.AgentCodexBindingStore }

func (b *bindingStoreAdapter) Bind(ctx context.Context, agentID, codexThreadID, sessionID string) error {
	if b == nil || b.store == nil {
		return nil
	}
	return b.store.Bind(ctx, agentID, codexThreadID, sessionID)
}
func (b *bindingStoreAdapter) FindByAgentID(ctx context.Context, agentID string) (*agentcore.Binding, error) {
	if b == nil || b.store == nil {
		return nil, nil
	}
	binding, err := b.store.FindByAgentID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, nil
	}
	return &agentcore.Binding{CodexThreadID: binding.CodexThreadID}, nil
}

func wrapBindingStore(s *store.AgentCodexBindingStore) func() agentcore.BindingStore {
	return func() agentcore.BindingStore {
		if s == nil {
			return nil
		}
		return &bindingStoreAdapter{store: s}
	}
}

// uiRuntimeAdapter adapts *uistate.RuntimeManager to agentcore.TimelineRuntime.
type uiRuntimeAdapter struct{ rt *uistate.RuntimeManager }

func (u *uiRuntimeAdapter) AppendUserMessage(threadID, text string, attachments []agentcore.TimelineAttachment) {
	if u == nil || u.rt == nil {
		return
	}
	uiAttachments := make([]uistate.TimelineAttachment, len(attachments))
	for i, a := range attachments {
		uiAttachments[i] = uistate.TimelineAttachment{Kind: a.Kind, Name: a.Name, Path: a.Path, PreviewURL: a.PreviewURL}
	}
	u.rt.AppendUserMessage(threadID, text, uiAttachments)
}
func (u *uiRuntimeAdapter) ThreadTimeline(threadID string) []agentcore.TimelineItem {
	if u == nil || u.rt == nil {
		return nil
	}
	items := u.rt.ThreadTimeline(threadID)
	result := make([]agentcore.TimelineItem, len(items))
	for i, it := range items {
		result[i] = agentcore.TimelineItem{Kind: it.Kind, Text: it.Text}
	}
	return result
}

func wrapUIRuntime(rt *uistate.RuntimeManager) func() agentcore.TimelineRuntime {
	return func() agentcore.TimelineRuntime {
		if rt == nil {
			return nil
		}
		return &uiRuntimeAdapter{rt: rt}
	}
}

func RegisterBinding(ctx context.Context, deps Deps, agentID string, proc *runner.AgentProcess) {
	serviceruntime.RegisterBinding(toServiceRuntimeAdapter(deps), ctx, agentID, wrapProcess(proc))
}

func TurnStart(ctx context.Context, deps Deps, req agentcore.TurnStartRequest) (TurnStartEntryResult, error) {
	return serviceruntime.TurnStart(toServiceRuntimeAdapter(deps), ctx, req)
}

func TurnSteerFromInput(deps Deps, req agentcore.TurnSteerRequest) (map[string]any, error) {
	return serviceruntime.TurnSteerFromInput(toServiceRuntimeAdapter(deps), req)
}

func TurnSteerFromInputAligned(deps Deps, req agentcore.TurnSteerRequest) (map[string]any, error) {
	return serviceruntime.TurnSteerFromInputAlignedByAdapter(
		toServiceRuntimeAdapter(deps).PrepareAdapter,
		req,
		func(runtimeReq serviceruntime.TurnSteerRequest) (map[string]any, error) {
			return TurnSteerFromInput(deps, runtimeReq)
		},
	)
}

func ResolveProcess(deps Deps, caller, threadID string) (*runner.AgentProcess, error) {
	proc, err := serviceruntime.ResolveProcess(toServiceRuntimeAdapter(deps), caller, threadID)
	if err != nil {
		return nil, err
	}
	return unwrapProcess(proc), nil
}

func WithProcess[T any](
	deps Deps,
	caller string,
	threadID string,
	fn func(*runner.AgentProcess) (T, error),
) (T, error) {
	return serviceruntime.WithProcess(toServiceRuntimeAdapter(deps), caller, threadID, func(proc agentcore.Process) (T, error) {
		return fn(unwrapProcess(proc))
	})
}

func CollectAutoMatchedSkillMatchesForThread(
	deps Deps,
	threadID string,
	prompt string,
	input []agentcore.TurnInput,
	options agentcore.AutoSkillMatchOptions,
) []agentcore.AutoMatchedSkillMatch {
	return serviceruntime.CollectAutoMatchedSkillMatchesForThread(
		toServiceRuntimeAdapter(deps).PrepareAdapter,
		threadID,
		prompt,
		input,
		options,
	)
}

package runtime

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
	serviceruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type TurnStartEntryResult = agentcore.TurnStartEntryResult

type Deps struct {
	Manager                        *runner.AgentManager
	BindingStore                   *store.AgentCodexBindingStore
	UIRuntime                      *uistate.RuntimeManager
	BuildSelectedSkillPrompt       func([]string) (string, int)
	ListSkillMatchCandidates       func() ([]agentcore.SkillMatchCandidate, error)
	ListAgentSkills                func(string) []string
	CollectAutoMatchedSkillMatch   func(string, []agentcore.AutoMatchInput, []string, []agentcore.SkillMatchCandidate, agentcore.AutoSkillMatchOptions) []agentcore.AutoMatchedSkillMatch
	RenderAutoMatchedSkillPrompt   func(string, []agentcore.AutoMatchedSkillMatch) (string, int)
	ActiveTrackedTurnID            func(string) (string, bool)
	ShowInjectedPromptInChat       func(context.Context) bool
	ResolveLSPUsagePromptHint      func(context.Context, string, int) string
	ThreadExistsInHistory          func(context.Context, string) bool
	AllDynamicToolSchemas          func() []agentcore.DynamicTool
	ResolveStartInstructions       func(context.Context, []agentcore.DynamicTool) string
	SetAgentWorkDir                func(string, string)
	GetThreadID                    func(*runner.AgentProcess) string
	CancelCodeRuns                 func(string) int
	ResolveCodexThreadCandidates   func(context.Context, string) []string
	ResumeThread                   func(*runner.AgentProcess, agentcore.ResumeThreadRequest) error
	IsCodexProcessCrashError       func(error) bool
	IsHistoricalResumeCandidateErr func(error) bool
	PreviewResumeCandidates        func([]string, int) []string
	Notify                         func(string, any)
	Submit                         func(*runner.AgentProcess, string, []string, []string, json.RawMessage) error
	ResolveClientActiveTurnID      func(*runner.AgentProcess) string
	BeginTrackedTurn               func(string, string) string
	TurnSteer                      func(string, string, []string, []string) (map[string]any, error)
}

type managerAdapter struct{ *runner.AgentManager }

func (a managerAdapter) Get(agentID string) agentcore.Process {
	if a.AgentManager == nil {
		return nil
	}
	return wrapProcess(a.AgentManager.Get(agentID))
}

type bindingStoreAdapter struct{ *store.AgentCodexBindingStore }

func (a bindingStoreAdapter) FindByAgentID(ctx context.Context, agentID string) (*agentcore.Binding, error) {
	if a.AgentCodexBindingStore == nil {
		return nil, nil
	}
	b, err := a.AgentCodexBindingStore.FindByAgentID(ctx, agentID)
	if err != nil || b == nil {
		return nil, err
	}
	return &agentcore.Binding{CodexThreadID: b.CodexThreadID}, nil
}

type uiRuntimeAdapter struct{ *uistate.RuntimeManager }

func (a uiRuntimeAdapter) AppendUserMessage(threadID, text string, attachments []agentcore.TimelineAttachment) {
	if a.RuntimeManager == nil {
		return
	}
	x := make([]uistate.TimelineAttachment, len(attachments))
	for i := range attachments {
		it := attachments[i]
		x[i] = uistate.TimelineAttachment{Kind: it.Kind, Name: it.Name, Path: it.Path, PreviewURL: it.PreviewURL}
	}
	a.RuntimeManager.AppendUserMessage(threadID, text, x)
}

func (a uiRuntimeAdapter) ThreadTimeline(threadID string) []agentcore.TimelineItem {
	if a.RuntimeManager == nil {
		return nil
	}
	items := a.RuntimeManager.ThreadTimeline(threadID)
	out := make([]agentcore.TimelineItem, len(items))
	for i := range items {
		out[i] = agentcore.TimelineItem{Kind: items[i].Kind, Text: items[i].Text}
	}
	return out
}

type runnerProcess struct{ *runner.AgentProcess }

func (p runnerProcess) Port() int {
	if p.AgentProcess == nil || p.Client == nil {
		return 0
	}
	return p.Client.GetPort()
}

func (p runnerProcess) IsAlive() bool {
	return p.AgentProcess != nil && p.AgentProcess.IsAlive()
}

func wrapProcess(proc *runner.AgentProcess) agentcore.Process {
	if proc == nil {
		return nil
	}
	return runnerProcess{proc}
}

func unwrapProcess(proc agentcore.Process) *runner.AgentProcess {
	switch p := proc.(type) {
	case runnerProcess:
		return p.AgentProcess
	case *runnerProcess:
		if p == nil {
			return nil
		}
		return p.AgentProcess
	default:
		return nil
	}
}

func optionalProvider[T any](enabled bool, value T) func() T {
	return func() T {
		if enabled {
			return value
		}
		var zero T
		return zero
	}
}

func adaptProcessString(fn func(*runner.AgentProcess) string) func(agentcore.Process) string {
	if fn == nil {
		return nil
	}
	return func(proc agentcore.Process) string {
		return fn(unwrapProcess(proc))
	}
}

func adaptResumeThread(fn func(*runner.AgentProcess, agentcore.ResumeThreadRequest) error) func(agentcore.Process, serviceruntime.ResumeThreadRequest) error {
	if fn == nil {
		return nil
	}
	return func(proc agentcore.Process, req serviceruntime.ResumeThreadRequest) error {
		return fn(unwrapProcess(proc), agentcore.ResumeThreadRequest{ThreadID: req.ThreadID, Cwd: req.Cwd})
	}
}

func adaptSubmit(fn func(*runner.AgentProcess, string, []string, []string, json.RawMessage) error) func(agentcore.Process, string, []string, []string, json.RawMessage) error {
	if fn == nil {
		return nil
	}
	return func(proc agentcore.Process, prompt string, images, files []string, outputSchema json.RawMessage) error {
		return fn(unwrapProcess(proc), prompt, images, files, outputSchema)
	}
}

func toServiceRuntimeAdapter(d Deps) serviceruntime.RuntimeAdapter {
	manager := managerAdapter{AgentManager: d.Manager}
	bindingStore := bindingStoreAdapter{AgentCodexBindingStore: d.BindingStore}
	uiRuntime := uiRuntimeAdapter{RuntimeManager: d.UIRuntime}
	return serviceruntime.RuntimeAdapter{PrepareAdapter: serviceruntime.PrepareAdapter{MergePromptText: commonadapter.MergePromptText, FileContentInputText: commonadapter.FileContentInputText, BuildAttachmentName: BuildAttachmentName, BuildAttachmentPreviewURL: BuildAttachmentPreviewURL, BuildSelectedSkillPrompt: d.BuildSelectedSkillPrompt, ListSkillMatchCandidates: d.ListSkillMatchCandidates, ListAgentSkills: d.ListAgentSkills, CollectAutoMatchedSkillMatches: d.CollectAutoMatchedSkillMatch, RenderAutoMatchedSkillPrompt: d.RenderAutoMatchedSkillPrompt, ActiveTrackedTurnID: d.ActiveTrackedTurnID, ShowInjectedPromptInChat: d.ShowInjectedPromptInChat, ResolveLSPUsagePromptHint: d.ResolveLSPUsagePromptHint, UIRuntime: optionalProvider(d.UIRuntime != nil, agentcore.TimelineRuntime(uiRuntime))},
		Manager: optionalProvider(d.Manager != nil, agentcore.Manager(manager)), ThreadExistsInHistory: d.ThreadExistsInHistory, AllDynamicToolSchemas: d.AllDynamicToolSchemas, ResolveStartInstructionsForLaunch: d.ResolveStartInstructions, SetAgentWorkDir: d.SetAgentWorkDir, GetThreadID: adaptProcessString(d.GetThreadID), CancelCodeRuns: d.CancelCodeRuns, BindingStore: optionalProvider(d.BindingStore != nil, agentcore.BindingStore(bindingStore)), ResolveCodexThreadCandidates: d.ResolveCodexThreadCandidates,
		ResumeThread: adaptResumeThread(d.ResumeThread), IsCodexProcessCrashError: d.IsCodexProcessCrashError, IsHistoricalResumeCandidateError: d.IsHistoricalResumeCandidateErr, PreviewResumeCandidates: d.PreviewResumeCandidates, Notify: d.Notify, NormalizeSkillNames: commonadapter.NormalizeSkillNames, Submit: adaptSubmit(d.Submit), ResolveClientActiveTurnID: adaptProcessString(d.ResolveClientActiveTurnID), BeginTrackedTurn: d.BeginTrackedTurn, TurnSteer: d.TurnSteer}
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
	adapter := toServiceRuntimeAdapter(deps)
	return serviceruntime.TurnSteerFromInputAlignedByAdapter(adapter.PrepareAdapter, req, func(runtimeReq serviceruntime.TurnSteerRequest) (map[string]any, error) {
		return serviceruntime.TurnSteerFromInput(adapter, runtimeReq)
	})
}

func ResolveProcess(deps Deps, caller, threadID string) (*runner.AgentProcess, error) {
	proc, err := serviceruntime.ResolveProcess(toServiceRuntimeAdapter(deps), caller, threadID)
	if err != nil {
		return nil, err
	}
	return unwrapProcess(proc), nil
}

func WithProcess[T any](deps Deps, caller, threadID string, fn func(*runner.AgentProcess) (T, error)) (T, error) {
	return serviceruntime.WithProcess(toServiceRuntimeAdapter(deps), caller, threadID, func(proc agentcore.Process) (T, error) {
		return fn(unwrapProcess(proc))
	})
}

func CollectAutoMatchedSkillMatchesForThread(deps Deps, threadID, prompt string, input []agentcore.TurnInput, options agentcore.AutoSkillMatchOptions) []agentcore.AutoMatchedSkillMatch {
	return serviceruntime.CollectAutoMatchedSkillMatchesForThread(toServiceRuntimeAdapter(deps).PrepareAdapter, threadID, prompt, input, options)
}

func SetDefaultFunc[T any](slot *T, fallback T) {
	if slot == nil {
		return
	}
	v := reflect.ValueOf(*slot)
	if v.IsValid() && v.Kind() == reflect.Func && !v.IsNil() {
		return
	}
	*slot = fallback
}

func BuildAttachmentName(path string) string {
	return util.BuildAttachmentName(path)
}

func BuildAttachmentPreviewURL(path string) string {
	return util.BuildAttachmentPreviewURL(path)
}

func SetStreamReadIdleTimeout(timeout time.Duration) {
	codex.SetAppServerReadIdleTimeout(timeout)
}

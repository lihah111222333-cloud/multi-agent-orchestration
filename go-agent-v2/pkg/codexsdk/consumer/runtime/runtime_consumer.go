package runtime

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	serviceruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type TurnStartEntryResult struct {
	TurnID string
}

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

type serviceRuntimeProcess struct {
	proc *runner.AgentProcess
}

func (p *serviceRuntimeProcess) Port() int {
	if p == nil || p.proc == nil || p.proc.Client == nil {
		return 0
	}
	return p.proc.Client.GetPort()
}

func (p *serviceRuntimeProcess) MarkSessionLost() {
	if p == nil || p.proc == nil {
		return
	}
	p.proc.MarkSessionLost()
}

func wrapServiceRuntimeProcess(proc *runner.AgentProcess) serviceruntime.Process {
	if proc == nil {
		return nil
	}
	return &serviceRuntimeProcess{proc: proc}
}

func unwrapServiceRuntimeProcess(proc serviceruntime.Process) *runner.AgentProcess {
	typed, ok := proc.(*serviceRuntimeProcess)
	if !ok {
		return nil
	}
	return typed.proc
}

type serviceRuntimeManager struct {
	manager *runner.AgentManager
}

func (m *serviceRuntimeManager) Get(agentID string) serviceruntime.Process {
	if m == nil || m.manager == nil {
		return nil
	}
	return wrapServiceRuntimeProcess(m.manager.Get(agentID))
}

func (m *serviceRuntimeManager) Launch(
	ctx context.Context,
	agentID, alias, profile, cwd, startInstructions string,
	dynamicTools []agentcore.DynamicTool,
) error {
	if m == nil || m.manager == nil {
		return appErrors.New("Server.ensureThreadReady", "thread manager is not initialized")
	}
	return m.manager.Launch(ctx, agentID, alias, profile, cwd, startInstructions, dynamicTools)
}

func (m *serviceRuntimeManager) Stop(agentID string) error {
	if m == nil || m.manager == nil {
		return nil
	}
	return m.manager.Stop(agentID)
}

type serviceRuntimeBindingStore struct {
	store *store.AgentCodexBindingStore
}

func (b *serviceRuntimeBindingStore) Bind(ctx context.Context, agentID, codexThreadID, sessionID string) error {
	if b == nil || b.store == nil {
		return nil
	}
	return b.store.Bind(ctx, agentID, codexThreadID, sessionID)
}

func (b *serviceRuntimeBindingStore) FindByAgentID(ctx context.Context, agentID string) (*serviceruntime.Binding, error) {
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
	return &serviceruntime.Binding{CodexThreadID: binding.CodexThreadID}, nil
}

type serviceRuntimeUIRuntime struct {
	uiRuntime *uistate.RuntimeManager
}

func (u *serviceRuntimeUIRuntime) AppendUserMessage(threadID, text string, attachments []serviceruntime.TimelineAttachment) {
	if u == nil || u.uiRuntime == nil {
		return
	}
	u.uiRuntime.AppendUserMessage(threadID, text, fromRuntimeTimelineAttachments(attachments))
}

func (u *serviceRuntimeUIRuntime) ThreadTimeline(threadID string) []serviceruntime.TimelineItem {
	if u == nil || u.uiRuntime == nil {
		return nil
	}
	return mapSlice(u.uiRuntime.ThreadTimeline(threadID), func(item uistate.TimelineItem) serviceruntime.TimelineItem {
		return serviceruntime.TimelineItem{Kind: item.Kind, Text: item.Text}
	})
}

func toServiceRuntimeAdapter(deps Deps) serviceruntime.RuntimeAdapter {
	d := deps

	var getThreadID func(serviceruntime.Process) string
	if d.GetThreadID != nil {
		getThreadID = func(proc serviceruntime.Process) string {
			return d.GetThreadID(unwrapServiceRuntimeProcess(proc))
		}
	}

	var resumeThread func(serviceruntime.Process, serviceruntime.ResumeThreadRequest) error
	if d.ResumeThread != nil {
		resumeThread = func(proc serviceruntime.Process, req serviceruntime.ResumeThreadRequest) error {
			return d.ResumeThread(unwrapServiceRuntimeProcess(proc), agentcore.ResumeThreadRequest{ThreadID: req.ThreadID, Cwd: req.Cwd})
		}
	}

	var submit func(serviceruntime.Process, string, []string, []string, json.RawMessage) error
	if d.Submit != nil {
		submit = func(proc serviceruntime.Process, prompt string, images, files []string, outputSchema json.RawMessage) error {
			return d.Submit(unwrapServiceRuntimeProcess(proc), prompt, images, files, outputSchema)
		}
	}

	var resolveClientActiveTurnID func(serviceruntime.Process) string
	if d.ResolveClientActiveTurnID != nil {
		resolveClientActiveTurnID = func(proc serviceruntime.Process) string {
			return d.ResolveClientActiveTurnID(unwrapServiceRuntimeProcess(proc))
		}
	}

	listSkillMatchCandidates := func() ([]serviceruntime.SkillMatchCandidate, error) {
		if d.ListSkillMatchCandidates == nil {
			return nil, nil
		}
		candidates, err := d.ListSkillMatchCandidates()
		if err != nil {
			return nil, err
		}
		return toRuntimeSkillMatchCandidates(candidates), nil
	}

	collectAutoMatchedSkillMatches := func(
		prompt string,
		inputs []serviceruntime.AutoMatchInput,
		configuredSkillNames []string,
		candidates []serviceruntime.SkillMatchCandidate,
		options serviceruntime.AutoSkillMatchOptions,
	) []serviceruntime.AutoMatchedSkillMatch {
		if d.CollectAutoMatchedSkillMatch == nil {
			return nil
		}
		matches := d.CollectAutoMatchedSkillMatch(
			prompt,
			fromRuntimeAutoMatchInputs(inputs),
			configuredSkillNames,
			fromRuntimeSkillMatchCandidates(candidates),
			fromRuntimeAutoSkillMatchOptions(options),
		)
		return toRuntimeAutoMatchedSkillMatches(matches)
	}

	renderAutoMatchedSkillPrompt := func(agentID string, matches []serviceruntime.AutoMatchedSkillMatch) (string, int) {
		if d.RenderAutoMatchedSkillPrompt == nil {
			return "", 0
		}
		return d.RenderAutoMatchedSkillPrompt(agentID, fromRuntimeAutoMatchedSkillMatches(matches))
	}

	return serviceruntime.RuntimeAdapter{
		PrepareAdapter: serviceruntime.PrepareAdapter{
			MergePromptText:                commonadapter.MergePromptText,
			FileContentInputText:           commonadapter.FileContentInputText,
			BuildAttachmentName:            BuildAttachmentName,
			BuildAttachmentPreviewURL:      BuildAttachmentPreviewURL,
			BuildSelectedSkillPrompt:       d.BuildSelectedSkillPrompt,
			ListSkillMatchCandidates:       listSkillMatchCandidates,
			ListAgentSkills:                d.ListAgentSkills,
			CollectAutoMatchedSkillMatches: collectAutoMatchedSkillMatches,
			RenderAutoMatchedSkillPrompt:   renderAutoMatchedSkillPrompt,
			ActiveTrackedTurnID:            d.ActiveTrackedTurnID,
			ShowInjectedPromptInChat:       d.ShowInjectedPromptInChat,
			ResolveLSPUsagePromptHint:      d.ResolveLSPUsagePromptHint,
			UIRuntime: func() serviceruntime.TimelineRuntime {
				if d.UIRuntime == nil {
					return nil
				}
				return &serviceRuntimeUIRuntime{uiRuntime: d.UIRuntime}
			},
		},
		Manager: func() serviceruntime.Manager {
			if d.Manager == nil {
				return nil
			}
			return &serviceRuntimeManager{manager: d.Manager}
		},
		ThreadExistsInHistory:             d.ThreadExistsInHistory,
		AllDynamicToolSchemas:             d.AllDynamicToolSchemas,
		ResolveStartInstructionsForLaunch: d.ResolveStartInstructions,
		SetAgentWorkDir:                   d.SetAgentWorkDir,
		GetThreadID:                       getThreadID,
		CancelCodeRuns:                    d.CancelCodeRuns,
		BindingStore: func() serviceruntime.BindingStore {
			if d.BindingStore == nil {
				return nil
			}
			return &serviceRuntimeBindingStore{store: d.BindingStore}
		},
		ResolveCodexThreadCandidates:     d.ResolveCodexThreadCandidates,
		ResumeThread:                     resumeThread,
		IsCodexProcessCrashError:         d.IsCodexProcessCrashError,
		IsHistoricalResumeCandidateError: d.IsHistoricalResumeCandidateErr,
		PreviewResumeCandidates:          d.PreviewResumeCandidates,
		Notify:                           d.Notify,
		NormalizeSkillNames:              commonadapter.NormalizeSkillNames,
		Submit:                           submit,
		ResolveClientActiveTurnID:        resolveClientActiveTurnID,
		BeginTrackedTurn:                 d.BeginTrackedTurn,
		TurnSteer:                        d.TurnSteer,
	}
}

func RegisterBinding(ctx context.Context, deps Deps, agentID string, proc *runner.AgentProcess) {
	serviceruntime.RegisterBinding(toServiceRuntimeAdapter(deps), ctx, agentID, wrapServiceRuntimeProcess(proc))
}

func BuildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
	return serviceruntime.BuildSessionLostNotification(agentID, lastErr)
}

func TurnStart(ctx context.Context, deps Deps, req contracts.TurnStartRequest) (TurnStartEntryResult, error) {
	result, err := serviceruntime.TurnStart(toServiceRuntimeAdapter(deps), ctx, toRuntimeTurnStartRequest(req))
	if err != nil {
		return TurnStartEntryResult{}, err
	}
	return TurnStartEntryResult{TurnID: result.TurnID}, nil
}

func TurnSteerFromInput(deps Deps, req contracts.TurnSteerRequest) (map[string]any, error) {
	return serviceruntime.TurnSteerFromInput(toServiceRuntimeAdapter(deps), toRuntimeTurnSteerRequest(req))
}

func TurnSteerFromInputAligned(deps Deps, req contracts.TurnSteerRequest) (map[string]any, error) {
	return serviceruntime.TurnSteerFromInputAlignedByAdapter(
		toServiceRuntimeAdapter(deps).PrepareAdapter,
		toRuntimeTurnSteerRequest(req),
		func(runtimeReq serviceruntime.TurnSteerRequest) (map[string]any, error) {
			return TurnSteerFromInput(deps, fromRuntimeTurnSteerRequest(runtimeReq))
		},
	)
}

func ResolveProcess(deps Deps, caller, threadID string) (*runner.AgentProcess, error) {
	proc, err := serviceruntime.ResolveProcess(toServiceRuntimeAdapter(deps), caller, threadID)
	if err != nil {
		return nil, err
	}
	return unwrapServiceRuntimeProcess(proc), nil
}

func WithProcess[T any](
	deps Deps,
	caller string,
	threadID string,
	fn func(*runner.AgentProcess) (T, error),
) (T, error) {
	return serviceruntime.WithProcess(toServiceRuntimeAdapter(deps), caller, threadID, func(proc serviceruntime.Process) (T, error) {
		return fn(unwrapServiceRuntimeProcess(proc))
	})
}

func CollectAutoMatchedSkillMatchesForThread(
	deps Deps,
	threadID string,
	prompt string,
	input []contracts.TurnInput,
	options contracts.AutoSkillMatchOptions,
) []contracts.AutoMatchedSkillMatch {
	matches := serviceruntime.CollectAutoMatchedSkillMatchesForThread(
		toServiceRuntimeAdapter(deps).PrepareAdapter,
		threadID,
		prompt,
		toRuntimeTurnInputs(input),
		toRuntimeAutoSkillMatchOptions(options),
	)
	return fromRuntimeAutoMatchedSkillMatches(matches)
}

func mapSlice[S any, D any](in []S, mapFn func(S) D) []D {
	if len(in) == 0 {
		return nil
	}
	out := make([]D, 0, len(in))
	for _, item := range in {
		out = append(out, mapFn(item))
	}
	return out
}

func cloneStrings(in []string) []string {
	return append([]string(nil), in...)
}

func toRuntimeTurnInputs(inputs []contracts.TurnInput) []serviceruntime.TurnInput {
	return mapSlice(inputs, func(input contracts.TurnInput) serviceruntime.TurnInput {
		return serviceruntime.TurnInput{
			Type:    input.Type,
			Text:    input.Text,
			URL:     input.URL,
			Path:    input.Path,
			Name:    input.Name,
			Content: input.Content,
		}
	})
}

func toRuntimeTurnStartRequest(req contracts.TurnStartRequest) serviceruntime.TurnStartRequest {
	return serviceruntime.TurnStartRequest{
		ThreadID:             req.ThreadID,
		Cwd:                  req.Cwd,
		Input:                toRuntimeTurnInputs(req.Input),
		SelectedSkills:       cloneStrings(req.SelectedSkills),
		ManualSkillSelection: req.ManualSkillSelection,
		OutputSchema:         req.OutputSchema,
	}
}

func toRuntimeTurnSteerRequest(req contracts.TurnSteerRequest) serviceruntime.TurnSteerRequest {
	return serviceruntime.TurnSteerRequest{
		ThreadID:             req.ThreadID,
		ExpectedTurnID:       req.ExpectedTurnID,
		Input:                toRuntimeTurnInputs(req.Input),
		SelectedSkills:       cloneStrings(req.SelectedSkills),
		ManualSkillSelection: req.ManualSkillSelection,
	}
}

func fromRuntimeTurnSteerRequest(req serviceruntime.TurnSteerRequest) contracts.TurnSteerRequest {
	return contracts.TurnSteerRequest{
		ThreadID:             req.ThreadID,
		ExpectedTurnID:       req.ExpectedTurnID,
		Input:                fromRuntimeTurnInputs(req.Input),
		SelectedSkills:       cloneStrings(req.SelectedSkills),
		ManualSkillSelection: req.ManualSkillSelection,
	}
}

func fromRuntimeTurnInputs(inputs []serviceruntime.TurnInput) []contracts.TurnInput {
	return mapSlice(inputs, func(input serviceruntime.TurnInput) contracts.TurnInput {
		return contracts.TurnInput{
			Type:    input.Type,
			Text:    input.Text,
			URL:     input.URL,
			Path:    input.Path,
			Name:    input.Name,
			Content: input.Content,
		}
	})
}

func fromRuntimeTimelineAttachments(in []serviceruntime.TimelineAttachment) []uistate.TimelineAttachment {
	return mapSlice(in, func(item serviceruntime.TimelineAttachment) uistate.TimelineAttachment {
		return uistate.TimelineAttachment{
			Kind:       item.Kind,
			Name:       item.Name,
			Path:       item.Path,
			PreviewURL: item.PreviewURL,
		}
	})
}

func fromRuntimeAutoMatchInputs(inputs []serviceruntime.AutoMatchInput) []contracts.AutoMatchInput {
	return mapSlice(inputs, func(input serviceruntime.AutoMatchInput) contracts.AutoMatchInput {
		return contracts.AutoMatchInput{Type: input.Type, Name: input.Name}
	})
}

func toRuntimeSkillMatchCandidates(candidates []contracts.SkillMatchCandidate) []serviceruntime.SkillMatchCandidate {
	return mapSlice(candidates, func(candidate contracts.SkillMatchCandidate) serviceruntime.SkillMatchCandidate {
		return serviceruntime.SkillMatchCandidate{
			Name:         candidate.Name,
			ForceWords:   cloneStrings(candidate.ForceWords),
			TriggerWords: cloneStrings(candidate.TriggerWords),
		}
	})
}

func fromRuntimeSkillMatchCandidates(candidates []serviceruntime.SkillMatchCandidate) []contracts.SkillMatchCandidate {
	return mapSlice(candidates, func(candidate serviceruntime.SkillMatchCandidate) contracts.SkillMatchCandidate {
		return contracts.SkillMatchCandidate{
			Name:         candidate.Name,
			ForceWords:   cloneStrings(candidate.ForceWords),
			TriggerWords: cloneStrings(candidate.TriggerWords),
		}
	})
}

func toRuntimeAutoSkillMatchOptions(options contracts.AutoSkillMatchOptions) serviceruntime.AutoSkillMatchOptions {
	return serviceruntime.AutoSkillMatchOptions{
		IncludeConfiguredExplicit: options.IncludeConfiguredExplicit,
		IncludeConfiguredForce:    options.IncludeConfiguredForce,
	}
}

func fromRuntimeAutoSkillMatchOptions(options serviceruntime.AutoSkillMatchOptions) contracts.AutoSkillMatchOptions {
	return contracts.AutoSkillMatchOptions{
		IncludeConfiguredExplicit: options.IncludeConfiguredExplicit,
		IncludeConfiguredForce:    options.IncludeConfiguredForce,
	}
}

func toRuntimeAutoMatchedSkillMatches(matches []contracts.AutoMatchedSkillMatch) []serviceruntime.AutoMatchedSkillMatch {
	return mapSlice(matches, func(match contracts.AutoMatchedSkillMatch) serviceruntime.AutoMatchedSkillMatch {
		return serviceruntime.AutoMatchedSkillMatch{
			Name:         match.Name,
			MatchedBy:    match.MatchedBy,
			MatchedTerms: cloneStrings(match.MatchedTerms),
		}
	})
}

func fromRuntimeAutoMatchedSkillMatches(matches []serviceruntime.AutoMatchedSkillMatch) []contracts.AutoMatchedSkillMatch {
	return mapSlice(matches, func(match serviceruntime.AutoMatchedSkillMatch) contracts.AutoMatchedSkillMatch {
		return contracts.AutoMatchedSkillMatch{
			Name:         match.Name,
			MatchedBy:    match.MatchedBy,
			MatchedTerms: cloneStrings(match.MatchedTerms),
		}
	})
}

package codexadapter

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	consumerruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/runtime"
	serviceruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type turnStartRequest = contracts.TurnStartRequest

type turnSteerRequest = contracts.TurnSteerRequest

type turnStartEntryResult struct {
	TurnID string `json:"turnId"`
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
	if proc == nil {
		return nil
	}
	if wrapped, ok := proc.(*serviceRuntimeProcess); ok {
		return wrapped.proc
	}
	return nil
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
	agentID, name, prompt, cwd, instructions string,
	dynamicTools []agentcore.DynamicTool,
) error {
	if m == nil || m.manager == nil {
		return appErrors.New("Server.ensureThreadReady", "thread manager is not initialized")
	}
	return m.manager.Launch(ctx, agentID, name, prompt, cwd, instructions, dynamicTools)
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
	if err != nil || binding == nil {
		return nil, err
	}
	return &serviceruntime.Binding{CodexThreadID: binding.CodexThreadID}, nil
}

type serviceRuntimeUIRuntime struct {
	runtime *uistate.RuntimeManager
}

func (u *serviceRuntimeUIRuntime) AppendUserMessage(threadID, text string, attachments []serviceruntime.TimelineAttachment) {
	if u == nil || u.runtime == nil {
		return
	}
	u.runtime.AppendUserMessage(threadID, text, fromRuntimeTimelineAttachments(attachments))
}

func (u *serviceRuntimeUIRuntime) ThreadTimeline(threadID string) []serviceruntime.TimelineItem {
	if u == nil || u.runtime == nil {
		return nil
	}
	timeline := u.runtime.ThreadTimeline(threadID)
	if len(timeline) == 0 {
		return nil
	}
	items := make([]serviceruntime.TimelineItem, 0, len(timeline))
	for _, item := range timeline {
		items = append(items, serviceruntime.TimelineItem{
			Kind: item.Kind,
			Text: item.Text,
		})
	}
	return items
}

type serviceRuntimeBridge struct {
	deps consumerruntime.Deps
}

func newServiceRuntimeBridge(deps consumerruntime.Deps) *serviceRuntimeBridge {
	return &serviceRuntimeBridge{deps: deps}
}

func (b *serviceRuntimeBridge) MergePromptText(left, right string) string {
	return commonadapter.MergePromptText(left, right)
}

func (b *serviceRuntimeBridge) FileContentInputText(name, content string) string {
	return commonadapter.FileContentInputText(name, content)
}

func (b *serviceRuntimeBridge) BuildSelectedSkillPrompt(selectedSkills []string) (string, int) {
	if b.deps.BuildSelectedSkillPrompt == nil {
		return "", 0
	}
	return b.deps.BuildSelectedSkillPrompt(selectedSkills)
}

func (b *serviceRuntimeBridge) ListSkillMatchCandidates() ([]serviceruntime.SkillMatchCandidate, error) {
	if b.deps.ListSkillMatchCandidates == nil {
		return nil, nil
	}
	candidates, err := b.deps.ListSkillMatchCandidates()
	if err != nil {
		return nil, err
	}
	return toRuntimeSkillMatchCandidates(candidates), nil
}

func (b *serviceRuntimeBridge) ListAgentSkills(agentID string) []string {
	if b.deps.ListAgentSkills == nil {
		return nil
	}
	return b.deps.ListAgentSkills(agentID)
}

func (b *serviceRuntimeBridge) CollectAutoMatchedSkillMatches(
	prompt string,
	inputs []serviceruntime.AutoMatchInput,
	configuredSkillNames []string,
	candidates []serviceruntime.SkillMatchCandidate,
	options serviceruntime.AutoSkillMatchOptions,
) []serviceruntime.AutoMatchedSkillMatch {
	if b.deps.CollectAutoMatchedSkillMatch == nil {
		return nil
	}
	matches := b.deps.CollectAutoMatchedSkillMatch(
		prompt,
		fromRuntimeAutoMatchInputs(inputs),
		configuredSkillNames,
		fromRuntimeSkillMatchCandidates(candidates),
		fromRuntimeAutoSkillMatchOptions(options),
	)
	return toRuntimeAutoMatchedSkillMatches(matches)
}

func (b *serviceRuntimeBridge) RenderAutoMatchedSkillPrompt(agentID string, matches []serviceruntime.AutoMatchedSkillMatch) (string, int) {
	if b.deps.RenderAutoMatchedSkillPrompt == nil {
		return "", 0
	}
	return b.deps.RenderAutoMatchedSkillPrompt(agentID, fromRuntimeAutoMatchedSkillMatches(matches))
}

func (b *serviceRuntimeBridge) ActiveTrackedTurnID(threadID string) (string, bool) {
	if b.deps.ActiveTrackedTurnID == nil {
		return "", false
	}
	return b.deps.ActiveTrackedTurnID(threadID)
}

func (b *serviceRuntimeBridge) RequireThreadID(caller, threadID string) (string, error) {
	return requireThreadID(caller, threadID)
}

func (b *serviceRuntimeBridge) NewError(caller, message string) error {
	return appErrors.New(caller, message)
}

func (b *serviceRuntimeBridge) NewErrorf(caller, format string, args ...any) error {
	return appErrors.Newf(caller, format, args...)
}

func (b *serviceRuntimeBridge) ShowInjectedPromptInChat(ctx context.Context) bool {
	if b.deps.ShowInjectedPromptInChat == nil {
		return false
	}
	return b.deps.ShowInjectedPromptInChat(ctx)
}

func (b *serviceRuntimeBridge) ResolveLSPUsagePromptHint(ctx context.Context, defaultHint string, maxHintLen int) string {
	if b.deps.ResolveLSPUsagePromptHint == nil {
		return defaultHint
	}
	return b.deps.ResolveLSPUsagePromptHint(ctx, defaultHint, maxHintLen)
}

func (b *serviceRuntimeBridge) DefaultLSPUsagePromptHint() string {
	return defaultLSPUsagePromptHint
}

func (b *serviceRuntimeBridge) MaxLSPUsagePromptHintLen() int {
	return maxLSPUsagePromptHintLen
}

func (b *serviceRuntimeBridge) UIRuntime() serviceruntime.TimelineRuntime {
	if b.deps.UIRuntime == nil {
		return nil
	}
	return &serviceRuntimeUIRuntime{runtime: b.deps.UIRuntime}
}

func (b *serviceRuntimeBridge) Manager() serviceruntime.Manager {
	if b.deps.Manager == nil {
		return nil
	}
	return &serviceRuntimeManager{manager: b.deps.Manager}
}

func (b *serviceRuntimeBridge) ThreadExistsInHistory(ctx context.Context, threadID string) bool {
	if b.deps.ThreadExistsInHistory == nil {
		return false
	}
	return b.deps.ThreadExistsInHistory(ctx, threadID)
}

func (b *serviceRuntimeBridge) AllDynamicToolSchemas() []agentcore.DynamicTool {
	if b.deps.AllDynamicToolSchemas == nil {
		return nil
	}
	return b.deps.AllDynamicToolSchemas()
}

func (b *serviceRuntimeBridge) ResolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []agentcore.DynamicTool) string {
	if b.deps.ResolveStartInstructions == nil {
		return ""
	}
	return b.deps.ResolveStartInstructions(ctx, dynamicTools)
}

func (b *serviceRuntimeBridge) SetAgentWorkDir(agentID, cwd string) {
	if b.deps.SetAgentWorkDir == nil {
		return
	}
	b.deps.SetAgentWorkDir(agentID, cwd)
}

func (b *serviceRuntimeBridge) ThreadLogFields(threadID string) []any {
	return threadLogFields(threadID)
}

func (b *serviceRuntimeBridge) GetThreadID(proc serviceruntime.Process) string {
	if b.deps.GetThreadID == nil {
		return ""
	}
	return b.deps.GetThreadID(unwrapServiceRuntimeProcess(proc))
}

func (b *serviceRuntimeBridge) CancelCodeRuns(agentID string) int {
	if b.deps.CancelCodeRuns == nil {
		return 0
	}
	return b.deps.CancelCodeRuns(agentID)
}

func (b *serviceRuntimeBridge) BindingStore() serviceruntime.BindingStore {
	if b.deps.BindingStore == nil {
		return nil
	}
	return &serviceRuntimeBindingStore{store: b.deps.BindingStore}
}

func (b *serviceRuntimeBridge) ResolveCodexThreadCandidates(ctx context.Context, agentID string) []string {
	if b.deps.ResolveCodexThreadCandidates == nil {
		return nil
	}
	return b.deps.ResolveCodexThreadCandidates(ctx, agentID)
}

func (b *serviceRuntimeBridge) ResumeThread(proc serviceruntime.Process, req serviceruntime.ResumeThreadRequest) error {
	if b.deps.ResumeThread == nil {
		return nil
	}
	return b.deps.ResumeThread(unwrapServiceRuntimeProcess(proc), agentcore.ResumeThreadRequest{
		ThreadID: req.ThreadID,
		Cwd:      req.Cwd,
	})
}

func (b *serviceRuntimeBridge) IsCodexProcessCrashError(err error) bool {
	if b.deps.IsCodexProcessCrashError == nil {
		return false
	}
	return b.deps.IsCodexProcessCrashError(err)
}

func (b *serviceRuntimeBridge) IsHistoricalResumeCandidateError(err error) bool {
	if b.deps.IsHistoricalResumeCandidateErr == nil {
		return false
	}
	return b.deps.IsHistoricalResumeCandidateErr(err)
}

func (b *serviceRuntimeBridge) PreviewResumeCandidates(candidates []string, limit int) []string {
	if b.deps.PreviewResumeCandidates == nil {
		return nil
	}
	return b.deps.PreviewResumeCandidates(candidates, limit)
}

func (b *serviceRuntimeBridge) Notify(method string, payload any) {
	if b.deps.Notify == nil {
		return
	}
	b.deps.Notify(method, payload)
}

func (b *serviceRuntimeBridge) NormalizeSkillNames(input []string) ([]string, error) {
	return commonadapter.NormalizeSkillNames(input)
}

func (b *serviceRuntimeBridge) WrapError(err error, caller, message string) error {
	return appErrors.Wrap(err, caller, message)
}

func (b *serviceRuntimeBridge) WrapErrorf(err error, caller, format string, args ...any) error {
	return appErrors.Wrapf(err, caller, format, args...)
}

func (b *serviceRuntimeBridge) Submit(proc serviceruntime.Process, prompt string, images, files []string, outputSchema json.RawMessage) error {
	if b.deps.Submit == nil {
		return nil
	}
	return b.deps.Submit(unwrapServiceRuntimeProcess(proc), prompt, images, files, outputSchema)
}

func (b *serviceRuntimeBridge) ResolveClientActiveTurnID(proc serviceruntime.Process) string {
	if b.deps.ResolveClientActiveTurnID == nil {
		return ""
	}
	return b.deps.ResolveClientActiveTurnID(unwrapServiceRuntimeProcess(proc))
}

func (b *serviceRuntimeBridge) BeginTrackedTurn(threadID, resolvedTurnID string) string {
	if b.deps.BeginTrackedTurn == nil {
		return resolvedTurnID
	}
	return b.deps.BeginTrackedTurn(threadID, resolvedTurnID)
}

func (b *serviceRuntimeBridge) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	if b.deps.TurnSteer == nil {
		return nil, nil
	}
	return b.deps.TurnSteer(threadID, submitPrompt, images, files)
}

func (a *Adapter) registerBinding(ctx context.Context, agentID string, proc *runner.AgentProcess) {
	serviceruntime.RegisterBinding(newServiceRuntimeBridge(a.runtimeConsumerDeps()), ctx, agentID, wrapServiceRuntimeProcess(proc))
}

func BuildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
	return serviceruntime.BuildSessionLostNotification(agentID, lastErr)
}

func (a *Adapter) TurnStart(ctx context.Context, req turnStartRequest) (turnStartEntryResult, error) {
	result, err := serviceruntime.TurnStart(newServiceRuntimeBridge(a.runtimeConsumerDeps()), ctx, toRuntimeTurnStartRequest(req))
	if err != nil {
		return turnStartEntryResult{}, err
	}
	return turnStartEntryResult{TurnID: result.TurnID}, nil
}

func (a *Adapter) TurnSteerFromInput(req turnSteerRequest) (map[string]any, error) {
	return serviceruntime.TurnSteerFromInput(newServiceRuntimeBridge(a.runtimeConsumerDeps()), toRuntimeTurnSteerRequest(req))
}

func (a *Adapter) TurnSteerFromInputAligned(req turnSteerRequest) (map[string]any, error) {
	bridge := newServiceRuntimeBridge(a.runtimeConsumerDeps())
	return serviceruntime.TurnSteerFromInputAlignedByAdapter(
		bridge,
		toRuntimeTurnSteerRequest(req),
		func(runtimeReq serviceruntime.TurnSteerRequest) (map[string]any, error) {
			return serviceruntime.TurnSteerFromInput(bridge, runtimeReq)
		},
	)
}

func (a *Adapter) resolveProcess(caller, threadID string) (*runner.AgentProcess, error) {
	proc, err := serviceruntime.ResolveProcess(newServiceRuntimeBridge(a.runtimeConsumerDeps()), caller, threadID)
	return unwrapServiceRuntimeProcess(proc), err
}

func withProcess[T any](
	a *Adapter,
	caller, threadID string,
	fn func(*runner.AgentProcess) (T, error),
) (T, error) {
	return serviceruntime.WithProcess(newServiceRuntimeBridge(a.runtimeConsumerDeps()), caller, threadID, func(proc serviceruntime.Process) (T, error) {
		return fn(unwrapServiceRuntimeProcess(proc))
	})
}

func toRuntimeTurnInputs(inputs []contracts.TurnInput) []serviceruntime.TurnInput {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]serviceruntime.TurnInput, 0, len(inputs))
	for _, inp := range inputs {
		out = append(out, serviceruntime.TurnInput{
			Type:    inp.Type,
			Text:    inp.Text,
			URL:     inp.URL,
			Path:    inp.Path,
			Name:    inp.Name,
			Content: inp.Content,
		})
	}
	return out
}

func toRuntimeTurnStartRequest(req contracts.TurnStartRequest) serviceruntime.TurnStartRequest {
	return serviceruntime.TurnStartRequest{
		ThreadID:             req.ThreadID,
		Cwd:                  req.Cwd,
		Input:                toRuntimeTurnInputs(req.Input),
		SelectedSkills:       req.SelectedSkills,
		ManualSkillSelection: req.ManualSkillSelection,
		OutputSchema:         req.OutputSchema,
	}
}

func toRuntimeTurnSteerRequest(req contracts.TurnSteerRequest) serviceruntime.TurnSteerRequest {
	return serviceruntime.TurnSteerRequest{
		ThreadID:             req.ThreadID,
		ExpectedTurnID:       req.ExpectedTurnID,
		Input:                toRuntimeTurnInputs(req.Input),
		SelectedSkills:       req.SelectedSkills,
		ManualSkillSelection: req.ManualSkillSelection,
	}
}

func fromRuntimeTimelineAttachments(in []serviceruntime.TimelineAttachment) []uistate.TimelineAttachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]uistate.TimelineAttachment, 0, len(in))
	for _, item := range in {
		out = append(out, uistate.TimelineAttachment{
			Kind:       item.Kind,
			Name:       item.Name,
			Path:       item.Path,
			PreviewURL: item.PreviewURL,
		})
	}
	return out
}

func fromRuntimeAutoMatchInputs(inputs []serviceruntime.AutoMatchInput) []contracts.AutoMatchInput {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]contracts.AutoMatchInput, 0, len(inputs))
	for _, inp := range inputs {
		out = append(out, contracts.AutoMatchInput{
			Type: inp.Type,
			Name: inp.Name,
		})
	}
	return out
}

func toRuntimeSkillMatchCandidates(candidates []contracts.SkillMatchCandidate) []serviceruntime.SkillMatchCandidate {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]serviceruntime.SkillMatchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, serviceruntime.SkillMatchCandidate{
			Name:         candidate.Name,
			ForceWords:   candidate.ForceWords,
			TriggerWords: candidate.TriggerWords,
		})
	}
	return out
}

func fromRuntimeSkillMatchCandidates(candidates []serviceruntime.SkillMatchCandidate) []contracts.SkillMatchCandidate {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]contracts.SkillMatchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, contracts.SkillMatchCandidate{
			Name:         candidate.Name,
			ForceWords:   candidate.ForceWords,
			TriggerWords: candidate.TriggerWords,
		})
	}
	return out
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
	if len(matches) == 0 {
		return nil
	}
	out := make([]serviceruntime.AutoMatchedSkillMatch, 0, len(matches))
	for _, match := range matches {
		out = append(out, serviceruntime.AutoMatchedSkillMatch{
			Name:         match.Name,
			MatchedBy:    match.MatchedBy,
			MatchedTerms: match.MatchedTerms,
		})
	}
	return out
}

func fromRuntimeAutoMatchedSkillMatches(matches []serviceruntime.AutoMatchedSkillMatch) []contracts.AutoMatchedSkillMatch {
	if len(matches) == 0 {
		return nil
	}
	out := make([]contracts.AutoMatchedSkillMatch, 0, len(matches))
	for _, match := range matches {
		out = append(out, contracts.AutoMatchedSkillMatch{
			Name:         match.Name,
			MatchedBy:    match.MatchedBy,
			MatchedTerms: match.MatchedTerms,
		})
	}
	return out
}

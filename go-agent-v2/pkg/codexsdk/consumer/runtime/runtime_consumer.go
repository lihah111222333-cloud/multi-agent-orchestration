package runtime

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	serviceruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	defaultLSPUsagePromptHint = ""
	maxLSPUsagePromptHintLen  = 16000
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

func normalizeDeps(deps Deps) Deps {
	d := deps
	if d.BuildSelectedSkillPrompt == nil {
		d.BuildSelectedSkillPrompt = func([]string) (string, int) { return "", 0 }
	}
	if d.ListSkillMatchCandidates == nil {
		d.ListSkillMatchCandidates = func() ([]contracts.SkillMatchCandidate, error) { return nil, nil }
	}
	if d.ListAgentSkills == nil {
		d.ListAgentSkills = func(string) []string { return nil }
	}
	if d.CollectAutoMatchedSkillMatch == nil {
		d.CollectAutoMatchedSkillMatch = func(string, []contracts.AutoMatchInput, []string, []contracts.SkillMatchCandidate, contracts.AutoSkillMatchOptions) []contracts.AutoMatchedSkillMatch {
			return nil
		}
	}
	if d.RenderAutoMatchedSkillPrompt == nil {
		d.RenderAutoMatchedSkillPrompt = func(string, []contracts.AutoMatchedSkillMatch) (string, int) { return "", 0 }
	}
	if d.ActiveTrackedTurnID == nil {
		d.ActiveTrackedTurnID = func(string) (string, bool) { return "", false }
	}
	if d.ShowInjectedPromptInChat == nil {
		d.ShowInjectedPromptInChat = func(context.Context) bool { return false }
	}
	if d.ResolveLSPUsagePromptHint == nil {
		d.ResolveLSPUsagePromptHint = func(_ context.Context, defaultHint string, _ int) string { return defaultHint }
	}
	if d.ThreadExistsInHistory == nil {
		d.ThreadExistsInHistory = func(context.Context, string) bool { return false }
	}
	if d.AllDynamicToolSchemas == nil {
		d.AllDynamicToolSchemas = func() []agentcore.DynamicTool { return nil }
	}
	if d.ResolveStartInstructions == nil {
		d.ResolveStartInstructions = func(context.Context, []agentcore.DynamicTool) string { return "" }
	}
	if d.SetAgentWorkDir == nil {
		d.SetAgentWorkDir = func(string, string) {}
	}
	if d.GetThreadID == nil {
		d.GetThreadID = func(*runner.AgentProcess) string { return "" }
	}
	if d.CancelCodeRuns == nil {
		d.CancelCodeRuns = func(string) int { return 0 }
	}
	if d.ResolveCodexThreadCandidates == nil {
		d.ResolveCodexThreadCandidates = func(context.Context, string) []string { return nil }
	}
	if d.ResumeThread == nil {
		d.ResumeThread = func(*runner.AgentProcess, agentcore.ResumeThreadRequest) error {
			return appErrors.New("Server.ensureThreadReady", "thread process is not available")
		}
	}
	if d.IsCodexProcessCrashError == nil {
		d.IsCodexProcessCrashError = func(error) bool { return false }
	}
	if d.IsHistoricalResumeCandidateErr == nil {
		d.IsHistoricalResumeCandidateErr = func(error) bool { return false }
	}
	if d.PreviewResumeCandidates == nil {
		d.PreviewResumeCandidates = func(candidates []string, _ int) []string {
			return append([]string(nil), candidates...)
		}
	}
	if d.Notify == nil {
		d.Notify = func(string, any) {}
	}
	if d.Submit == nil {
		d.Submit = func(*runner.AgentProcess, string, []string, []string, json.RawMessage) error {
			return appErrors.New("Server.turnStart", "thread process is not available")
		}
	}
	if d.ResolveClientActiveTurnID == nil {
		d.ResolveClientActiveTurnID = func(*runner.AgentProcess) string { return "" }
	}
	if d.BeginTrackedTurn == nil {
		d.BeginTrackedTurn = func(_ string, resolvedTurnID string) string { return strings.TrimSpace(resolvedTurnID) }
	}
	if d.TurnSteer == nil {
		d.TurnSteer = func(string, string, []string, []string) (map[string]any, error) {
			return nil, appErrors.New("Server.turnSteer", "turn steer is not configured")
		}
	}
	return d
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
	timeline := u.uiRuntime.ThreadTimeline(threadID)
	if len(timeline) == 0 {
		return nil
	}
	items := make([]serviceruntime.TimelineItem, 0, len(timeline))
	for _, item := range timeline {
		items = append(items, serviceruntime.TimelineItem{Kind: item.Kind, Text: item.Text})
	}
	return items
}

type serviceRuntimeBridge struct {
	deps Deps
}

func newServiceRuntimeBridge(deps Deps) *serviceRuntimeBridge {
	return &serviceRuntimeBridge{deps: normalizeDeps(deps)}
}

func (b *serviceRuntimeBridge) MergePromptText(left, right string) string {
	return commonadapter.MergePromptText(left, right)
}

func (b *serviceRuntimeBridge) FileContentInputText(name, content string) string {
	return commonadapter.FileContentInputText(name, content)
}

func (b *serviceRuntimeBridge) BuildAttachmentName(path string) string {
	return BuildAttachmentName(path)
}

func (b *serviceRuntimeBridge) BuildAttachmentPreviewURL(path string) string {
	return BuildAttachmentPreviewURL(path)
}

func (b *serviceRuntimeBridge) BuildSelectedSkillPrompt(selectedSkills []string) (string, int) {
	return b.deps.BuildSelectedSkillPrompt(selectedSkills)
}

func (b *serviceRuntimeBridge) ListSkillMatchCandidates() ([]serviceruntime.SkillMatchCandidate, error) {
	candidates, err := b.deps.ListSkillMatchCandidates()
	if err != nil {
		return nil, err
	}
	return toRuntimeSkillMatchCandidates(candidates), nil
}

func (b *serviceRuntimeBridge) ListAgentSkills(agentID string) []string {
	return b.deps.ListAgentSkills(agentID)
}

func (b *serviceRuntimeBridge) CollectAutoMatchedSkillMatches(
	prompt string,
	inputs []serviceruntime.AutoMatchInput,
	configuredSkillNames []string,
	candidates []serviceruntime.SkillMatchCandidate,
	options serviceruntime.AutoSkillMatchOptions,
) []serviceruntime.AutoMatchedSkillMatch {
	results := b.deps.CollectAutoMatchedSkillMatch(
		prompt,
		fromRuntimeAutoMatchInputs(inputs),
		configuredSkillNames,
		fromRuntimeSkillMatchCandidates(candidates),
		fromRuntimeAutoSkillMatchOptions(options),
	)
	return toRuntimeAutoMatchedSkillMatches(results)
}

func (b *serviceRuntimeBridge) RenderAutoMatchedSkillPrompt(agentID string, matches []serviceruntime.AutoMatchedSkillMatch) (string, int) {
	return b.deps.RenderAutoMatchedSkillPrompt(agentID, fromRuntimeAutoMatchedSkillMatches(matches))
}

func (b *serviceRuntimeBridge) ActiveTrackedTurnID(threadID string) (string, bool) {
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
	return b.deps.ShowInjectedPromptInChat(ctx)
}

func (b *serviceRuntimeBridge) ResolveLSPUsagePromptHint(ctx context.Context, defaultHint string, maxHintLen int) string {
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
	return &serviceRuntimeUIRuntime{uiRuntime: b.deps.UIRuntime}
}

func (b *serviceRuntimeBridge) Manager() serviceruntime.Manager {
	if b.deps.Manager == nil {
		return nil
	}
	return &serviceRuntimeManager{manager: b.deps.Manager}
}

func (b *serviceRuntimeBridge) ThreadExistsInHistory(ctx context.Context, threadID string) bool {
	return b.deps.ThreadExistsInHistory(ctx, threadID)
}

func (b *serviceRuntimeBridge) AllDynamicToolSchemas() []agentcore.DynamicTool {
	return b.deps.AllDynamicToolSchemas()
}

func (b *serviceRuntimeBridge) ResolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []agentcore.DynamicTool) string {
	return b.deps.ResolveStartInstructions(ctx, dynamicTools)
}

func (b *serviceRuntimeBridge) SetAgentWorkDir(agentID, cwd string) {
	b.deps.SetAgentWorkDir(agentID, cwd)
}

func (b *serviceRuntimeBridge) ThreadLogFields(threadID string) []any {
	return threadLogFields(threadID)
}

func (b *serviceRuntimeBridge) GetThreadID(proc serviceruntime.Process) string {
	return b.deps.GetThreadID(unwrapServiceRuntimeProcess(proc))
}

func (b *serviceRuntimeBridge) CancelCodeRuns(agentID string) int {
	return b.deps.CancelCodeRuns(agentID)
}

func (b *serviceRuntimeBridge) BindingStore() serviceruntime.BindingStore {
	if b.deps.BindingStore == nil {
		return nil
	}
	return &serviceRuntimeBindingStore{store: b.deps.BindingStore}
}

func (b *serviceRuntimeBridge) ResolveCodexThreadCandidates(ctx context.Context, agentID string) []string {
	return b.deps.ResolveCodexThreadCandidates(ctx, agentID)
}

func (b *serviceRuntimeBridge) ResumeThread(proc serviceruntime.Process, req serviceruntime.ResumeThreadRequest) error {
	resolved := unwrapServiceRuntimeProcess(proc)
	if resolved == nil {
		return appErrors.New("Server.ensureThreadReady", "thread process is not available")
	}
	return b.deps.ResumeThread(resolved, agentcore.ResumeThreadRequest{ThreadID: req.ThreadID, Cwd: req.Cwd})
}

func (b *serviceRuntimeBridge) IsCodexProcessCrashError(err error) bool {
	return b.deps.IsCodexProcessCrashError(err)
}

func (b *serviceRuntimeBridge) IsHistoricalResumeCandidateError(err error) bool {
	return b.deps.IsHistoricalResumeCandidateErr(err)
}

func (b *serviceRuntimeBridge) PreviewResumeCandidates(candidates []string, limit int) []string {
	return b.deps.PreviewResumeCandidates(candidates, limit)
}

func (b *serviceRuntimeBridge) Notify(method string, payload any) {
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
	resolved := unwrapServiceRuntimeProcess(proc)
	if resolved == nil {
		return appErrors.New("Server.turnStart", "thread process is not available")
	}
	return b.deps.Submit(resolved, prompt, images, files, outputSchema)
}

func (b *serviceRuntimeBridge) ResolveClientActiveTurnID(proc serviceruntime.Process) string {
	return b.deps.ResolveClientActiveTurnID(unwrapServiceRuntimeProcess(proc))
}

func (b *serviceRuntimeBridge) BeginTrackedTurn(threadID, resolvedTurnID string) string {
	return b.deps.BeginTrackedTurn(threadID, resolvedTurnID)
}

func (b *serviceRuntimeBridge) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	return b.deps.TurnSteer(threadID, submitPrompt, images, files)
}

func RegisterBinding(ctx context.Context, deps Deps, agentID string, proc *runner.AgentProcess) {
	serviceruntime.RegisterBinding(newServiceRuntimeBridge(deps), ctx, agentID, wrapServiceRuntimeProcess(proc))
}

func BuildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
	return serviceruntime.BuildSessionLostNotification(agentID, lastErr)
}

func TurnStart(ctx context.Context, deps Deps, req contracts.TurnStartRequest) (TurnStartEntryResult, error) {
	result, err := serviceruntime.TurnStart(newServiceRuntimeBridge(deps), ctx, toRuntimeTurnStartRequest(req))
	if err != nil {
		return TurnStartEntryResult{}, err
	}
	return TurnStartEntryResult{TurnID: result.TurnID}, nil
}

func TurnSteerFromInput(deps Deps, req contracts.TurnSteerRequest) (map[string]any, error) {
	return serviceruntime.TurnSteerFromInput(newServiceRuntimeBridge(deps), toRuntimeTurnSteerRequest(req))
}

func TurnSteerFromInputAligned(deps Deps, req contracts.TurnSteerRequest) (map[string]any, error) {
	return serviceruntime.TurnSteerFromInputAlignedByAdapter(
		newServiceRuntimeBridge(deps),
		toRuntimeTurnSteerRequest(req),
		func(runtimeReq serviceruntime.TurnSteerRequest) (map[string]any, error) {
			return TurnSteerFromInput(deps, fromRuntimeTurnSteerRequest(runtimeReq))
		},
	)
}

func ResolveProcess(deps Deps, caller, threadID string) (*runner.AgentProcess, error) {
	proc, err := serviceruntime.ResolveProcess(newServiceRuntimeBridge(deps), caller, threadID)
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
	return serviceruntime.WithProcess(newServiceRuntimeBridge(deps), caller, threadID, func(proc serviceruntime.Process) (T, error) {
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
		newServiceRuntimeBridge(deps),
		threadID,
		prompt,
		toRuntimeTurnInputs(input),
		toRuntimeAutoSkillMatchOptions(options),
	)
	return fromRuntimeAutoMatchedSkillMatches(matches)
}

func toRuntimeTurnInputs(inputs []contracts.TurnInput) []serviceruntime.TurnInput {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]serviceruntime.TurnInput, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, serviceruntime.TurnInput{
			Type:    input.Type,
			Text:    input.Text,
			URL:     input.URL,
			Path:    input.Path,
			Name:    input.Name,
			Content: input.Content,
		})
	}
	return out
}

func toRuntimeTurnStartRequest(req contracts.TurnStartRequest) serviceruntime.TurnStartRequest {
	return serviceruntime.TurnStartRequest{
		ThreadID:             req.ThreadID,
		Cwd:                  req.Cwd,
		Input:                toRuntimeTurnInputs(req.Input),
		SelectedSkills:       append([]string(nil), req.SelectedSkills...),
		ManualSkillSelection: req.ManualSkillSelection,
		OutputSchema:         req.OutputSchema,
	}
}

func toRuntimeTurnSteerRequest(req contracts.TurnSteerRequest) serviceruntime.TurnSteerRequest {
	return serviceruntime.TurnSteerRequest{
		ThreadID:             req.ThreadID,
		ExpectedTurnID:       req.ExpectedTurnID,
		Input:                toRuntimeTurnInputs(req.Input),
		SelectedSkills:       append([]string(nil), req.SelectedSkills...),
		ManualSkillSelection: req.ManualSkillSelection,
	}
}

func fromRuntimeTurnSteerRequest(req serviceruntime.TurnSteerRequest) contracts.TurnSteerRequest {
	return contracts.TurnSteerRequest{
		ThreadID:             req.ThreadID,
		ExpectedTurnID:       req.ExpectedTurnID,
		Input:                fromRuntimeTurnInputs(req.Input),
		SelectedSkills:       append([]string(nil), req.SelectedSkills...),
		ManualSkillSelection: req.ManualSkillSelection,
	}
}

func fromRuntimeTurnInputs(inputs []serviceruntime.TurnInput) []contracts.TurnInput {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]contracts.TurnInput, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, contracts.TurnInput{
			Type:    input.Type,
			Text:    input.Text,
			URL:     input.URL,
			Path:    input.Path,
			Name:    input.Name,
			Content: input.Content,
		})
	}
	return out
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
	for _, input := range inputs {
		out = append(out, contracts.AutoMatchInput{Type: input.Type, Name: input.Name})
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
			ForceWords:   append([]string(nil), candidate.ForceWords...),
			TriggerWords: append([]string(nil), candidate.TriggerWords...),
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
			ForceWords:   append([]string(nil), candidate.ForceWords...),
			TriggerWords: append([]string(nil), candidate.TriggerWords...),
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
			MatchedTerms: append([]string(nil), match.MatchedTerms...),
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
			MatchedTerms: append([]string(nil), match.MatchedTerms...),
		})
	}
	return out
}

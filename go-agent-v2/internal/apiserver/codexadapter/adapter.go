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
	serviceruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/runtime"
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
	adapter *Adapter
}

func newServiceRuntimeBridge(a *Adapter) *serviceRuntimeBridge {
	return &serviceRuntimeBridge{adapter: a}
}

func (b *serviceRuntimeBridge) MergePromptText(left, right string) string {
	return commonadapter.MergePromptText(left, right)
}

func (b *serviceRuntimeBridge) FileContentInputText(name, content string) string {
	return commonadapter.FileContentInputText(name, content)
}

func (b *serviceRuntimeBridge) BuildSelectedSkillPrompt(selectedSkills []string) (string, int) {
	return b.adapter.BuildSelectedSkillPrompt(selectedSkills)
}

func (b *serviceRuntimeBridge) ListSkillMatchCandidates() ([]serviceruntime.SkillMatchCandidate, error) {
	candidates, err := b.adapter.listSkillMatchCandidates()
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	out := make([]serviceruntime.SkillMatchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, serviceruntime.SkillMatchCandidate{
			Name:         candidate.Name,
			ForceWords:   append([]string(nil), candidate.ForceWords...),
			TriggerWords: append([]string(nil), candidate.TriggerWords...),
		})
	}
	return out, nil
}

func (b *serviceRuntimeBridge) ListAgentSkills(agentID string) []string {
	return b.adapter.listAgentSkills(agentID)
}

func (b *serviceRuntimeBridge) CollectAutoMatchedSkillMatches(
	prompt string,
	inputs []serviceruntime.AutoMatchInput,
	configuredSkillNames []string,
	candidates []serviceruntime.SkillMatchCandidate,
	options serviceruntime.AutoSkillMatchOptions,
) []serviceruntime.AutoMatchedSkillMatch {
	results := b.adapter.CollectAutoMatchedSkillMatches(
		prompt,
		fromRuntimeAutoMatchInputs(inputs),
		configuredSkillNames,
		fromRuntimeSkillMatchCandidates(candidates),
		fromRuntimeAutoSkillMatchOptions(options),
	)
	return toRuntimeAutoMatchedSkillMatches(results)
}

func (b *serviceRuntimeBridge) RenderAutoMatchedSkillPrompt(agentID string, matches []serviceruntime.AutoMatchedSkillMatch) (string, int) {
	return b.adapter.RenderAutoMatchedSkillPrompt(agentID, fromRuntimeAutoMatchedSkillMatches(matches))
}

func (b *serviceRuntimeBridge) ActiveTrackedTurnID(threadID string) (string, bool) {
	return b.adapter.activeTrackedTurnID(threadID)
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
	return b.adapter.showInjectedPromptInChat(ctx)
}

func (b *serviceRuntimeBridge) ResolveLSPUsagePromptHint(ctx context.Context, defaultHint string, maxHintLen int) string {
	return b.adapter.ResolveLSPUsagePromptHint(ctx, defaultHint, maxHintLen)
}

func (b *serviceRuntimeBridge) DefaultLSPUsagePromptHint() string {
	return defaultLSPUsagePromptHint
}

func (b *serviceRuntimeBridge) MaxLSPUsagePromptHintLen() int {
	return maxLSPUsagePromptHintLen
}

func (b *serviceRuntimeBridge) UIRuntime() serviceruntime.TimelineRuntime {
	ui := b.adapter.uiRuntime()
	if ui == nil {
		return nil
	}
	return &serviceRuntimeUIRuntime{uiRuntime: ui}
}

func (b *serviceRuntimeBridge) Manager() serviceruntime.Manager {
	manager := b.adapter.manager()
	if manager == nil {
		return nil
	}
	return &serviceRuntimeManager{manager: manager}
}

func (b *serviceRuntimeBridge) ThreadExistsInHistory(ctx context.Context, threadID string) bool {
	return b.adapter.ThreadExistsInHistory(ctx, threadID)
}

func (b *serviceRuntimeBridge) AllDynamicToolSchemas() []agentcore.DynamicTool {
	return b.adapter.allDynamicToolSchemas()
}

func (b *serviceRuntimeBridge) ResolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []agentcore.DynamicTool) string {
	return b.adapter.resolveStartInstructionsForLaunch(ctx, dynamicTools)
}

func (b *serviceRuntimeBridge) SetAgentWorkDir(agentID, cwd string) {
	b.adapter.setAgentWorkDir(agentID, cwd)
}

func (b *serviceRuntimeBridge) ThreadLogFields(threadID string) []any {
	return threadLogFields(threadID)
}

func (b *serviceRuntimeBridge) GetThreadID(proc serviceruntime.Process) string {
	return b.adapter.GetThreadID(unwrapServiceRuntimeProcess(proc))
}

func (b *serviceRuntimeBridge) CancelCodeRuns(agentID string) int {
	return b.adapter.cancelCodeRuns(agentID)
}

func (b *serviceRuntimeBridge) BindingStore() serviceruntime.BindingStore {
	bindingStore := b.adapter.bindingStore()
	if bindingStore == nil {
		return nil
	}
	return &serviceRuntimeBindingStore{store: bindingStore}
}

func (b *serviceRuntimeBridge) ResolveCodexThreadCandidates(ctx context.Context, agentID string) []string {
	return b.adapter.ResolveCodexThreadCandidates(ctx, agentID, appendUniqueThreadIDFallback, PreviewResumeCandidates)
}

func (b *serviceRuntimeBridge) ResumeThread(proc serviceruntime.Process, req serviceruntime.ResumeThreadRequest) error {
	resolved := unwrapServiceRuntimeProcess(proc)
	if resolved == nil {
		return appErrors.New("Server.ensureThreadReady", "thread process is not available")
	}
	return b.adapter.ResumeThread(resolved, agentcore.ResumeThreadRequest{ThreadID: req.ThreadID, Cwd: req.Cwd})
}

func (b *serviceRuntimeBridge) IsCodexProcessCrashError(err error) bool {
	return IsCodexProcessCrashError(err)
}

func (b *serviceRuntimeBridge) IsHistoricalResumeCandidateError(err error) bool {
	return IsHistoricalResumeCandidateError(err)
}

func (b *serviceRuntimeBridge) PreviewResumeCandidates(candidates []string, limit int) []string {
	return PreviewResumeCandidates(candidates, limit)
}

func (b *serviceRuntimeBridge) Notify(method string, payload any) {
	b.adapter.notify(method, payload)
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
	return b.adapter.Submit(resolved, prompt, images, files, outputSchema)
}

func (b *serviceRuntimeBridge) ResolveClientActiveTurnID(proc serviceruntime.Process) string {
	resolved := unwrapServiceRuntimeProcess(proc)
	if resolved == nil {
		return ""
	}
	return resolveClientActiveTurnID(resolved.Client)
}

func (b *serviceRuntimeBridge) BeginTrackedTurn(threadID, resolvedTurnID string) string {
	return b.adapter.beginTrackedTurn(threadID, resolvedTurnID)
}

func (b *serviceRuntimeBridge) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	return b.adapter.TurnSteer(threadID, submitPrompt, images, files)
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

func toRuntimeTimelineAttachments(in []uistate.TimelineAttachment) []serviceruntime.TimelineAttachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]serviceruntime.TimelineAttachment, 0, len(in))
	for _, item := range in {
		out = append(out, serviceruntime.TimelineAttachment{
			Kind:       item.Kind,
			Name:       item.Name,
			Path:       item.Path,
			PreviewURL: item.PreviewURL,
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

func toRuntimeAutoMatchInputs(inputs []autoMatchInput) []serviceruntime.AutoMatchInput {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]serviceruntime.AutoMatchInput, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, serviceruntime.AutoMatchInput{Type: input.Type, Name: input.Name})
	}
	return out
}

func fromRuntimeAutoMatchInputs(inputs []serviceruntime.AutoMatchInput) []autoMatchInput {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]autoMatchInput, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, autoMatchInput{Type: input.Type, Name: input.Name})
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

func toRuntimeAutoMatchedSkillMatches(matches []autoMatchedSkillMatch) []serviceruntime.AutoMatchedSkillMatch {
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

func fromRuntimeAutoMatchedSkillMatches(matches []serviceruntime.AutoMatchedSkillMatch) []autoMatchedSkillMatch {
	if len(matches) == 0 {
		return nil
	}
	out := make([]autoMatchedSkillMatch, 0, len(matches))
	for _, match := range matches {
		out = append(out, autoMatchedSkillMatch{
			Name:         match.Name,
			MatchedBy:    match.MatchedBy,
			MatchedTerms: append([]string(nil), match.MatchedTerms...),
		})
	}
	return out
}

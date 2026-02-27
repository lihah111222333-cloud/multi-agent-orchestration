package codexadapter
import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	archiveconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/archive"
	commandconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/command"
	historyconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/history"
	interruptconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/interrupt"
	lifecycleconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/lifecycle"
	listingconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/listing"
	promptconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/prompt"
	consumerruntime "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/runtime"
	trackerconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/tracker"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)
const (
	defaultLSPUsagePromptHint = ""
	maxLSPUsagePromptHintLen  = 16000
)
const (
	DefaultTurnWatchdogTimeout        = trackerconsumer.DefaultTurnWatchdogTimeout
	DefaultTrackedTurnSummaryTTL      = trackerconsumer.DefaultTrackedTurnSummaryTTL
	TrackedTurnSummaryCacheMaxEntries = trackerconsumer.TrackedTurnSummaryCacheMaxEntries
	defaultStallThreshold             = trackerconsumer.DefaultStallThreshold
	defaultStallHeartbeat             = trackerconsumer.DefaultStallHeartbeat
)
type (
	trackedTurn                  = trackerconsumer.TrackedTurn
	trackedTurnTransitionRequest = trackerconsumer.TrackedTurnTransitionRequest
	trackedTurnTransitionResult  = trackerconsumer.TrackedTurnTransitionResult
	trackedTurnSummaryCacheEntry = trackerconsumer.TrackedTurnSummaryCacheEntry
	turnTrackerState             = trackerconsumer.TurnTrackerState
)
const prefThreadArchivesChat = "threadArchives.chat"
type Deps struct {
	Manager                  *codexsdk.AgentManager
	Store                    *uistate.PreferenceManager
	BindingStore             *store.AgentCodexBindingStore
	AgentStatusStore         *store.AgentStatusStore
	UIRuntime                *uistate.RuntimeManager
	AllSchemas               func() []codexsdk.DynamicTool
	NowUnixMilli             func() int64
	SetAgentWorkDir          func(agentID, cwd string)
	CancelCodeRuns           func(agentID string) int
	ReadSkillContent         func(skillName string) (string, error)
	ListSkillNames           func() ([]string, error)
	ListSkillMatchCandidates func() ([]contracts.SkillMatchCandidate, error)
	GetAgentSkills           func(agentID string) []string
	Notify                   func(method string, params any)
}
func defaultAllSchemaProvider() []codexsdk.DynamicTool { return nil }
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
	consumerruntime.SetDefaultFunc(&d.AllSchemas, defaultAllSchemaProvider)
	consumerruntime.SetDefaultFunc(&d.NowUnixMilli, defaultNowUnixMilliProvider)
	consumerruntime.SetDefaultFunc(&d.SetAgentWorkDir, defaultSetAgentWorkDirProvider)
	consumerruntime.SetDefaultFunc(&d.CancelCodeRuns, defaultCancelCodeRunsProvider)
	consumerruntime.SetDefaultFunc(&d.ReadSkillContent, defaultReadSkillContentProvider)
	consumerruntime.SetDefaultFunc(&d.ListSkillNames, defaultListSkillNamesProvider)
	consumerruntime.SetDefaultFunc(&d.ListSkillMatchCandidates, defaultListSkillMatchCandidatesProvider)
	consumerruntime.SetDefaultFunc(&d.GetAgentSkills, defaultGetAgentSkillsProvider)
	consumerruntime.SetDefaultFunc(&d.Notify, defaultNotifyProvider)
	return &d
}
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
		StallThreshold:      &a.trackerStallThreshold,
		StallHeartbeat:      &a.trackerStallHeartbeat,
	}
}
func (a *Adapter) Context() *Deps {
	if a == nil {
		return nil
	}
	return a.ctx
}
func (a *Adapter) depsOrDefault() *Deps {
	if deps := a.Context(); deps != nil {
		return deps
	}
	return normalizeDeps(Deps{})
}
func (a *Adapter) store() *uistate.PreferenceManager { return a.depsOrDefault().Store }
func (a *Adapter) manager() *codexsdk.AgentManager { return a.depsOrDefault().Manager }
func (a *Adapter) bindingStore() *store.AgentCodexBindingStore { return a.depsOrDefault().BindingStore }
func (a *Adapter) statusStore() *store.AgentStatusStore { return a.depsOrDefault().AgentStatusStore }
func (a *Adapter) uiRuntime() *uistate.RuntimeManager { return a.depsOrDefault().UIRuntime }
func (a *Adapter) allDynamicToolSchemas() []codexsdk.DynamicTool { return a.depsOrDefault().AllSchemas() }
func (a *Adapter) setAgentWorkDir(agentID string, cwd string) { a.depsOrDefault().SetAgentWorkDir(agentID, cwd) }
func (a *Adapter) cancelCodeRuns(agentID string) int { return a.depsOrDefault().CancelCodeRuns(agentID) }
func (a *Adapter) nowUnixMilli() int64 { return a.depsOrDefault().NowUnixMilli() }
func (a *Adapter) notify(method string, payload any) {
	if strings.TrimSpace(method) == "" {
		return
	}
	a.depsOrDefault().Notify(method, payload)
}
func (a *Adapter) bindingExists(ctx context.Context, agentID string) (bool, error) { return historyconsumer.BindingExistsByAgentID(ctx, a.bindingStore(), agentID) }
func (a *Adapter) agentStatusExists(ctx context.Context, agentID string) (bool, error) { return historyconsumer.AgentStatusExistsByID(ctx, a.statusStore(), agentID) }
func (a *Adapter) bindingCodexThreadID(ctx context.Context, agentID string) (string, error) { return historyconsumer.BindingCodexThreadIDByAgentID(ctx, a.bindingStore(), agentID) }
func (a *Adapter) statusSessionID(ctx context.Context, agentID string) (string, error) { return historyconsumer.StatusSessionIDByAgentID(ctx, a.statusStore(), agentID) }
func asAgentProcess(proc any) *codexsdk.AgentProcess { typed, _ := proc.(*codexsdk.AgentProcess); return typed }
func (a *Adapter) storeGetter() func(context.Context, string) (any, error) {
	if store := a.store(); store != nil {
		return store.Get
	}
	return nil
}
func (a *Adapter) resolveThreadFromSlashCommand(
	ctx context.Context,
	threadID string,
	requireThreadID bool,
) (string, error) {
	return commandconsumer.ResolveThreadForSlashCommandLogic(ctx, threadID, requireThreadID, func(ctx context.Context) ([]commandconsumer.ThreadListItem, error) {
		items, err := a.ThreadList(ctx)
		if err != nil {
			return nil, err
		}
		return commandconsumer.ToThreadListItems(items), nil
	})
}
func (a *Adapter) withProcessMap(methodName string, threadID string, fn func(any) (map[string]any, error)) (map[string]any, error) {
	return withProcess(a, methodName, threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) { return fn(proc) })
}
func (a *Adapter) sendCommandFromAny(proc any, command, args string) error { return a.SendCommand(asAgentProcess(proc), command, args) }
func (a *Adapter) submitFromAny(proc any, prompt string, images, files []string, outputSchema json.RawMessage) error { return a.Submit(asAgentProcess(proc), prompt, images, files, outputSchema) }
func (a *Adapter) resumeThreadFromAny(proc any, req codexsdk.ResumeThreadRequest) error { return a.ResumeThread(asAgentProcess(proc), req) }
func (a *Adapter) forkThreadFromAny(proc any, req codexsdk.ForkThreadRequest) (*codexsdk.ForkThreadResponse, error) { return a.ForkThread(asAgentProcess(proc), req) }
func (a *Adapter) listThreadsFromAny(proc any) ([]codexsdk.ThreadInfo, error) { return a.ListThreads(asAgentProcess(proc)) }
func (a *Adapter) sendInterruptFromAny(proc any) (bool, error) { return a.sendInterruptCommand(asAgentProcess(proc)) }
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
var errNoProcess = errors.New("codexadapter: agent process not found")
func withClientE[T any](proc *codexsdk.AgentProcess, fn func(codexsdk.Client) (T, error)) (T, error) {
	var zero T
	if proc == nil || proc.Client == nil {
		return zero, errNoProcess
	}
	return fn(proc.Client)
}
func withClient(proc *codexsdk.AgentProcess, fn func(codexsdk.Client) error) error { _, err := withClientE(proc, func(c codexsdk.Client) (struct{}, error) { return struct{}{}, fn(c) }); return err }
func (a *Adapter) Submit(proc *codexsdk.AgentProcess, prompt string, images, files []string, outputSchema json.RawMessage) error {
	return withClient(proc, func(c codexsdk.Client) error { return c.Submit(prompt, images, files, outputSchema) })
}
func (a *Adapter) SendCommand(proc *codexsdk.AgentProcess, command string, args string) error {
	return withClient(proc, func(c codexsdk.Client) error { return c.SendCommand(command, args) })
}
func (a *Adapter) GetThreadID(proc *codexsdk.AgentProcess) string {
	if proc == nil || proc.Client == nil {
		return ""
	}
	return strings.TrimSpace(proc.Client.GetThreadID())
}
func (a *Adapter) ResumeThread(proc *codexsdk.AgentProcess, req codexsdk.ResumeThreadRequest) error { return withClient(proc, func(c codexsdk.Client) error { return c.ResumeThread(req) }) }
func (a *Adapter) ListThreads(proc *codexsdk.AgentProcess) ([]codexsdk.ThreadInfo, error) {
	return withClientE(proc, func(c codexsdk.Client) ([]codexsdk.ThreadInfo, error) { return c.ListThreads() })
}
func (a *Adapter) ForkThread(proc *codexsdk.AgentProcess, req codexsdk.ForkThreadRequest) (*codexsdk.ForkThreadResponse, error) {
	return withClientE(proc, func(c codexsdk.Client) (*codexsdk.ForkThreadResponse, error) { return c.ForkThread(req) })
}
func (a *Adapter) RespondError(proc *codexsdk.AgentProcess, id int64, code int, message string) error {
	return withClient(proc, func(c codexsdk.Client) error { return c.RespondError(id, code, message) })
}
func (a *Adapter) SendDynamicToolResult(proc *codexsdk.AgentProcess, callID, output string, requestID *int64) error {
	return withClient(proc, func(c codexsdk.Client) error { return c.SendDynamicToolResult(callID, output, requestID) })
}
func (a *Adapter) resolveStartInstructionsForLaunch(ctx context.Context, dynamicTools []codexsdk.DynamicTool) string {
	hint := promptconsumer.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen, a.storeGetter())
	startInstructions, warnings := promptconsumer.PrependLSPAvailabilityWarning(
		hint,
		promptconsumer.CollectDynamicToolNames(dynamicTools),
		promptconsumer.CollectReferencedLSPToolNames,
		commonadapter.MergePromptText,
	)
	if len(warnings) > 0 {
		logger.Warn("codexadapter: start instructions warnings: " + strings.Join(warnings, "; "))
	}
	return startInstructions
}
func (a *Adapter) ResolveLSPUsagePromptHint(ctx context.Context, defaultHint string, maxHintLen int) string {
	return promptconsumer.ResolveLSPUsagePromptHint(ctx, defaultHint, maxHintLen, a.storeGetter())
}
func (a *Adapter) readSkillContent(skillName string) (string, error) {
	if strings.TrimSpace(skillName) == "" {
		return "", appErrors.New("codexadapter.readSkillContent", "skill name is required")
	}
	if deps := a.Context(); deps != nil {
		return deps.ReadSkillContent(skillName)
	}
	return "", appErrors.New("codexadapter.readSkillContent", "server context is not configured")
}
func (a *Adapter) buildSelectedSkillPrompt(selectedSkills []string) (string, int) {
	return promptconsumer.BuildSelectedSkillPrompt(selectedSkills, a.readSkillContent, commonadapter.SkillInputText)
}
func (a *Adapter) renderAutoMatchedSkillPrompt(agentID string, matches []contracts.AutoMatchedSkillMatch) (string, int) {
	return promptconsumer.RenderAutoMatchedSkillPrompt(agentID, matches, a.readSkillContent, commonadapter.MergePromptText, commonadapter.SkillInputText)
}
func (a *Adapter) resolveCodexThreadCandidatesForRuntime(ctx context.Context, agentID string) []string { return a.ResolveCodexThreadCandidates(ctx, agentID, lifecycleconsumer.AppendUniqueThreadIDFallback, lifecycleconsumer.PreviewResumeCandidates) }
func (a *Adapter) runtimeConsumerDeps() consumerruntime.Deps {
	if a == nil {
		return consumerruntime.Deps{}
	}
	ctxDeps := a.depsOrDefault()
	return consumerruntime.Deps{
		Manager:                      a.manager(),
		BindingStore:                 a.bindingStore(),
		UIRuntime:                    a.uiRuntime(),
		BuildSelectedSkillPrompt:     a.buildSelectedSkillPrompt,
		ListSkillMatchCandidates:     ctxDeps.ListSkillMatchCandidates,
		ListAgentSkills:              ctxDeps.GetAgentSkills,
		CollectAutoMatchedSkillMatch: promptconsumer.CollectAutoMatchedSkillMatches,
		RenderAutoMatchedSkillPrompt: a.renderAutoMatchedSkillPrompt,
		ActiveTrackedTurnID:       a.activeTrackedTurnID,
		ShowInjectedPromptInChat:  a.showInjectedPromptInChat,
		ResolveLSPUsagePromptHint: a.ResolveLSPUsagePromptHint,
		ThreadExistsInHistory:     a.ThreadExistsInHistory,
		AllDynamicToolSchemas:    a.allDynamicToolSchemas,
		ResolveStartInstructions: a.resolveStartInstructionsForLaunch,
		SetAgentWorkDir:          a.setAgentWorkDir,
		GetThreadID:              a.GetThreadID,
		CancelCodeRuns:           a.cancelCodeRuns,
		ResolveCodexThreadCandidates:   a.resolveCodexThreadCandidatesForRuntime,
		ResumeThread:                   a.ResumeThread,
		IsCodexProcessCrashError:       lifecycleconsumer.IsCodexProcessCrashError,
		IsHistoricalResumeCandidateErr: lifecycleconsumer.IsHistoricalResumeCandidateError,
		PreviewResumeCandidates:        lifecycleconsumer.PreviewResumeCandidates,
		Notify:                         a.notify,
		Submit:                         a.Submit,
		ResolveClientActiveTurnID:      a.resolveClientActiveTurnIDForRuntime,
		BeginTrackedTurn:               a.beginTrackedTurn,
		TurnSteer:                      a.TurnSteer,
	}
}
func (a *Adapter) resolveClientActiveTurnIDForRuntime(proc *codexsdk.AgentProcess) string {
	if proc == nil || proc.Client == nil {
		return ""
	}
	reader, ok := proc.Client.(interface{ GetActiveTurnID() string })
	if !ok {
		return ""
	}
	return strings.TrimSpace(reader.GetActiveTurnID())
}
func (a *Adapter) resolveProcess(caller, threadID string) (*codexsdk.AgentProcess, error) {
	return consumerruntime.ResolveProcess(a.runtimeConsumerDeps(), caller, threadID)
}
func withProcess[T any](a *Adapter, caller string, threadID string, fn func(*codexsdk.AgentProcess) (T, error)) (T, error) {
	return consumerruntime.WithProcess(a.runtimeConsumerDeps(), caller, threadID, fn)
}
func (a *Adapter) managerProcess(threadID string) any { if manager := a.manager(); manager != nil { return manager.Get(threadID) }; return nil }
func (a *Adapter) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	return withProcess(a, "Server.turnSteer", threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) { return lifecycleconsumer.RunTurnSteer(proc, a.submitFromAny, submitPrompt, images, files) })
}
func (a *Adapter) CollectAutoMatchedSkillMatchesForThread(threadID string, prompt string, input []contracts.TurnInput, options contracts.AutoSkillMatchOptions) []contracts.AutoMatchedSkillMatch {
	return consumerruntime.CollectAutoMatchedSkillMatchesForThread(a.runtimeConsumerDeps(), threadID, prompt, input, options)
}
func (a *Adapter) TurnStart(ctx context.Context, req contracts.TurnStartRequest) (consumerruntime.TurnStartEntryResult, error) {
	return consumerruntime.TurnStart(ctx, a.runtimeConsumerDeps(), req)
}
func (a *Adapter) TurnSteerFromInput(req contracts.TurnSteerRequest) (map[string]any, error) {
	return consumerruntime.TurnSteerFromInput(a.runtimeConsumerDeps(), req)
}
func (a *Adapter) TurnSteerFromInputAligned(req contracts.TurnSteerRequest) (map[string]any, error) {
	return consumerruntime.TurnSteerFromInputAligned(a.runtimeConsumerDeps(), req)
}
type threadStartResult = lifecycleconsumer.ThreadStartResult
type threadResumeResult = lifecycleconsumer.ThreadResumeResult
type threadForkResult = lifecycleconsumer.ThreadForkResult
func (a *Adapter) ThreadStart(ctx context.Context, threadID, cwd, model, modelProvider, approvalPolicy string) (threadStartResult, error) { return a.threadStart(ctx, threadID, cwd, model, modelProvider, approvalPolicy) }
func (a *Adapter) threadStart(ctx context.Context, threadID, cwd, model, modelProvider, approvalPolicy string) (threadStartResult, error) {
	return lifecycleconsumer.RunThreadStart(ctx, threadID, cwd, model, modelProvider, approvalPolicy, a.allDynamicToolSchemas(), func(ctx context.Context, agentID, name, path, cwd, startInstructions string, dynamicTools []codexsdk.DynamicTool) error {
		if manager := a.manager(); manager != nil { return manager.Launch(ctx, agentID, name, path, cwd, startInstructions, dynamicTools) }
		return appErrors.New("Server.threadStart", "thread manager is not initialized")
	}, a.managerProcess, func() []lifecycleconsumer.AgentInfo { return lifecycleconsumer.ToAgentInfos(a.runningAgents()) }, a.resolveStartInstructionsForLaunch, func(ctx context.Context, threadID string, proc any) { if typed, ok := proc.(*codexsdk.AgentProcess); ok { a.registerBinding(ctx, threadID, typed) } }, func(items []lifecycleconsumer.AgentInfo) { if runtime := a.uiRuntime(); runtime != nil { runtime.ReplaceThreads(lifecycleconsumer.ToRuntimeThreadSnapshots(items)) } })
}
func (a *Adapter) ThreadResume(ctx context.Context, threadID, path, cwd, model string) (threadResumeResult, error) {
	id, err := requireThreadID("Server.threadResume", threadID)
	if err != nil {
		return threadResumeResult{}, err
	}
	return withProcess(a, "Server.threadResume", id, func(proc *codexsdk.AgentProcess) (threadResumeResult, error) {
		return lifecycleconsumer.RunThreadResume(ctx, id, path, cwd, model, proc, a.ResolveCodexThreadCandidates, lifecycleconsumer.NormalizeCodexThreadID, a.resumeThreadFromAny)
	})
}
func (a *Adapter) ThreadFork(threadID string) (threadForkResult, error) {
	sourceThreadID := strings.TrimSpace(threadID)
	return withProcess(a, "Server.threadFork", sourceThreadID, func(proc *codexsdk.AgentProcess) (threadForkResult, error) { return lifecycleconsumer.RunThreadFork(sourceThreadID, proc, a.forkThreadFromAny, a.nowUnixMilli) })
}
func (a *Adapter) ThreadRollback(threadID string, numTurns int) (map[string]any, error) { return a.sendThreadCommand("Server.threadRollback", threadID, "/undo", strconv.Itoa(numTurns), "send undo command") }
func (a *Adapter) ReviewStart(threadID, reviewArgs string) (map[string]any, error) { return a.sendThreadCommand("Server.reviewStart", threadID, "/review", reviewArgs, "send review command") }
func (a *Adapter) sendThreadCommand(methodName, threadID, command, args, wrapMsg string) (map[string]any, error) {
	return withProcess(a, methodName, threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) { return lifecycleconsumer.RunThreadCommand(proc, methodName, command, args, wrapMsg, a.sendCommandFromAny) })
}
func (a *Adapter) ThreadRealtimeStart(threadID, prompt string, _ *string) (map[string]any, error) { return lifecycleconsumer.RunThreadRealtimeStart(threadID, prompt) }
func (a *Adapter) ThreadRealtimeAppendAudio(threadID string, audio any) (map[string]any, error) { return lifecycleconsumer.RunThreadRealtimeAppendAudio(threadID, audio) }
func (a *Adapter) ThreadRealtimeAppendText(threadID, text string) (map[string]any, error) { return lifecycleconsumer.RunThreadRealtimeAppendText(threadID, text) }
func (a *Adapter) ThreadRealtimeStop(threadID string) (map[string]any, error) { return lifecycleconsumer.RunThreadRealtimeStop(threadID) }
func (a *Adapter) ThreadNameSet(ctx context.Context, threadID, name string) (map[string]any, error) {
	return lifecycleconsumer.RunThreadNameSet(ctx, threadID, name, a.managerProcess, func(threadID string) bool {
		return lifecycleconsumer.ThreadExistsInRuntime(threadID, a.uiRuntime())
	}, a.ThreadExistsInHistory, a.sendCommandFromAny, func(threadID, alias string) { if runtime := a.uiRuntime(); runtime != nil { runtime.SetThreadName(threadID, alias) } }, a.persistThreadAlias)
}
func (a *Adapter) ThreadRead(_ context.Context, threadID string) (map[string]any, error) {
	return withProcess(a, "Server.threadRead", threadID, func(proc *codexsdk.AgentProcess) (map[string]any, error) { return lifecycleconsumer.RunThreadRead(proc, a.listThreadsFromAny) })
}
func (a *Adapter) ThreadResolve(ctx context.Context, threadID string) (map[string]any, error) {
	return lifecycleconsumer.RunThreadResolve(ctx, threadID, a.resolveRunningThreadIdentity, a.firstResolvedCodexThreadID, a.ThreadExistsInHistory)
}
func (a *Adapter) ResolveCodexThreadCandidates(ctx context.Context, agentID string, appendUniqueThreadID func(dst []string, seen map[string]struct{}, candidate string) []string, previewCandidates func([]string, int) []string) []string {
	preview := previewCandidates
	if preview == nil {
		preview = lifecycleconsumer.PreviewResumeCandidates
	}
	return historyconsumer.ResolveCodexThreadCandidates(ctx, agentID, 0, appendUniqueThreadID, a.bindingCodexThreadID, a.statusSessionID, preview)
}
func (a *Adapter) ThreadExistsInHistory(ctx context.Context, threadID string) bool {
	return historyconsumer.ThreadExistsInHistory(ctx, threadID, 0, lifecycleconsumer.IsLikelyCodexThreadID, a.bindingExists, a.agentStatusExists, a.loadThreadArchiveMap)
}
func (a *Adapter) firstResolvedCodexThreadID(ctx context.Context, threadID string) string {
	return lifecycleconsumer.FirstResolvedCodexThreadIDFromCandidates(ctx, threadID, a.ResolveCodexThreadCandidates)
}
func (a *Adapter) resolveRunningThreadIdentity(threadID string) (state string, port int, codexThreadID string, found bool) {
	return lifecycleconsumer.ResolveRunningThreadIdentityFromAgents(threadID, lifecycleconsumer.ToAgentInfos(a.runningAgents()))
}
func (a *Adapter) FuzzyFileSearch(query string, roots []string, fuzzyMatch func(text, pattern string) bool) []map[string]any {
	return lifecycleconsumer.FuzzyFileSearch(query, roots, fuzzyMatch)
}
func (a *Adapter) readThreadRuntimeState(threadID string) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	return interruptconsumer.ReadThreadRuntimeStateByHooks(id, a.readRuntimeStatus, a.hasActiveTrackedTurn)
}
func (a *Adapter) readRuntimeStatus(threadID string) string {
	uiRuntime := a.uiRuntime()
	if uiRuntime == nil {
		return ""
	}
	snapshot := uiRuntime.Snapshot()
	return snapshot.Statuses[threadID]
}
func (a *Adapter) waitInterruptOutcome(threadID string, timeout time.Duration, activeHint bool) (bool, string, int64, bool) {
	return interruptconsumer.WaitInterruptOutcome(threadID, timeout, activeHint, a.waitTrackedTurnTerminal, a.readThreadRuntimeState)
}
func (a *Adapter) sendInterruptCommand(proc *codexsdk.AgentProcess) (bool, error) {
	return interruptconsumer.SendInterruptCommand(proc, a.sendCommandFromAny)
}
func (a *Adapter) notifyTurnCompleted(threadID, status, reason string) {
	interruptconsumer.NotifyTurnCompleted(threadID, status, reason, a.completeTrackedTurnByID, a.notify)
}
func (a *Adapter) withProcessAny(methodName string, threadID string, fn func(any) (any, error)) (any, error) {
	return withProcess(a, methodName, threadID, func(proc *codexsdk.AgentProcess) (any, error) { return fn(proc) })
}
func (a *Adapter) TurnInterrupt(threadID string) (any, error) {
	return interruptconsumer.TurnInterrupt(threadID, a.readThreadRuntimeState, a.hasActiveTrackedTurn, a.cancelCodeRuns, a.sendInterruptFromAny, a.withProcessAny, a.markTrackedTurnInterruptRequested, a.waitInterruptOutcome, a.notifyTurnCompleted)
}
func (a *Adapter) TurnForceComplete(threadID string) (any, error) {
	return interruptconsumer.TurnForceComplete(threadID, a.cancelCodeRuns, a.sendInterruptFromAny, a.notifyTurnCompleted, a.withProcessAny)
}
func (a *Adapter) sendSlashCommandWithParams(ctx context.Context, params json.RawMessage, command, argKey string, requireThreadID bool) (any, error) {
	parsed, err := commandconsumer.ParseSlashCommandArgParams(params, argKey, trackerconsumer.ExtractTrackedString)
	if err != nil {
		return nil, err
	}
	return a.sendSlashCommand(ctx, "Server.sendSlashCommand", parsed.ThreadID, command, parsed.Args, requireThreadID)
}
func (a *Adapter) SendSlashCommandFromRawParams(ctx context.Context, params json.RawMessage, command string) (any, error) {
	return a.sendSlashCommandWithParams(ctx, params, command, "args", false)
}
func (a *Adapter) SendSlashCommandFromRawParamsRequireThreadID(ctx context.Context, params json.RawMessage, command string) (any, error) {
	return a.sendSlashCommandWithParams(ctx, params, command, "args", true)
}
func (a *Adapter) SendSlashCommandWithArgs(params json.RawMessage, command, argKey string) (any, error) {
	return a.sendSlashCommandWithParams(context.Background(), params, command, argKey, false)
}
func (a *Adapter) ThreadSkillsList() (any, error) {
	result, err := a.sendSlashCommand(context.Background(), "Server.threadSkillsList", "", "/skills", "", false)
	return commandconsumer.ThreadSkillsListResult(result, err)
}
func (a *Adapter) sendSlashCommand(ctx context.Context, methodName, threadID, command, args string, requireThreadID bool) (map[string]any, error) {
	return commandconsumer.RunSendSlashCommand(ctx, methodName, threadID, command, args, requireThreadID, a.resolveThreadFromSlashCommand, a.withProcessMap, a.sendCommandFromAny)
}
func (a *Adapter) runningAgents() []codexsdk.AgentInfo { if m := a.manager(); m != nil { return m.List() }; return nil }
func (a *Adapter) ThreadList(ctx context.Context) ([]contracts.ThreadListItem, error) {
	items, err := listingconsumer.BuildThreadListFromDeps(
		ctx,
		listingconsumer.ToAgentInfos(a.runningAgents()),
		a.bindingStore(),
		a.statusStore(),
		a.store(),
		a.uiRuntime(),
		a.loadThreadArchiveMap,
	)
	if err != nil {
		return nil, err
	}
	return listingconsumer.ToThreadListItems(items), nil
}
func (a *Adapter) ThreadLoadedList(_ context.Context, cursor *string, limit *uint32) ([]string, *string, error) {
	ids := listingconsumer.LoadedThreadIDsFromAgents(listingconsumer.ToAgentInfos(a.runningAgents()))
	data, nextCursor := listingconsumer.PaginateLoadedThreadIDs(ids, cursor, limit)
	return data, nextCursor, nil
}
func (a *Adapter) registerBinding(ctx context.Context, agentID string, proc *codexsdk.AgentProcess) {
	consumerruntime.RegisterBinding(ctx, a.runtimeConsumerDeps(), agentID, proc)
}
func (a *Adapter) persistThreadAlias(ctx context.Context, threadID, alias string) error {
	if store := a.store(); store != nil {
		return listingconsumer.PersistThreadAlias(ctx, threadID, alias, store.Get, store.Set)
	}
	return nil
}
func (a *Adapter) loadThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	archivedMap := map[string]int64{}
	if store := a.store(); store != nil {
		if value, err := store.Get(ctx, prefThreadArchivesChat); err != nil { return nil, err } else { archivedMap = archiveconsumer.NormalizeThreadArchiveMap(value) }
	}
	fromDisk, err := archiveconsumer.LoadThreadArchiveMapFromDisk()
	if err != nil {
		logger.Warn("thread/archive: scan archive root failed", logger.FieldError, err)
		return archivedMap, nil
	}
	return archiveconsumer.MergeThreadArchiveMaps(archivedMap, fromDisk), nil
}
func (a *Adapter) trackerStateAndNotify() (turnTrackerState, func(string, any)) {
	if a == nil {
		return turnTrackerState{}, nil
	}
	var notify func(string, any)
	if deps := a.Context(); deps != nil {
		notify = deps.Notify
	}
	return a.tracker, notify
}
func (a *Adapter) applyTrackedTurnTransition(threadID string, req trackedTurnTransitionRequest) trackedTurnTransitionResult { state, _ := a.trackerStateAndNotify(); return trackerconsumer.ApplyTrackedTurnTransitionCore(state, threadID, req) }
func (a *Adapter) activeTrackedTurnID(threadID string) (string, bool) {
	state := a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{})
	if !state.Found || strings.TrimSpace(state.TurnID) == "" {
		return "", false
	}
	return state.TurnID, true
}
func (a *Adapter) hasActiveTrackedTurn(threadID string) bool {
	return a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{}).Found
}
func (a *Adapter) markTrackedTurnInterruptRequested(threadID string) bool {
	state := a.applyTrackedTurnTransition(threadID, trackedTurnTransitionRequest{MarkInterruptRequested: true})
	return state.Found && state.InterruptRequested
}
func (a *Adapter) waitTrackedTurnTerminal(threadID string, timeout time.Duration) (string, bool) { state, _ := a.trackerStateAndNotify(); return trackerconsumer.WaitTrackedTurnTerminalCore(state, threadID, timeout) }
func (a *Adapter) completeTrackedTurnByID(threadID, turnID, status, reason string) (map[string]any, bool) {
	state, _ := a.trackerStateAndNotify(); return trackerconsumer.CompleteTrackedTurnByIDCore(state, threadID, turnID, status, reason)
}
func (a *Adapter) beginTrackedTurn(threadID, turnID string) string {
	state, notify := a.trackerStateAndNotify(); return trackerconsumer.BeginTrackedTurnCore(state, threadID, turnID, a.completeTrackedTurnByID, notify, a.checkTurnStall)
}
func (a *Adapter) trackerDuration(getter func(turnTrackerState) *time.Duration, fallback time.Duration) time.Duration {
	state, _ := a.trackerStateAndNotify(); return trackerconsumer.TrackerDurationCore(state, getter, fallback)
}
func (a *Adapter) setTrackerDuration(getter func(turnTrackerState) *time.Duration, value time.Duration) {
	state, _ := a.trackerStateAndNotify(); trackerconsumer.SetTrackerDurationCore(state, getter, value)
}
func (a *Adapter) trackerState() (map[string]*trackedTurn, *sync.Mutex, time.Duration, time.Duration) { state, _ := a.trackerStateAndNotify(); return trackerconsumer.TrackerStateCore(state) }
func (a *Adapter) stallThreshold() time.Duration {
	return a.trackerDuration(func(state turnTrackerState) *time.Duration { return state.StallThreshold }, defaultStallThreshold)
}
func (a *Adapter) SetStallThreshold(threshold time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration { return state.StallThreshold }, threshold)
}
func (a *Adapter) SetStallHeartbeat(interval time.Duration) {
	a.setTrackerDuration(func(state turnTrackerState) *time.Duration { return state.StallHeartbeat }, interval)
}
func (a *Adapter) touchTrackedTurnLastEvent(threadID string) { state, _ := a.trackerStateAndNotify(); trackerconsumer.TouchTrackedTurnLastEventCore(state, threadID) }
func (a *Adapter) StartApprovalStallHeartbeat(threadID string) func() {
	_, _, _, stallThreshold := a.trackerState()
	return trackerconsumer.StartStallHeartbeat(threadID, stallThreshold, defaultStallThreshold, defaultStallThreshold, a.touchTrackedTurnLastEvent)
}
func (a *Adapter) StartDynamicToolStallHeartbeat(threadID string) func() {
	return trackerconsumer.StartStallHeartbeat(threadID, a.stallThreshold(), defaultStallThreshold, defaultStallThreshold, a.touchTrackedTurnLastEvent)
}
func (a *Adapter) checkTurnStall(threadID string, turnID string) {
	state, _ := a.trackerStateAndNotify(); trackerconsumer.CheckTurnStallCore(state, threadID, turnID, a.handleStallGracePeriod, a.executeStallAutoInterrupt, a.checkTurnStall)
}
func (a *Adapter) handleStallGracePeriod(threadID, turnID string, silent, threshold time.Duration) {
	state, _ := a.trackerStateAndNotify(); trackerconsumer.HandleStallGracePeriodCore(state, threadID, turnID, silent, threshold, trackerconsumer.TrackerRuntimePushAlert(a.uiRuntime()), a.checkTurnStall)
}
func (a *Adapter) executeStallAutoInterrupt(threadID string, turnID string, silent time.Duration, threshold time.Duration) {
	_, notify := a.trackerStateAndNotify()
	trackerconsumer.ExecuteStallAutoInterruptCore(
		threadID,
		turnID,
		silent,
		threshold,
		trackerconsumer.TrackerRuntimePushAlert(a.uiRuntime()),
		a.markTrackedTurnInterruptRequested,
		a.cancelCodeRuns,
		trackerconsumer.TrackerInterruptSender(a.managerProcess, a.sendCommandFromAny),
		a.completeTrackedTurnByID,
		notify,
	)
}
func (a *Adapter) ExtractTrackedString(payload map[string]any, keys ...string) string {
	return trackerconsumer.ExtractTrackedString(payload, keys...)
}
func (a *Adapter) TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	return trackerconsumer.TrackedTurnTerminalFromEvent(eventType, method, payload)
}
func (a *Adapter) TrackedTurnSummaryFromPayload(payload map[string]any) string {
	return trackerconsumer.TrackedTurnSummaryFromPayload(payload)
}
func (a *Adapter) CaptureAndInjectTurnSummary(threadID, eventType, method string, payload map[string]any) {
	state, _ := a.trackerStateAndNotify(); trackerconsumer.CaptureAndInjectTurnSummaryCore(state, threadID, eventType, method, payload)
}
func (a *Adapter) FinalizeTrackedTurnEvent(threadID string, eventType string, method string, payload map[string]any) {
	state, notify := a.trackerStateAndNotify(); trackerconsumer.FinalizeTrackedTurnEventCore(state, threadID, eventType, method, payload, notify)
}
func (a *Adapter) ThreadArchiveMap(ctx context.Context) (map[string]int64, error) { return a.loadThreadArchiveMap(ctx) }
func (a *Adapter) threadExistsForArchive(ctx context.Context, threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	if manager := a.manager(); manager != nil && manager.Get(id) != nil {
		return true
	}
	if lifecycleconsumer.ThreadExistsInRuntime(id, a.uiRuntime()) {
		return true
	}
	return a.ThreadExistsInHistory(ctx, id)
}
func (a *Adapter) saveThreadArchiveMap(ctx context.Context, archivedMap map[string]int64) error { if store := a.store(); store != nil { return store.Set(ctx, prefThreadArchivesChat, archivedMap) }; return nil }
func (a *Adapter) archiveDeps() archiveconsumer.ThreadArchiveDeps {
	return archiveconsumer.ThreadArchiveDeps{
		ThreadExists:                a.threadExistsForArchive,
		LoadArchiveMap:              a.loadThreadArchiveMap,
		SaveArchiveMap:              a.saveThreadArchiveMap,
		ResolveRolloutHistorySource: a.resolveRolloutHistorySource,
		BindRolloutPath:             a.bindRolloutPath,
	}
}
func (a *Adapter) ThreadArchive(ctx context.Context, threadID string) (map[string]any, error) { return archiveconsumer.ThreadArchive(ctx, threadID, a.archiveDeps(), a.nowUnixMilli) }
func (a *Adapter) ThreadUnarchive(ctx context.Context, threadID string) (map[string]any, error) { return archiveconsumer.ThreadUnarchive(ctx, threadID, a.archiveDeps()) }
func (a *Adapter) bindRolloutPath(ctx context.Context, agentID, codexThreadID, rolloutPath string) {
	if strings.TrimSpace(codexThreadID) == "" || strings.TrimSpace(rolloutPath) == "" {
		return
	}
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := bindingStore.Bind(dbCtx, agentID, codexThreadID, rolloutPath); err != nil {
		logger.Warn("thread/archive: persist rollout path failed", logger.FieldThreadID, agentID, "codex_thread_id", codexThreadID, "rollout_path", rolloutPath, logger.FieldError, err)
	}
}

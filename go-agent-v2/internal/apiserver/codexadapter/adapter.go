package codexadapter

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	archivesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/archive"
	interruptsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/interrupt"
	listingsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/listing"
	promptsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/prompt"
	trackersvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/tracker"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	defaultLSPUsagePromptHint = ""
	maxLSPUsagePromptHintLen  = 16000
)

const (
	DefaultTurnWatchdogTimeout        = trackersvc.DefaultTurnWatchdogTimeout
	DefaultTrackedTurnSummaryTTL      = trackersvc.DefaultTrackedTurnSummaryTTL
	TrackedTurnSummaryCacheMaxEntries = trackersvc.TrackedTurnSummaryCacheMaxEntries
	defaultStallThreshold             = trackersvc.DefaultStallThreshold
	defaultStallHeartbeat             = trackersvc.DefaultStallHeartbeat
)

type (
	trackedTurn                  = trackersvc.TrackedTurn
	trackedTurnTransitionRequest = trackersvc.TrackedTurnTransitionRequest
	trackedTurnTransitionResult  = trackersvc.TrackedTurnTransitionResult
	trackedTurnSummaryCacheEntry = trackersvc.TrackedTurnSummaryCacheEntry
	turnTrackerState             = trackersvc.TurnTrackerState
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

type Adapter struct {
	ctx                    *Deps
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
	initializeTrackerState(adapter)
	return adapter
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

func (a *Adapter) store() *uistate.PreferenceManager           { return a.depsOrDefault().Store }
func (a *Adapter) manager() *codexsdk.AgentManager             { return a.depsOrDefault().Manager }
func (a *Adapter) bindingStore() *store.AgentCodexBindingStore { return a.depsOrDefault().BindingStore }
func (a *Adapter) statusStore() *store.AgentStatusStore        { return a.depsOrDefault().AgentStatusStore }
func (a *Adapter) uiRuntime() *uistate.RuntimeManager          { return a.depsOrDefault().UIRuntime }

func (a *Adapter) allDynamicToolSchemas() []codexsdk.DynamicTool {
	return a.depsOrDefault().AllSchemas()
}

func (a *Adapter) setAgentWorkDir(agentID string, cwd string) {
	a.depsOrDefault().SetAgentWorkDir(agentID, cwd)
}

func (a *Adapter) cancelCodeRuns(agentID string) int {
	return a.depsOrDefault().CancelCodeRuns(agentID)
}

func (a *Adapter) nowUnixMilli() int64 { return a.depsOrDefault().NowUnixMilli() }

func (a *Adapter) notify(method string, payload any) {
	if strings.TrimSpace(method) == "" {
		return
	}
	a.depsOrDefault().Notify(method, payload)
}

func (a *Adapter) bindingExists(ctx context.Context, agentID string) (bool, error) {
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return false, nil
	}
	binding, err := bindingStore.FindByAgentID(ctx, agentID)
	return binding != nil, err
}

func (a *Adapter) agentStatusExists(ctx context.Context, agentID string) (bool, error) {
	statusStore := a.statusStore()
	if statusStore == nil {
		return false, nil
	}
	status, err := statusStore.Get(ctx, agentID)
	return status != nil, err
}

func (a *Adapter) bindingCodexThreadID(ctx context.Context, agentID string) (string, error) {
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return "", nil
	}
	binding, err := bindingStore.FindByAgentID(ctx, agentID)
	if err != nil || binding == nil {
		return "", err
	}
	return binding.CodexThreadID, nil
}

func (a *Adapter) statusSessionID(ctx context.Context, agentID string) (string, error) {
	statusStore := a.statusStore()
	if statusStore == nil {
		return "", nil
	}
	status, err := statusStore.Get(ctx, agentID)
	if err != nil || status == nil {
		return "", err
	}
	return status.SessionID, nil
}

func (a *Adapter) storeGetter() func(context.Context, string) (any, error) {
	if store := a.store(); store != nil {
		return store.Get
	}
	return nil
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
	return promptsvc.BuildSelectedSkillPrompt(selectedSkills, a.readSkillContent, commonadapter.SkillInputText)
}

func (a *Adapter) renderAutoMatchedSkillPrompt(agentID string, matches []contracts.AutoMatchedSkillMatch) (string, int) {
	return promptsvc.RenderAutoMatchedSkillPrompt(agentID, matches, a.readSkillContent, commonadapter.MergePromptText, commonadapter.SkillInputText)
}

func (a *Adapter) runningAgents() []codexsdk.AgentInfo {
	if m := a.manager(); m != nil {
		return m.List()
	}
	return nil
}

func toListingAgentInfos(items []codexsdk.AgentInfo) []listingsvc.AgentInfo {
	if len(items) == 0 {
		return nil
	}
	out := make([]listingsvc.AgentInfo, len(items))
	for i, item := range items {
		out[i] = listingsvc.AgentInfo{ID: item.ID, Name: item.Name, State: string(item.State)}
	}
	return out
}

func (a *Adapter) ThreadList(ctx context.Context) ([]contracts.ThreadListItem, error) {
	runningAgents := toListingAgentInfos(a.runningAgents())
	appendBinding := func(ctx context.Context, threads []listingsvc.ThreadListItem, seen map[string]struct{}, methodName string) []listingsvc.ThreadListItem {
		return listingsvc.AppendHistoryFromBindingStore(ctx, threads, seen, methodName, func(ctx context.Context) ([]listingsvc.AgentCodexBinding, error) {
			bindingStore := a.bindingStore()
			if bindingStore == nil {
				return nil, nil
			}
			items, err := bindingStore.ListAll(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]listingsvc.AgentCodexBinding, 0, len(items))
			for _, item := range items {
				out = append(out, listingsvc.AgentCodexBinding{AgentID: item.AgentID})
			}
			return out, nil
		})
	}
	appendStatus := func(ctx context.Context, threads []listingsvc.ThreadListItem, seen map[string]struct{}, methodName string) []listingsvc.ThreadListItem {
		return listingsvc.AppendHistoryFromStatusStore(ctx, threads, seen, methodName, func(ctx context.Context) ([]listingsvc.AgentStatus, error) {
			statusStore := a.statusStore()
			if statusStore == nil {
				return nil, nil
			}
			items, err := statusStore.List(ctx, "")
			if err != nil {
				return nil, err
			}
			out := make([]listingsvc.AgentStatus, 0, len(items))
			for _, item := range items {
				out = append(out, listingsvc.AgentStatus{AgentID: item.AgentID, AgentName: item.AgentName})
			}
			return out, nil
		})
	}
	appendArchive := func(ctx context.Context, threads []listingsvc.ThreadListItem, seen map[string]struct{}, methodName string) []listingsvc.ThreadListItem {
		return listingsvc.AppendHistoryFromArchiveState(ctx, threads, seen, methodName, a.loadThreadArchiveMap)
	}
	loadAliases := func(ctx context.Context) map[string]string {
		store := a.store()
		if store == nil {
			return map[string]string{}
		}
		return listingsvc.LoadThreadAliases(ctx, store.Get)
	}
	syncRuntimeThreads := func(threads []listingsvc.ThreadListItem) {
		runtime := a.uiRuntime()
		if runtime == nil {
			return
		}
		snapshots := make([]uistate.ThreadSnapshot, 0, len(threads))
		for _, item := range threads {
			snapshots = append(snapshots, uistate.ThreadSnapshot{ID: item.ID, Name: item.Name, State: item.State})
		}
		runtime.ReplaceThreads(snapshots)
	}
	items, err := listingsvc.BuildThreadList(
		ctx,
		"thread/list",
		true,
		func() []listingsvc.AgentInfo { return runningAgents },
		func(ctx context.Context, threads []listingsvc.ThreadListItem, seen map[string]struct{}, methodName string) []listingsvc.ThreadListItem {
			return listingsvc.AppendThreadHistoryFromStores(ctx, threads, seen, methodName, appendBinding, appendStatus, appendArchive)
		},
		loadAliases,
		syncRuntimeThreads,
	)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (a *Adapter) ThreadLoadedList(_ context.Context, cursor *string, limit *uint32) ([]string, *string, error) {
	ids := listingsvc.LoadedThreadIDsFromAgents(toListingAgentInfos(a.runningAgents()))
	data, nextCursor := listingsvc.PaginateLoadedThreadIDs(ids, cursor, limit)
	return data, nextCursor, nil
}

func (a *Adapter) registerBinding(ctx context.Context, agentID string, proc *codexsdk.AgentProcess) {
	bindingStore := a.bindingStore()
	if bindingStore == nil || proc == nil {
		return
	}
	codexThreadID := a.GetThreadID(proc)
	if codexThreadID == "" {
		return
	}
	if err := bindingStore.Bind(ctx, agentID, codexThreadID, ""); err != nil {
		logger.Warn("turn/start: failed to register binding",
			append(threadLogFields(agentID), "codex_thread_id", codexThreadID, logger.FieldError, err)...,
		)
	}
}

func (a *Adapter) persistThreadAlias(ctx context.Context, threadID, alias string) error {
	if store := a.store(); store != nil {
		return listingsvc.PersistThreadAlias(ctx, threadID, alias, store.Get, store.Set)
	}
	return nil
}

func (a *Adapter) loadThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	archivedMap := map[string]int64{}
	if store := a.store(); store != nil {
		if value, err := store.Get(ctx, prefThreadArchivesChat); err != nil {
			return nil, err
		} else {
			archivedMap = archivesvc.NormalizeThreadArchiveMap(value)
		}
	}
	fromDisk, err := archivesvc.LoadThreadArchiveMapFromDisk()
	if err != nil {
		logger.Warn("thread/archive: scan archive root failed", logger.FieldError, err)
		return archivedMap, nil
	}
	return archivesvc.MergeThreadArchiveMaps(archivedMap, fromDisk), nil
}

func (a *Adapter) FuzzyFileSearch(query string, roots []string, fuzzyMatch func(text, pattern string) bool) []map[string]any {
	return fuzzyFileSearch(query, roots, fuzzyMatch)
}

func (a *Adapter) readThreadRuntimeState(threadID string) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	return interruptsvc.ReadThreadRuntimeStateByHooks(id, a.readRuntimeStatus, a.hasActiveTrackedTurn)
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
	return interruptsvc.WaitInterruptOutcome(threadID, timeout, activeHint, a.waitTrackedTurnTerminal, a.readThreadRuntimeState)
}

func (a *Adapter) sendInterruptCommand(proc *codexsdk.AgentProcess) (bool, error) {
	return interruptsvc.SendInterruptCommand(proc, a.sendCommandFromAny)
}

func (a *Adapter) notifyTurnCompleted(threadID, status, reason string) {
	interruptsvc.NotifyTurnCompleted(threadID, status, reason, a.completeTrackedTurnByID, a.notify)
}

func (a *Adapter) withProcessAny(methodName string, threadID string, fn func(any) (any, error)) (any, error) {
	return withProcess(a, methodName, threadID, func(proc *codexsdk.AgentProcess) (any, error) { return fn(proc) })
}

func (a *Adapter) TurnInterrupt(threadID string) (any, error) {
	return interruptsvc.TurnInterrupt(threadID, a.readThreadRuntimeState, a.hasActiveTrackedTurn, a.cancelCodeRuns, a.sendInterruptFromAny, a.withProcessAny, a.markTrackedTurnInterruptRequested, a.waitInterruptOutcome, a.notifyTurnCompleted)
}

func (a *Adapter) TurnForceComplete(threadID string) (any, error) {
	return interruptsvc.TurnForceComplete(threadID, a.cancelCodeRuns, a.sendInterruptFromAny, a.notifyTurnCompleted, a.withProcessAny)
}

func (a *Adapter) ThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	return a.loadThreadArchiveMap(ctx)
}

func (a *Adapter) threadExistsForArchive(ctx context.Context, threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	if manager := a.manager(); manager != nil && manager.Get(id) != nil {
		return true
	}
	if threadExistsInRuntime(id, a.uiRuntime()) {
		return true
	}
	return a.ThreadExistsInHistory(ctx, id)
}

func (a *Adapter) saveThreadArchiveMap(ctx context.Context, archivedMap map[string]int64) error {
	if store := a.store(); store != nil {
		return store.Set(ctx, prefThreadArchivesChat, archivedMap)
	}
	return nil
}

func (a *Adapter) archiveDeps() archivesvc.ThreadArchiveDeps {
	return archivesvc.ThreadArchiveDeps{
		ThreadExists:                a.threadExistsForArchive,
		LoadArchiveMap:              a.loadThreadArchiveMap,
		SaveArchiveMap:              a.saveThreadArchiveMap,
		ResolveRolloutHistorySource: a.resolveRolloutHistorySource,
		BindRolloutPath:             a.bindRolloutPath,
	}
}

func (a *Adapter) ThreadArchive(ctx context.Context, threadID string) (map[string]any, error) {
	return archivesvc.ThreadArchive(ctx, threadID, a.archiveDeps(), a.nowUnixMilli)
}

func (a *Adapter) ThreadUnarchive(ctx context.Context, threadID string) (map[string]any, error) {
	return archivesvc.ThreadUnarchive(ctx, threadID, a.archiveDeps())
}

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

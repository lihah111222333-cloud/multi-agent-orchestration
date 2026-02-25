package codexadapter

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ThreadStartOptions carries dependencies for thread/start.
type ThreadStartOptions struct {
	ThreadID          string
	Cwd               string
	Model             string
	ModelProvider     string
	ApprovalPolicy    string
	DynamicTools      []agentcore.DynamicTool
	StartInstructions string
}

// ThreadStartResult is the normalized thread/start payload.
type ThreadStartResult struct {
	ThreadID       string
	Status         string
	Model          string
	ModelProvider  string
	Cwd            string
	ApprovalPolicy string
}

// ThreadStartFromParams launches a thread using constructor-time dependencies.
func (a *Adapter) ThreadStartFromParams(
	ctx context.Context,
	threadID string,
	cwd string,
	model string,
	modelProvider string,
	approvalPolicy string,
) (ThreadStartResult, error) {
	dynamicTools := a.allDynamicToolSchemas()
	startInstructions := a.resolveStartInstructionsForLaunch(ctx, dynamicTools)
	return a.ThreadStart(ctx, ThreadStartOptions{
		ThreadID:          threadID,
		Cwd:               cwd,
		Model:             model,
		ModelProvider:     modelProvider,
		ApprovalPolicy:    approvalPolicy,
		DynamicTools:      dynamicTools,
		StartInstructions: startInstructions,
	})
}

// ThreadStart launches thread runtime and syncs UI snapshots.
func (a *Adapter) ThreadStart(ctx context.Context, opt ThreadStartOptions) (ThreadStartResult, error) {
	result := ThreadStartResult{
		ThreadID:       strings.TrimSpace(opt.ThreadID),
		Status:         "running",
		Model:          opt.Model,
		ModelProvider:  opt.ModelProvider,
		Cwd:            strings.TrimSpace(opt.Cwd),
		ApprovalPolicy: opt.ApprovalPolicy,
	}
	if result.ThreadID == "" {
		return ThreadStartResult{}, apperrors.New("Server.threadStart", "threadId is required")
	}
	if result.Cwd == "" {
		result.Cwd = "."
	}
	if a == nil || a.ctx == nil || a.ctx.Manager() == nil {
		return ThreadStartResult{}, apperrors.New("Server.threadStart", "thread manager is not initialized")
	}

	if err := a.ctx.Manager().Launch(ctx, result.ThreadID, result.ThreadID, "", result.Cwd, opt.StartInstructions, opt.DynamicTools); err != nil {
		return ThreadStartResult{}, apperrors.Wrap(err, "Server.threadStart", "launch thread")
	}
	if proc := a.ctx.Manager().Get(result.ThreadID); proc != nil {
		a.registerBinding(ctx, result.ThreadID, proc)
	}
	if runtime := a.ctx.UIRuntime(); runtime != nil {
		runtime.ReplaceThreads(toThreadSnapshots(a.ctx.Manager().List()))
	}
	return result, nil
}

// ThreadResumeResult is the normalized thread/resume payload.
type ThreadResumeResult struct {
	ThreadID string
	Status   string
	Model    string
}

// ThreadResume resumes a historical codex thread by candidate probing.
func (a *Adapter) ThreadResume(ctx context.Context, threadID, path, cwd, model string) (ThreadResumeResult, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return ThreadResumeResult{}, apperrors.New("Server.threadResume", "threadId is required")
	}
	normalize := a.normalizeCodexThreadID
	if normalize == nil {
		normalize = strings.TrimSpace
	}
	resolveCandidates := func(resolveCtx context.Context, id string) []string {
		return a.ResolveCodexThreadCandidates(resolveCtx, id, appendUniqueThreadIDFallback, PreviewResumeCandidates)
	}
	return withProcess(a, "Server.threadResume", threadID, func(proc *runner.AgentProcess) (ThreadResumeResult, error) {
		resolved := resolveCandidates(ctx, threadID)
		candidates := BuildResumeCandidates(threadID, resolved, normalize)
		logger.Info("thread/resume: resolved candidates",
			logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
			"candidate_count", len(candidates),
			"candidates", PreviewResumeCandidates(candidates, 4),
			"cwd", strings.TrimSpace(cwd),
		)

		resumedID, resumeErr := TryResumeCandidates(candidates, threadID, func(id string) error {
			return a.ResumeThread(proc, agentcore.ResumeThreadRequest{
				ThreadID: id,
				Path:     path,
				Cwd:      cwd,
			})
		}, IsHistoricalResumeCandidateError)
		if resumeErr != nil {
			return ThreadResumeResult{}, apperrors.Wrap(resumeErr, "Server.threadResume", "resume thread")
		}
		_ = resumedID
		return ThreadResumeResult{
			ThreadID: threadID,
			Status:   "resumed",
			Model:    model,
		}, nil
	})
}

// ThreadForkOptions carries dependencies for thread/fork.
type ThreadForkOptions struct {
	ThreadID     string
	NowUnixMilli func() int64
}

// ThreadForkResult is the normalized thread/fork payload.
type ThreadForkResult struct {
	ThreadID   string
	ForkedFrom string
}

// ThreadForkByID forks thread using constructor-time dependencies.
func (a *Adapter) ThreadForkByID(threadID string) (ThreadForkResult, error) {
	return a.ThreadFork(ThreadForkOptions{
		ThreadID: threadID,
	})
}

// ThreadFork creates a fork from source thread.
func (a *Adapter) ThreadFork(opt ThreadForkOptions) (ThreadForkResult, error) {
	return withProcess(a, "Server.threadFork", opt.ThreadID,
		func(proc *runner.AgentProcess) (ThreadForkResult, error) {
			threadID := strings.TrimSpace(opt.ThreadID)
			resp, forkErr := a.ForkThread(proc, agentcore.ForkThreadRequest{
				SourceThreadID: threadID,
			})
			if forkErr != nil {
				return ThreadForkResult{}, apperrors.Wrap(forkErr, "Server.threadFork", "fork thread")
			}
			newID := ""
			if resp != nil {
				newID = strings.TrimSpace(resp.ThreadID)
			}
			if newID == "" {
				now := time.Now().UnixMilli()
				if opt.NowUnixMilli != nil {
					now = opt.NowUnixMilli()
				}
				newID = fmt.Sprintf("thread-%d", now)
			}
			return ThreadForkResult{
				ThreadID:   newID,
				ForkedFrom: threadID,
			}, nil
		})
}

// ThreadRollback sends /undo index command.
func (a *Adapter) ThreadRollback(threadID string, turnIndex int) (map[string]any, error) {
	return withProcess(a, "Server.threadRollback", threadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			if cmdErr := a.SendCommand(proc, "/undo", fmt.Sprintf("%d", turnIndex)); cmdErr != nil {
				return nil, apperrors.Wrap(cmdErr, "Server.threadRollback", "send undo command")
			}
			return map[string]any{}, nil
		})
}

// ReviewStart dispatches /review command.
func (a *Adapter) ReviewStart(threadID, delivery string) (map[string]any, error) {
	return withProcess(a, "Server.reviewStart", threadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			if cmdErr := a.SendCommand(proc, "/review", delivery); cmdErr != nil {
				return nil, apperrors.Wrap(cmdErr, "Server.reviewStart", "send review command")
			}
			return map[string]any{}, nil
		})
}

// TurnSteerOptions carries dependencies for turn/steer.
type TurnSteerOptions struct {
	ThreadID     string
	SubmitPrompt string
	Images       []string
	Files        []string
}

// TurnSteer submits steering prompt to existing thread.
func (a *Adapter) TurnSteer(opt TurnSteerOptions) (map[string]any, error) {
	return withProcess(a, "Server.turnSteer", opt.ThreadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			if submitErr := a.Submit(proc, opt.SubmitPrompt, opt.Images, opt.Files, nil); submitErr != nil {
				return nil, submitErr
			}
			return map[string]any{}, nil
		})
}

// ThreadListItem models thread list entry payload.
type ThreadListItem = contracts.ThreadListItem

// ThreadListOptions configures thread/list and thread/loaded/list behavior.
type ThreadListOptions struct {
	MethodName  string
	SyncRuntime bool
}

// ThreadListDefault returns thread/list payload using constructor-time dependencies.
func (a *Adapter) ThreadListDefault(ctx context.Context) ([]ThreadListItem, error) {
	return a.ThreadList(ctx, ThreadListOptions{
		MethodName: "thread/list",
	})
}

// ThreadLoadedListDefault returns thread/loaded/list payload using constructor-time dependencies.
func (a *Adapter) ThreadLoadedListDefault(ctx context.Context) ([]ThreadListItem, error) {
	return a.ThreadLoadedList(ctx, ThreadListOptions{
		MethodName: "thread/loaded/list",
	})
}

// ThreadList returns thread/list payload and syncs runtime snapshots.
func (a *Adapter) ThreadList(ctx context.Context, opt ThreadListOptions) ([]ThreadListItem, error) {
	opt.SyncRuntime = true
	if strings.TrimSpace(opt.MethodName) == "" {
		opt.MethodName = "thread/list"
	}
	return a.threadList(ctx, opt)
}

// ThreadLoadedList returns thread/loaded/list payload.
func (a *Adapter) ThreadLoadedList(ctx context.Context, opt ThreadListOptions) ([]ThreadListItem, error) {
	opt.SyncRuntime = false
	if strings.TrimSpace(opt.MethodName) == "" {
		opt.MethodName = "thread/loaded/list"
	}
	return a.threadList(ctx, opt)
}

func (a *Adapter) threadList(ctx context.Context, opt ThreadListOptions) ([]ThreadListItem, error) {
	agents := []runner.AgentInfo{}
	if a != nil && a.ctx != nil && a.ctx.Manager() != nil {
		agents = a.ctx.Manager().List()
	}
	threads := make([]ThreadListItem, 0, len(agents)+32)
	seen := make(map[string]struct{}, len(agents)+32)
	for _, item := range agents {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		threads = append(threads, ThreadListItem{
			ID:    id,
			Name:  name,
			State: string(item.State),
		})
		seen[id] = struct{}{}
	}

	threads = a.appendThreadHistoryFromStores(ctx, threads, seen, opt.MethodName)
	applyThreadAliases(threads, a.loadThreadAliases(ctx))
	if opt.SyncRuntime && a != nil && a.ctx != nil && a.ctx.UIRuntime() != nil {
		a.ctx.UIRuntime().ReplaceThreads(toThreadSnapshots(threads))
	}
	return threads, nil
}

func (a *Adapter) appendThreadHistoryFromStores(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
) []ThreadListItem {
	idMethod := strings.TrimSpace(methodName)
	if idMethod == "" {
		idMethod = "thread/list"
	}
	if a == nil || a.ctx == nil {
		return threads
	}
	if bindingStore := a.ctx.BindingStore(); bindingStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		bindings, err := bindingStore.ListAll(dbCtx)
		cancel()
		if err != nil {
			logger.Warn(idMethod+": load history threads from agent_codex_binding failed", logger.FieldError, err)
		} else {
			threads = appendThreadItems(threads, seen, bindings)
		}
	}
	if statusStore := a.ctx.AgentStatusStore(); statusStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		items, err := statusStore.List(dbCtx, "")
		cancel()
		if err != nil {
			logger.Warn(idMethod+": load history threads from agent_status failed", logger.FieldError, err)
		} else {
			threads = appendThreadItems(threads, seen, items)
		}
	}
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	archivedMap, err := a.loadThreadArchiveMap(dbCtx)
	cancel()
	if err != nil {
		logger.Warn(idMethod+": load history threads from threadArchives.chat failed", logger.FieldError, err)
	} else {
		threads = appendArchivedThreads(threads, seen, archivedMap)
	}
	return threads
}

func appendArchivedThreads(threads []ThreadListItem, seen map[string]struct{}, archived map[string]int64) []ThreadListItem {
	type archivedEntry struct {
		ID string
		At int64
	}
	entries := make([]archivedEntry, 0, len(archived))
	for rawID, rawAt := range archived {
		id := strings.TrimSpace(rawID)
		if id == "" || rawAt <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		entries = append(entries, archivedEntry{ID: id, At: rawAt})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].At != entries[j].At {
			return entries[i].At > entries[j].At
		}
		return entries[i].ID < entries[j].ID
	})
	for _, item := range entries {
		threads = append(threads, ThreadListItem{
			ID:    item.ID,
			Name:  item.ID,
			State: "idle",
		})
		seen[item.ID] = struct{}{}
	}
	return threads
}

// ThreadReadOptions carries dependencies for thread/read.
type ThreadReadOptions struct {
	ThreadID string
}

// ThreadReadByID fetches codex history list for the target thread using constructor-time dependencies.
func (a *Adapter) ThreadReadByID(ctx context.Context, threadID string) (map[string]any, error) {
	return a.ThreadRead(ctx, ThreadReadOptions{
		ThreadID: threadID,
	})
}

// ThreadRead fetches codex history list for the target thread.
func (a *Adapter) ThreadRead(_ context.Context, opt ThreadReadOptions) (map[string]any, error) {
	return withProcess(a, "Server.threadRead", opt.ThreadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			threads, listErr := a.ListThreads(proc)
			if listErr != nil {
				return nil, listErr
			}
			return map[string]any{"history": threads}, nil
		})
}

// ThreadResolveOptions carries dependencies for thread/resolve.
type ThreadResolveOptions struct {
	ThreadID string
}

// ThreadResolveByID resolves thread identity using constructor-time dependencies.
func (a *Adapter) ThreadResolveByID(ctx context.Context, threadID string) (map[string]any, error) {
	return a.ThreadResolve(ctx, ThreadResolveOptions{
		ThreadID: threadID,
	})
}

// ThreadResolve resolves thread identity from runtime and history sources.
func (a *Adapter) ThreadResolve(ctx context.Context, opt ThreadResolveOptions) (map[string]any, error) {
	id := strings.TrimSpace(opt.ThreadID)
	if id == "" {
		return nil, apperrors.New("Server.threadResolve", "threadId is required")
	}
	result := map[string]any{
		"threadId": id,
	}

	var codexThreadID string
	resolveSource := "history"
	if a != nil && a.ctx != nil && a.ctx.Manager() != nil {
		for _, info := range a.ctx.Manager().List() {
			if strings.TrimSpace(info.ID) != id {
				continue
			}
			if state := strings.TrimSpace(string(info.State)); state != "" {
				result["state"] = state
			}
			if port := info.Port; port > 0 {
				result["port"] = port
			}
			codexThreadID = strings.TrimSpace(info.ThreadID)
			resolveSource = "running"
			break
		}
	}
	if codexThreadID == "" {
		candidates := a.ResolveCodexThreadCandidates(ctx, id, appendUniqueThreadIDFallback, PreviewResumeCandidates)
		if len(candidates) > 0 {
			codexThreadID = strings.TrimSpace(candidates[0])
		}
	}
	if codexThreadID != "" {
		result["codexThreadId"] = codexThreadID
	}
	if a.isLikelyCodexThreadID(codexThreadID) {
		result["uuid"] = codexThreadID
	}
	hasHistory := a.threadExistsInHistory(ctx, id)
	result["hasHistory"] = hasHistory
	logger.Info("thread/resolve: identity resolved",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"source", resolveSource,
		"state", result["state"],
		logger.FieldPort, result["port"],
		"codex_thread_id", codexThreadID,
		"has_history", hasHistory,
	)
	return result, nil
}

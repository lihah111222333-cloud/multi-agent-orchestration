package codexadapter

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ThreadStartOptions carries dependencies for thread/start.
type ThreadStartOptions struct {
	ThreadID         string
	Cwd              string
	Model            string
	ModelProvider    string
	ApprovalPolicy   string
	DynamicTools     []agentcore.DynamicTool
	StartInstructions string

	RegisterBinding func(context.Context, string, *runner.AgentProcess)
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
	if opt.RegisterBinding != nil {
		if proc := a.ctx.Manager().Get(result.ThreadID); proc != nil {
			opt.RegisterBinding(ctx, result.ThreadID, proc)
		}
	}
	if runtime := a.ctx.UIRuntime(); runtime != nil {
		runtime.ReplaceThreads(buildThreadSnapshots(a.ctx.Manager().List()))
	}
	return result, nil
}

// ThreadResumeOptions carries dependencies for thread/resume.
type ThreadResumeOptions struct {
	ThreadID                    string
	Path                        string
	Cwd                         string
	Model                       string
	WithThread                  func(string, func(*runner.AgentProcess) (any, error)) (any, error)
	ResolveCodexThreadCandidates func(context.Context, string) []string
	NormalizeCodexThreadID      func(string) string
}

// ThreadResumeResult is the normalized thread/resume payload.
type ThreadResumeResult struct {
	ThreadID string
	Status   string
	Model    string
}

// ThreadResume resumes a historical codex thread by candidate probing.
func (a *Adapter) ThreadResume(ctx context.Context, opt ThreadResumeOptions) (ThreadResumeResult, error) {
	threadID := strings.TrimSpace(opt.ThreadID)
	if threadID == "" {
		return ThreadResumeResult{}, apperrors.New("Server.threadResume", "threadId is required")
	}
	if opt.WithThread == nil {
		return ThreadResumeResult{}, apperrors.New("Server.threadResume", "thread resolver is not configured")
	}
	normalize := opt.NormalizeCodexThreadID
	if normalize == nil {
		normalize = strings.TrimSpace
	}
	out, err := opt.WithThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		resolved := []string(nil)
		if opt.ResolveCodexThreadCandidates != nil {
			resolved = opt.ResolveCodexThreadCandidates(ctx, threadID)
		}
		candidates := BuildResumeCandidates(threadID, resolved, normalize)
		logger.Info("thread/resume: resolved candidates",
			logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
			"candidate_count", len(candidates),
			"candidates", PreviewResumeCandidates(candidates, 4),
			"cwd", strings.TrimSpace(opt.Cwd),
		)

		resumedID, resumeErr := TryResumeCandidates(candidates, threadID, func(id string) error {
			return a.ResumeThread(proc, agentcore.ResumeThreadRequest{
				ThreadID: id,
				Path:     opt.Path,
				Cwd:      opt.Cwd,
			})
		}, IsHistoricalResumeCandidateError)
		if resumeErr != nil {
			return nil, apperrors.Wrap(resumeErr, "Server.threadResume", "resume thread")
		}
		_ = resumedID
		return ThreadResumeResult{
			ThreadID: threadID,
			Status:   "resumed",
			Model:    opt.Model,
		}, nil
	})
	if err != nil {
		return ThreadResumeResult{}, err
	}
	result, ok := out.(ThreadResumeResult)
	if !ok {
		return ThreadResumeResult{}, apperrors.New("Server.threadResume", "invalid resume result type")
	}
	return result, nil
}

// ThreadForkOptions carries dependencies for thread/fork.
type ThreadForkOptions struct {
	ThreadID    string
	WithThread  func(string, func(*runner.AgentProcess) (any, error)) (any, error)
	NowUnixMilli func() int64
}

// ThreadForkResult is the normalized thread/fork payload.
type ThreadForkResult struct {
	ThreadID   string
	ForkedFrom string
}

// ThreadFork creates a fork from source thread.
func (a *Adapter) ThreadFork(opt ThreadForkOptions) (ThreadForkResult, error) {
	threadID := strings.TrimSpace(opt.ThreadID)
	if threadID == "" {
		return ThreadForkResult{}, apperrors.New("Server.threadFork", "threadId is required")
	}
	if opt.WithThread == nil {
		return ThreadForkResult{}, apperrors.New("Server.threadFork", "thread resolver is not configured")
	}
	out, err := opt.WithThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		resp, forkErr := a.ForkThread(proc, agentcore.ForkThreadRequest{
			SourceThreadID: threadID,
		})
		if forkErr != nil {
			return nil, apperrors.Wrap(forkErr, "Server.threadFork", "fork thread")
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
	if err != nil {
		return ThreadForkResult{}, err
	}
	result, ok := out.(ThreadForkResult)
	if !ok {
		return ThreadForkResult{}, apperrors.New("Server.threadFork", "invalid fork result type")
	}
	return result, nil
}

// ThreadRollbackOptions carries dependencies for thread/rollback.
type ThreadRollbackOptions struct {
	ThreadID   string
	TurnIndex  int
	WithThread func(string, func(*runner.AgentProcess) (any, error)) (any, error)
}

// ThreadRollback sends /undo index command.
func (a *Adapter) ThreadRollback(opt ThreadRollbackOptions) (map[string]any, error) {
	threadID := strings.TrimSpace(opt.ThreadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadRollback", "threadId is required")
	}
	if opt.WithThread == nil {
		return nil, apperrors.New("Server.threadRollback", "thread resolver is not configured")
	}
	out, err := opt.WithThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		if cmdErr := a.SendCommand(proc, "/undo", fmt.Sprintf("%d", opt.TurnIndex)); cmdErr != nil {
			return nil, apperrors.Wrap(cmdErr, "Server.threadRollback", "send undo command")
		}
		return map[string]any{}, nil
	})
	if err != nil {
		return nil, err
	}
	result, ok := out.(map[string]any)
	if !ok {
		return nil, apperrors.New("Server.threadRollback", "invalid rollback result type")
	}
	return result, nil
}

// ReviewStartOptions carries dependencies for review/start.
type ReviewStartOptions struct {
	ThreadID   string
	Delivery   string
	WithThread func(string, func(*runner.AgentProcess) (any, error)) (any, error)
}

// ReviewStart dispatches /review command.
func (a *Adapter) ReviewStart(opt ReviewStartOptions) (map[string]any, error) {
	threadID := strings.TrimSpace(opt.ThreadID)
	if threadID == "" {
		return nil, apperrors.New("Server.reviewStart", "threadId is required")
	}
	if opt.WithThread == nil {
		return nil, apperrors.New("Server.reviewStart", "thread resolver is not configured")
	}
	out, err := opt.WithThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		if cmdErr := a.SendCommand(proc, "/review", opt.Delivery); cmdErr != nil {
			return nil, apperrors.Wrap(cmdErr, "Server.reviewStart", "send review command")
		}
		return map[string]any{}, nil
	})
	if err != nil {
		return nil, err
	}
	result, ok := out.(map[string]any)
	if !ok {
		return nil, apperrors.New("Server.reviewStart", "invalid review result type")
	}
	return result, nil
}

// TurnSteerOptions carries dependencies for turn/steer.
type TurnSteerOptions struct {
	ThreadID    string
	SubmitPrompt string
	Images      []string
	Files       []string
	WithThread  func(string, func(*runner.AgentProcess) (any, error)) (any, error)
}

// TurnSteer submits steering prompt to existing thread.
func (a *Adapter) TurnSteer(opt TurnSteerOptions) (map[string]any, error) {
	threadID := strings.TrimSpace(opt.ThreadID)
	if threadID == "" {
		return nil, apperrors.New("Server.turnSteer", "threadId is required")
	}
	if opt.WithThread == nil {
		return nil, apperrors.New("Server.turnSteer", "thread resolver is not configured")
	}
	out, err := opt.WithThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		if submitErr := a.Submit(proc, opt.SubmitPrompt, opt.Images, opt.Files, nil); submitErr != nil {
			return nil, submitErr
		}
		return map[string]any{}, nil
	})
	if err != nil {
		return nil, err
	}
	result, ok := out.(map[string]any)
	if !ok {
		return nil, apperrors.New("Server.turnSteer", "invalid steer result type")
	}
	return result, nil
}

// ThreadListItem models thread list entry payload.
type ThreadListItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// ThreadListOptions configures thread/list and thread/loaded/list behavior.
type ThreadListOptions struct {
	MethodName          string
	LoadThreadArchiveMap func(context.Context) (map[string]int64, error)
	LoadThreadAliases   func(context.Context) map[string]string
	ApplyThreadAliases  func([]ThreadListItem, map[string]string)
	SyncRuntime         bool
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

	threads = a.appendThreadHistoryFromStores(ctx, threads, seen, opt.MethodName, opt.LoadThreadArchiveMap)

	if opt.ApplyThreadAliases != nil && opt.LoadThreadAliases != nil {
		opt.ApplyThreadAliases(threads, opt.LoadThreadAliases(ctx))
	}
	if opt.SyncRuntime && a != nil && a.ctx != nil && a.ctx.UIRuntime() != nil {
		a.ctx.UIRuntime().ReplaceThreads(buildThreadSnapshotsFromListItems(threads))
	}
	return threads, nil
}

func (a *Adapter) appendThreadHistoryFromStores(
	ctx context.Context,
	threads []ThreadListItem,
	seen map[string]struct{},
	methodName string,
	loadThreadArchiveMap func(context.Context) (map[string]int64, error),
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
			threads = appendBindingThreads(threads, seen, bindings)
		}
	}
	if statusStore := a.ctx.AgentStatusStore(); statusStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		items, err := statusStore.List(dbCtx, "")
		cancel()
		if err != nil {
			logger.Warn(idMethod+": load history threads from agent_status failed", logger.FieldError, err)
		} else {
			threads = appendAgentStatusThreads(threads, seen, items)
		}
	}
	if loadThreadArchiveMap != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		archivedMap, err := loadThreadArchiveMap(dbCtx)
		cancel()
		if err != nil {
			logger.Warn(idMethod+": load history threads from threadArchives.chat failed", logger.FieldError, err)
		} else {
			threads = appendArchivedThreads(threads, seen, archivedMap)
		}
	}
	return threads
}

func appendBindingThreads(threads []ThreadListItem, seen map[string]struct{}, bindings []store.AgentCodexBinding) []ThreadListItem {
	for _, item := range bindings {
		agentID := strings.TrimSpace(item.AgentID)
		if agentID == "" {
			continue
		}
		if _, ok := seen[agentID]; ok {
			continue
		}
		threads = append(threads, ThreadListItem{
			ID:    agentID,
			Name:  agentID,
			State: "idle",
		})
		seen[agentID] = struct{}{}
	}
	return threads
}

func appendAgentStatusThreads(threads []ThreadListItem, seen map[string]struct{}, items []store.AgentStatus) []ThreadListItem {
	for _, item := range items {
		agentID := strings.TrimSpace(item.AgentID)
		if agentID == "" {
			continue
		}
		if _, ok := seen[agentID]; ok {
			continue
		}
		name := strings.TrimSpace(item.AgentName)
		if name == "" {
			name = agentID
		}
		threads = append(threads, ThreadListItem{
			ID:    agentID,
			Name:  name,
			State: "idle",
		})
		seen[agentID] = struct{}{}
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

func buildThreadSnapshots(items []runner.AgentInfo) []uistate.ThreadSnapshot {
	snapshots := make([]uistate.ThreadSnapshot, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		snapshots = append(snapshots, uistate.ThreadSnapshot{
			ID:    id,
			Name:  name,
			State: string(item.State),
		})
	}
	return snapshots
}

func buildThreadSnapshotsFromListItems(items []ThreadListItem) []uistate.ThreadSnapshot {
	snapshots := make([]uistate.ThreadSnapshot, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		snapshots = append(snapshots, uistate.ThreadSnapshot{
			ID:    id,
			Name:  name,
			State: item.State,
		})
	}
	return snapshots
}

// ThreadReadOptions carries dependencies for thread/read.
type ThreadReadOptions struct {
	ThreadID  string
	WithThread func(string, func(*runner.AgentProcess) (any, error)) (any, error)
}

// ThreadRead fetches codex history list for the target thread.
func (a *Adapter) ThreadRead(_ context.Context, opt ThreadReadOptions) (map[string]any, error) {
	threadID := strings.TrimSpace(opt.ThreadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadRead", "threadId is required")
	}
	if opt.WithThread == nil {
		return nil, apperrors.New("Server.threadRead", "thread resolver is not configured")
	}
	out, err := opt.WithThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		threads, listErr := a.ListThreads(proc)
		if listErr != nil {
			return nil, listErr
		}
		return map[string]any{"history": threads}, nil
	})
	if err != nil {
		return nil, err
	}
	result, ok := out.(map[string]any)
	if !ok {
		return nil, apperrors.New("Server.threadRead", "invalid read result type")
	}
	return result, nil
}

// ThreadResolveOptions carries dependencies for thread/resolve.
type ThreadResolveOptions struct {
	ThreadID                    string
	ResolvePrimaryCodexThreadID func(context.Context, string) string
	IsLikelyCodexThreadID       func(string) bool
	ThreadExistsInHistory       func(context.Context, string) bool
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
	if codexThreadID == "" && opt.ResolvePrimaryCodexThreadID != nil {
		codexThreadID = strings.TrimSpace(opt.ResolvePrimaryCodexThreadID(ctx, id))
	}
	if codexThreadID != "" {
		result["codexThreadId"] = codexThreadID
	}
	if opt.IsLikelyCodexThreadID != nil && opt.IsLikelyCodexThreadID(codexThreadID) {
		result["uuid"] = codexThreadID
	}
	hasHistory := false
	if opt.ThreadExistsInHistory != nil {
		hasHistory = opt.ThreadExistsInHistory(ctx, id)
	}
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

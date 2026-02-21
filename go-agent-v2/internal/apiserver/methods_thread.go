// methods_thread.go — thread/* JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type threadStartParams struct {
	Model                 string `json:"model,omitempty"`
	ModelProvider         string `json:"modelProvider,omitempty"`
	Cwd                   string `json:"cwd,omitempty"`
	ApprovalPolicy        string `json:"approvalPolicy,omitempty"`
	BaseInstructions      string `json:"baseInstructions,omitempty"`
	DeveloperInstructions string `json:"developerInstructions,omitempty"`
}

// threadInfo 通用线程信息。
type threadInfo struct {
	ID         string `json:"id"`
	Status     string `json:"status,omitempty"`
	ForkedFrom string `json:"forkedFrom,omitempty"`
}

// threadStartResponse thread/start 响应。
type threadStartResponse struct {
	Thread         threadInfo `json:"thread"`
	Model          string     `json:"model"`
	ModelProvider  string     `json:"modelProvider"`
	Cwd            string     `json:"cwd"`
	ApprovalPolicy string     `json:"approvalPolicy"`
}

func (s *Server) threadStartTyped(ctx context.Context, p threadStartParams) (any, error) {
	if p.Cwd == "" {
		p.Cwd = "."
	}

	id := fmt.Sprintf("thread-%d-%d", time.Now().UnixMilli(), s.threadSeq.Add(1))

	// 构建全部动态工具注入 agent (LSP + 编排 + 资源)
	dynamicTools := s.buildAllDynamicTools()

	// 解析 json-render 提示词 (支持用户自定义)
	jsonRenderPrompt := s.resolveJsonRenderPrompt(ctx)

	if err := s.mgr.Launch(ctx, id, id, "", p.Cwd, jsonRenderPrompt, dynamicTools); err != nil {
		return nil, apperrors.Wrap(err, "Server.threadStart", "launch thread")
	}
	if proc := s.mgr.Get(id); proc != nil {
		s.registerBinding(ctx, id, proc)
	}
	if s.uiRuntime != nil {
		s.uiRuntime.ReplaceThreads(buildThreadSnapshots(s.mgr.List()))
	}

	return threadStartResponse{
		Thread: threadInfo{
			ID:     id,
			Status: "running",
		},
		Model:          p.Model,
		ModelProvider:  p.ModelProvider,
		Cwd:            p.Cwd,
		ApprovalPolicy: p.ApprovalPolicy,
	}, nil
}

// threadResumeParams thread/resume 请求参数。
type threadResumeParams struct {
	ThreadID string `json:"threadId"`
	Path     string `json:"path,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
	Model    string `json:"model,omitempty"`
}

// threadResumeResponse thread/resume 响应。
type threadResumeResponse struct {
	Thread threadInfo `json:"thread"`
	Model  string     `json:"model"`
}

func (s *Server) threadResumeTyped(ctx context.Context, p threadResumeParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		candidates := buildResumeCandidates(p.ThreadID, s.resolveCodexThreadCandidates(ctx, p.ThreadID))
		logger.Info("thread/resume: resolved candidates",
			logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
			"candidate_count", len(candidates),
			"candidates", previewResumeCandidates(candidates, 4),
			"cwd", strings.TrimSpace(p.Cwd),
		)
		resumedID, err := tryResumeCandidates(candidates, p.ThreadID, func(id string) error {
			return proc.Client.ResumeThread(codex.ResumeThreadRequest{
				ThreadID: id,
				Path:     p.Path,
				Cwd:      p.Cwd,
			})
		})
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.threadResume", "resume thread")
		}
		_ = resumedID // logged inside tryResumeCandidates
		return threadResumeResponse{
			Thread: threadInfo{ID: p.ThreadID, Status: "resumed"},
			Model:  p.Model,
		}, nil
	})
}

type threadIDParams struct {
	ThreadID string `json:"threadId"`
}

// threadForkParams thread/fork 请求参数。
type threadForkParams struct {
	ThreadID  string `json:"threadId"`
	TurnIndex *int   `json:"turnIndex,omitempty"`
}

// threadForkResponse thread/fork 响应。
type threadForkResponse struct {
	Thread threadInfo `json:"thread"`
}

func (s *Server) threadForkTyped(_ context.Context, p threadForkParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		resp, err := proc.Client.ForkThread(codex.ForkThreadRequest{
			SourceThreadID: p.ThreadID,
		})
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.threadFork", "fork thread")
		}
		newID := resp.ThreadID
		if newID == "" {
			newID = fmt.Sprintf("thread-%d", time.Now().UnixMilli())
		}
		return threadForkResponse{
			Thread: threadInfo{ID: newID, ForkedFrom: p.ThreadID},
		}, nil
	})
}

func (s *Server) threadArchiveTyped(ctx context.Context, p threadIDParams) (any, error) {
	threadID := strings.TrimSpace(p.ThreadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadArchive", "threadId is required")
	}
	if !s.threadExistsForArchive(ctx, threadID) {
		return nil, apperrors.Newf("Server.threadArchive", "thread %s not found", threadID)
	}

	manifest, err := s.archiveThreadArtifacts(ctx, threadID)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadArchive", "archive codex artifacts")
	}
	archivedAt := time.Now().UnixMilli()
	if err := s.persistThreadArchivedState(ctx, threadID, archivedAt); err != nil {
		return nil, apperrors.Wrap(err, "Server.threadArchive", "persist archive state")
	}

	return map[string]any{
		"ok":            true,
		"threadId":      threadID,
		"archivedAt":    archivedAt,
		"codexThreadId": manifest.CodexThreadID,
		"archiveDir":    manifest.ArchiveDir,
		"rolloutPath":   manifest.RolloutPath,
		"files":         manifest.Files,
	}, nil
}

func (s *Server) threadUnarchiveTyped(ctx context.Context, p threadIDParams) (any, error) {
	threadID := strings.TrimSpace(p.ThreadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadUnarchive", "threadId is required")
	}
	archivedMap, err := s.loadThreadArchiveMap(ctx)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadUnarchive", "load archive state")
	}
	_, wasArchived := archivedMap[threadID]

	restoreNotice := threadArchiveRestoreNotice{
		Modified:      false,
		ManifestPath:  "",
		ModifiedFiles: []string{},
	}
	restoredFiles := []string{}
	skippedRestoreFiles := []string{}
	if wasArchived {
		restoreNotice, err = inspectThreadArchiveForRestore(threadID)
		if err != nil {
			logger.Error("thread/unarchive: inspect archive integrity failed",
				logger.FieldThreadID, threadID,
				logger.FieldError, err,
			)
		}
		restoredFiles, skippedRestoreFiles, err = restoreThreadArchiveSources(threadID)
		if err != nil {
			logger.Error("thread/unarchive: restore archived codex artifacts failed",
				logger.FieldThreadID, threadID,
				logger.FieldError, err,
			)
			restoredFiles = []string{}
			skippedRestoreFiles = []string{}
		}
	}
	if err := s.removeThreadArchivedState(ctx, threadID); err != nil {
		return nil, apperrors.Wrap(err, "Server.threadUnarchive", "persist archive state")
	}
	result := map[string]any{
		"ok":       true,
		"threadId": threadID,
	}
	if len(restoredFiles) > 0 {
		result["restoredFiles"] = restoredFiles
	}
	if len(skippedRestoreFiles) > 0 {
		result["restoreSkippedFiles"] = skippedRestoreFiles
	}
	if restoreNotice.Modified {
		result["archiveModified"] = true
		result["manifestPath"] = restoreNotice.ManifestPath
		result["modifiedFiles"] = restoreNotice.ModifiedFiles
	}
	return result, nil
}

// threadNameSetParams thread/name/set 请求参数。
type threadNameSetParams struct {
	ThreadID string `json:"threadId"`
	Name     string `json:"name"`
}

func (s *Server) threadNameSetTyped(ctx context.Context, p threadNameSetParams) (any, error) {
	threadID := strings.TrimSpace(p.ThreadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadNameSet", "threadId is required")
	}
	requestedName := strings.TrimSpace(p.Name)
	persistedAlias := requestedName
	if persistedAlias == threadID {
		persistedAlias = ""
	}
	renameTarget := requestedName
	if renameTarget == "" {
		renameTarget = threadID
	}

	var proc *runner.AgentProcess
	if s.mgr != nil {
		proc = s.mgr.Get(threadID)
	}
	existsInRuntime := false
	if s.uiRuntime != nil {
		existsInRuntime = hasThread(s.uiRuntime.SnapshotLight().Threads, threadID)
	}
	if proc == nil && !existsInRuntime && !s.threadExistsInHistory(ctx, threadID) {
		return nil, apperrors.Newf("Server.threadNameSet", "thread %s not found", threadID)
	}

	if proc != nil && renameTarget != "" {
		if err := proc.Client.SendCommand("/rename", renameTarget); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "send rename command")
		}
	}

	if s.uiRuntime != nil {
		s.uiRuntime.SetThreadName(threadID, persistedAlias)
	}
	if err := s.persistThreadAlias(ctx, threadID, persistedAlias); err != nil {
		logger.Warn("thread/name/set: persist alias failed",
			logger.FieldThreadID, threadID,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrap(err, "Server.threadNameSet", "persist thread alias")
	}
	return map[string]any{}, nil
}

func (s *Server) threadCompact(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/compact")
}

// threadRollbackParams thread/rollback 请求参数。
type threadRollbackParams struct {
	ThreadID  string `json:"threadId"`
	TurnIndex int    `json:"turnIndex"`
}

func (s *Server) threadRollbackTyped(_ context.Context, p threadRollbackParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand("/undo", fmt.Sprintf("%d", p.TurnIndex)); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadRollback", "send undo command")
		}
		return map[string]any{}, nil
	})
}

// threadListItem thread/list 响应项。
type threadListItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// threadListResponse thread/list 响应。
type threadListResponse struct {
	Threads []threadListItem `json:"threads"`
}

func buildThreadSnapshots(agents []runner.AgentInfo) []uistate.ThreadSnapshot {
	snapshots := make([]uistate.ThreadSnapshot, 0, len(agents))
	for _, item := range agents {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.ID
		}
		snapshots = append(snapshots, uistate.ThreadSnapshot{
			ID:    item.ID,
			Name:  name,
			State: string(item.State),
		})
	}
	return snapshots
}

func buildThreadSnapshotsFromListItems(items []threadListItem) []uistate.ThreadSnapshot {
	snapshots := make([]uistate.ThreadSnapshot, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.ID
		}
		snapshots = append(snapshots, uistate.ThreadSnapshot{
			ID:    item.ID,
			Name:  name,
			State: item.State,
		})
	}
	return snapshots
}

func appendBindingThreads(threads []threadListItem, seen map[string]struct{}, bindings []store.AgentCodexBinding) []threadListItem {
	for _, item := range bindings {
		agentID := strings.TrimSpace(item.AgentID)
		if agentID == "" {
			continue
		}
		if _, ok := seen[agentID]; ok {
			continue
		}
		threads = append(threads, threadListItem{
			ID:    agentID,
			Name:  agentID,
			State: "idle",
		})
		seen[agentID] = struct{}{}
	}
	return threads
}

func appendAgentStatusThreads(threads []threadListItem, seen map[string]struct{}, items []store.AgentStatus) []threadListItem {
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
		// 来自 agent_status 的“历史兜底线程”并非当前运行实例，
		// 重启后 UI 应展示为等待指令，而不是沿用旧的 running/stuck 状态。
		state := "idle"
		threads = append(threads, threadListItem{
			ID:    agentID,
			Name:  name,
			State: state,
		})
		seen[agentID] = struct{}{}
	}
	return threads
}

func (s *Server) appendThreadHistoryFromStores(ctx context.Context, threads []threadListItem, seen map[string]struct{}, methodName string) []threadListItem {
	// DB 历史兜底 #1: agent_codex_binding (Codex 会话绑定)
	if s.bindingStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		bindings, err := s.bindingStore.ListAll(dbCtx)
		cancel()
		if err != nil {
			logger.Warn(methodName+": load history threads from agent_codex_binding failed", logger.FieldError, err)
		} else {
			threads = appendBindingThreads(threads, seen, bindings)
		}
	}

	// DB 历史兜底 #2: agent_status (补充 agent 名称与兜底状态)
	if s.agentStatusStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		statusItems, err := s.agentStatusStore.List(dbCtx, "")
		cancel()
		if err != nil {
			logger.Warn(methodName+": load history threads from agent_status failed", logger.FieldError, err)
		} else {
			threads = appendAgentStatusThreads(threads, seen, statusItems)
		}
	}
	return threads
}

func (s *Server) threadList(ctx context.Context, _ json.RawMessage) (any, error) {
	agents := []runner.AgentInfo{}
	if s.mgr != nil {
		agents = s.mgr.List()
	}

	threads := make([]threadListItem, 0, len(agents)+32)
	seen := make(map[string]struct{}, len(agents)+32)

	for _, a := range agents {
		if a.ID == "" {
			continue
		}
		threads = append(threads, threadListItem{
			ID:    a.ID,
			Name:  a.Name,
			State: string(a.State),
		})
		seen[a.ID] = struct{}{}
	}

	threads = s.appendThreadHistoryFromStores(ctx, threads, seen, "thread/list")
	applyThreadAliases(threads, s.loadThreadAliases(ctx))
	if s.uiRuntime != nil {
		s.uiRuntime.ReplaceThreads(buildThreadSnapshotsFromListItems(threads))
	}

	return threadListResponse{Threads: threads}, nil
}

// threadLoadedListResponse thread/loaded/list 响应。
type threadLoadedListResponse struct {
	Threads []threadListItem `json:"threads"`
}

func (s *Server) threadLoadedList(ctx context.Context, _ json.RawMessage) (any, error) {
	// 历史线程也视为可选会话：前端可直接选择，首次 turn/start 时自动补加载。
	agents := []runner.AgentInfo{}
	if s.mgr != nil {
		agents = s.mgr.List()
	}
	threads := make([]threadListItem, 0, len(agents)+32)
	seen := make(map[string]struct{}, len(agents)+32)

	for _, a := range agents {
		if a.ID == "" {
			continue
		}
		threads = append(threads, threadListItem{
			ID:    a.ID,
			Name:  a.Name,
			State: string(a.State),
		})
		seen[a.ID] = struct{}{}
	}

	threads = s.appendThreadHistoryFromStores(ctx, threads, seen, "thread/loaded/list")
	applyThreadAliases(threads, s.loadThreadAliases(ctx))

	return threadLoadedListResponse{Threads: threads}, nil
}

func (s *Server) threadReadTyped(ctx context.Context, p threadIDParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		threads, err := proc.Client.ListThreads()
		if err != nil {
			return nil, err
		}
		return map[string]any{"history": threads}, nil
	})
}

func (s *Server) threadResolveTyped(ctx context.Context, p threadIDParams) (any, error) {
	id := strings.TrimSpace(p.ThreadID)
	if id == "" {
		return nil, apperrors.New("Server.threadResolve", "threadId is required")
	}

	result := map[string]any{
		"threadId": id,
	}

	var codexThreadID string
	resolveSource := "history"
	for _, info := range s.mgr.List() {
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

	if codexThreadID == "" {
		codexThreadID = strings.TrimSpace(s.resolvePrimaryCodexThreadID(ctx, id))
	}
	if codexThreadID != "" {
		result["codexThreadId"] = codexThreadID
	}
	if isLikelyCodexThreadID(codexThreadID) {
		result["uuid"] = codexThreadID
	}
	result["hasHistory"] = s.threadExistsInHistory(ctx, id)
	logger.Info("thread/resolve: identity resolved",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"source", resolveSource,
		"state", result["state"],
		logger.FieldPort, result["port"],
		"codex_thread_id", codexThreadID,
		"has_history", result["hasHistory"],
	)

	return result, nil
}

// threadMessagesParams thread/messages 请求参数。
type threadMessagesParams struct {
	ThreadID string `json:"threadId"`
	Limit    int    `json:"limit,omitempty"`
	Before   int64  `json:"before,omitempty"` // cursor: id < before
}

const (
	threadMessageHydrationMaxRecords = 20000
	threadMessageHydrationPageSize   = 500
)

func (s *Server) threadMessagesTyped(ctx context.Context, p threadMessagesParams) (any, error) {
	if p.ThreadID == "" {
		return nil, apperrors.New("Server.threadMessages", "threadId is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	allMsgs, err := s.loadAllThreadMessagesFromCodexRollout(ctx, p.ThreadID)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadMessages", "load codex rollout messages")
	}
	total := int64(len(allMsgs))
	msgs := paginateRolloutMessages(allMsgs, p.Limit, p.Before)
	logger.Info("thread/messages: page selected",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		"before", p.Before,
		"limit", p.Limit,
		"page_count", len(msgs),
		"total", total,
	)

	// 第一页立即返回, 剩余页后台流式加载 + 通知
	if s.uiRuntime != nil && p.Before == 0 {
		firstRecords := msgsToRecords(msgs)
		hydrated := s.uiRuntime.HydrateHistory(p.ThreadID, firstRecords)
		logger.Debug("thread/messages: first page hydrated",
			logger.FieldAgentID, p.ThreadID,
			"first_page_count", len(msgs),
			"total", total,
			"hydrated", hydrated,
		)

		if hydrated {
			hydrateLimit := calculateHydrationLoadLimit(len(msgs), total)
			if hydrateLimit > len(msgs) {
				threadID := p.ThreadID
				allCopy := append([]threadHistoryMessage(nil), allMsgs...)
				firstCopy := append([]threadHistoryMessage(nil), msgs...)
				util.SafeGo(func() { s.streamRemainingHistory(threadID, allCopy, firstCopy, hydrateLimit) })
			}
		}
	} else if s.uiRuntime != nil {
		// 翻页请求: 直接 hydrate 当前页
		records := msgsToRecords(msgs)
		_ = s.uiRuntime.HydrateHistory(p.ThreadID, records)
	}

	diffLen := 0
	timelineLen := 0
	if s.uiRuntime != nil {
		diffLen = len(s.uiRuntime.ThreadDiff(p.ThreadID))
		timelineLen = len(s.uiRuntime.ThreadTimeline(p.ThreadID))
	}
	logger.Info("thread/messages: response prepared",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		"page_count", len(msgs),
		"total", total,
		"timeline_len", timelineLen,
		"diff_len", diffLen,
	)

	return map[string]any{
		"messages": msgs,
		"total":    total,
	}, nil
}

// streamRemainingHistory 后台分页加载剩余历史, 加载完后通过 AppendHistory 追加到 timeline。
//
// firstPage 已通过 HydrateHistory 加载, 此处只加载后续页并追加。
func (s *Server) streamRemainingHistory(threadID string, all []threadHistoryMessage, firstPage []threadHistoryMessage, limit int) {
	if s.uiRuntime == nil || len(all) == 0 || limit <= 0 || limit <= len(firstPage) {
		return
	}

	before := int64(0)
	if len(firstPage) > 0 {
		before = firstPage[len(firstPage)-1].ID
	}

	// 只累积后续页 (不含 firstPage)
	remaining := make([]threadHistoryMessage, 0, limit-len(firstPage))
	pageNum := 1
	loaded := len(firstPage)

	for loaded < limit {
		batchLimit := min(threadMessageHydrationPageSize, limit-loaded)
		batch := paginateRolloutMessages(all, batchLimit, before)
		if len(batch) == 0 {
			break
		}

		remaining = append(remaining, batch...)
		pageNum++
		loaded += len(batch)

		if len(batch) < batchLimit {
			break
		}
		before = batch[len(batch)-1].ID
	}

	if len(remaining) == 0 {
		return
	}

	// 追加到已有 timeline (不重置)
	records := msgsToRecords(remaining)
	s.uiRuntime.AppendHistory(threadID, records)
	diffLen := len(s.uiRuntime.ThreadDiff(threadID))
	timelineLen := len(s.uiRuntime.ThreadTimeline(threadID))

	// 通知前端 timeline 已更新
	s.Notify("thread/messages/page", map[string]any{
		"threadId":   threadID,
		"totalCount": loaded,
		"pages":      pageNum,
	})

	logger.Debug("thread/messages: streaming hydration complete",
		logger.FieldAgentID, threadID,
		"total_loaded", loaded,
		"pages", pageNum,
	)
	logger.Info("thread/messages: streaming page notified",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		"total_loaded", loaded,
		"pages", pageNum,
		"timeline_len", timelineLen,
		"diff_len", diffLen,
	)
}

// msgsToRecords 将消息列表转为 hydration 记录。
func msgsToRecords(msgs []threadHistoryMessage) []uistate.HistoryRecord {
	records := make([]uistate.HistoryRecord, 0, len(msgs))
	for _, msg := range msgs {
		records = append(records, uistate.HistoryRecord{
			ID:        msg.ID,
			Role:      msg.Role,
			EventType: msg.EventType,
			Method:    msg.Method,
			Content:   msg.Content,
			Metadata:  msg.Metadata,
			CreatedAt: msg.CreatedAt,
		})
	}
	return records
}

type threadHistoryMessage struct {
	ID        int64           `json:"id"`
	AgentID   string          `json:"agentId"`
	Role      string          `json:"role"`
	EventType string          `json:"eventType"`
	Method    string          `json:"method"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

func calculateHydrationLoadLimit(initialCount int, total int64) int {
	if initialCount < 0 {
		initialCount = 0
	}
	limit := initialCount
	if total > int64(limit) {
		limit = int(total)
	}
	if limit > threadMessageHydrationMaxRecords {
		limit = threadMessageHydrationMaxRecords
	}
	return limit
}

func parseRolloutTimestamp(raw string) time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	return time.Time{}
}

func (s *Server) resolveRolloutHistorySource(ctx context.Context, threadID string) (codexThreadID string, rolloutPath string) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", ""
	}

	if s.mgr != nil {
		if proc := s.mgr.Get(id); proc != nil && proc.Client != nil {
			candidate := normalizeCodexThreadID(proc.Client.GetThreadID())
			if candidate != "" {
				return candidate, ""
			}
		}
	}

	if s.bindingStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		binding, err := s.bindingStore.FindByAgentID(dbCtx, id)
		cancel()
		if err == nil && binding != nil {
			candidate := normalizeCodexThreadID(binding.CodexThreadID)
			if candidate != "" {
				return candidate, strings.TrimSpace(binding.RolloutPath)
			}
		}
	}

	if s.agentStatusStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		status, err := s.agentStatusStore.Get(dbCtx, id)
		cancel()
		if err == nil && status != nil {
			candidate := normalizeCodexThreadID(status.SessionID)
			if candidate != "" {
				return candidate, ""
			}
		}
	}

	if candidate := normalizeCodexThreadID(id); candidate != "" {
		return candidate, ""
	}
	return "", ""
}

func paginateRolloutMessages(all []threadHistoryMessage, limit int, before int64) []threadHistoryMessage {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(all) == 0 {
		return []threadHistoryMessage{}
	}

	page := make([]threadHistoryMessage, 0, min(limit, len(all)))
	for idx := len(all) - 1; idx >= 0; idx-- {
		item := all[idx]
		if before > 0 && item.ID >= before {
			continue
		}
		page = append(page, item)
		if len(page) >= limit {
			break
		}
	}
	return page
}

func (s *Server) loadAllThreadMessagesFromCodexRollout(ctx context.Context, threadID string) ([]threadHistoryMessage, error) {
	codexThreadID, rolloutPath := s.resolveRolloutHistorySource(ctx, threadID)
	codexThreadID = normalizeCodexThreadID(codexThreadID)
	if codexThreadID == "" {
		return []threadHistoryMessage{}, nil
	}

	path := strings.TrimSpace(rolloutPath)
	if path == "" {
		resolvedPath, err := codex.FindRolloutPath(codexThreadID)
		if err != nil {
			return []threadHistoryMessage{}, nil
		}
		path = resolvedPath
	}
	if path == "" {
		return []threadHistoryMessage{}, nil
	}
	if _, err := os.Stat(path); err != nil {
		return []threadHistoryMessage{}, nil
	}

	rolloutMsgs, err := codex.ReadRolloutMessages(path)
	if err != nil {
		return nil, err
	}
	if len(rolloutMsgs) == 0 {
		return []threadHistoryMessage{}, nil
	}

	all := make([]threadHistoryMessage, 0, len(rolloutMsgs))
	for i, item := range rolloutMsgs {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		createdAt := parseRolloutTimestamp(item.Timestamp)
		eventType := ""
		if role == "assistant" {
			eventType = codex.EventAgentMessage
		}
		all = append(all, threadHistoryMessage{
			ID:        int64(i + 1),
			AgentID:   threadID,
			Role:      role,
			EventType: eventType,
			Method:    "",
			Content:   item.Content,
			Metadata:  nil,
			CreatedAt: createdAt,
		})
	}
	if len(all) == 0 {
		return []threadHistoryMessage{}, nil
	}

	return all, nil
}

type threadArchiveFile struct {
	Kind         string `json:"kind"`
	SourcePath   string `json:"sourcePath"`
	ArchivedPath string `json:"archivedPath"`
	SizeBytes    int64  `json:"sizeBytes"`
	SHA256       string `json:"sha256,omitempty"`
}

type threadArchiveManifest struct {
	ThreadID      string              `json:"threadId"`
	CodexThreadID string              `json:"codexThreadId,omitempty"`
	ArchivedAt    string              `json:"archivedAt"`
	ArchiveDir    string              `json:"archiveDir"`
	RolloutPath   string              `json:"rolloutPath,omitempty"`
	Files         []threadArchiveFile `json:"files"`
}

type threadArtifactCandidate struct {
	Kind string
	Path string
}

func (s *Server) threadExistsForArchive(ctx context.Context, threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	if s.mgr != nil && s.mgr.Get(id) != nil {
		return true
	}
	if s.uiRuntime != nil && hasThread(s.uiRuntime.SnapshotLight().Threads, id) {
		return true
	}
	return s.threadExistsInHistory(ctx, id)
}

func (s *Server) persistThreadArchivedState(ctx context.Context, threadID string, archivedAt int64) error {
	if s.prefManager == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	archivedMap, err := s.loadThreadArchiveMap(ctx)
	if err != nil {
		return err
	}
	if archivedAt <= 0 {
		archivedAt = time.Now().UnixMilli()
	}
	archivedMap[id] = archivedAt
	return s.prefManager.Set(ctx, prefThreadArchivesChat, archivedMap)
}

func (s *Server) removeThreadArchivedState(ctx context.Context, threadID string) error {
	if s.prefManager == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	archivedMap, err := s.loadThreadArchiveMap(ctx)
	if err != nil {
		return err
	}
	delete(archivedMap, id)
	return s.prefManager.Set(ctx, prefThreadArchivesChat, archivedMap)
}

func (s *Server) loadThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	if s.prefManager == nil {
		return map[string]int64{}, nil
	}
	value, err := s.prefManager.Get(ctx, prefThreadArchivesChat)
	if err != nil {
		return nil, err
	}
	return normalizeThreadArchiveMap(value), nil
}

func normalizeThreadArchiveMap(value any) map[string]int64 {
	result := map[string]int64{}
	appendEntry := func(rawID string, rawAt any) {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return
		}
		var at int64
		switch v := rawAt.(type) {
		case int:
			at = int64(v)
		case int64:
			at = v
		case float64:
			at = int64(v)
		case json.Number:
			parsed, err := v.Int64()
			if err == nil {
				at = parsed
			}
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err == nil {
				at = parsed
			}
		}
		if at <= 0 {
			return
		}
		result[id] = at
	}

	switch typed := value.(type) {
	case map[string]int64:
		for id, at := range typed {
			appendEntry(id, at)
		}
	case map[string]any:
		for id, at := range typed {
			appendEntry(id, at)
		}
	case string:
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &decoded); err == nil {
			for id, at := range decoded {
				appendEntry(id, at)
			}
		}
	case json.RawMessage:
		decoded := map[string]any{}
		if err := json.Unmarshal(typed, &decoded); err == nil {
			for id, at := range decoded {
				appendEntry(id, at)
			}
		}
	}

	return result
}

func (s *Server) archiveThreadArtifacts(ctx context.Context, threadID string) (threadArchiveManifest, error) {
	id := strings.TrimSpace(threadID)
	manifest := threadArchiveManifest{
		ThreadID:   id,
		ArchivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Files:      []threadArchiveFile{},
	}
	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive root")
	}
	archiveDir, err := resolveThreadArchiveSnapshotDir(rootDir, id, manifest.ArchivedAt)
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive dir")
	}
	manifest.ArchiveDir = archiveDir
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "ensure archive dir")
	}

	codexThreadID, rolloutPath := s.resolveRolloutHistorySource(ctx, id)
	manifest.CodexThreadID = normalizeCodexThreadID(codexThreadID)
	candidates := collectThreadArtifactCandidates(manifest.CodexThreadID, rolloutPath)

	for _, candidate := range candidates {
		srcPath := strings.TrimSpace(candidate.Path)
		if srcPath == "" {
			continue
		}
		info, err := os.Stat(srcPath)
		if err != nil || info.IsDir() {
			continue
		}
		targetPath, err := nextArchiveFilePath(archiveDir, filepath.Base(srcPath))
		if err != nil {
			return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive target")
		}
		if err := copyFile(srcPath, targetPath); err != nil {
			logger.Error("thread/archive: copy artifact failed",
				logger.FieldThreadID, id,
				"source_path", srcPath,
				"target_path", targetPath,
				logger.FieldError, err,
			)
			continue
		}
		fileMeta := threadArchiveFile{
			Kind:         candidate.Kind,
			SourcePath:   srcPath,
			ArchivedPath: targetPath,
			SizeBytes:    info.Size(),
		}
		checksum, err := fileSHA256(targetPath)
		if err != nil {
			return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "compute archived file checksum")
		}
		fileMeta.SHA256 = checksum
		manifest.Files = append(manifest.Files, fileMeta)
		if manifest.RolloutPath == "" && candidate.Kind == "rollout" {
			manifest.RolloutPath = targetPath
		}
	}
	sort.SliceStable(manifest.Files, func(i, j int) bool {
		return manifest.Files[i].ArchivedPath < manifest.Files[j].ArchivedPath
	})

	if err := writeThreadArchiveManifest(manifest); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "write manifest")
	}
	if s.bindingStore != nil && manifest.CodexThreadID != "" && strings.TrimSpace(manifest.RolloutPath) != "" {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.bindingStore.Bind(dbCtx, id, manifest.CodexThreadID, manifest.RolloutPath)
		cancel()
		if err != nil {
			logger.Warn("thread/archive: persist rollout path failed",
				logger.FieldThreadID, id,
				"codex_thread_id", manifest.CodexThreadID,
				"rollout_path", manifest.RolloutPath,
				logger.FieldError, err,
			)
		}
	}
	pruneArchivedCodexSourceFiles(id, manifest.Files, manifest.ArchiveDir)
	return manifest, nil
}

func resolveThreadArchiveRootDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveRootDir", "resolve user home")
	}
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return "", apperrors.New("resolveThreadArchiveRootDir", "user home is empty")
	}
	appRootDir := filepath.Join(homeDir, ".multi-agent")
	if err := os.MkdirAll(appRootDir, 0o755); err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveRootDir", "ensure app root")
	}
	archiveRoot := filepath.Join(appRootDir, "thread-archives")
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveRootDir", "ensure archive root")
	}
	return archiveRoot, nil
}

func resolveThreadArchiveSnapshotDir(rootDir string, threadID string, archivedAt string) (string, error) {
	safeThreadID, err := sanitizeArchiveNameStrict(threadID)
	if err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveSnapshotDir", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveSnapshotDir", "ensure thread dir")
	}
	snapshotName, err := sanitizeArchiveNameStrict(strings.TrimSpace(archivedAt))
	if err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveSnapshotDir", "sanitize archive timestamp")
	}
	snapshotDir := filepath.Join(threadDir, snapshotName)
	if _, err := os.Stat(snapshotDir); os.IsNotExist(err) {
		return snapshotDir, nil
	} else if err != nil {
		return "", apperrors.Wrap(err, "resolveThreadArchiveSnapshotDir", "stat snapshot dir")
	}
	for i := 2; i <= 9999; i++ {
		candidate := filepath.Join(threadDir, fmt.Sprintf("%s-%d", snapshotName, i))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", apperrors.Wrap(err, "resolveThreadArchiveSnapshotDir", "stat snapshot candidate")
		}
	}
	return "", apperrors.New("resolveThreadArchiveSnapshotDir", "unable to allocate unique archive snapshot dir")
}

func collectThreadArtifactCandidates(codexThreadID string, rolloutPath string) []threadArtifactCandidate {
	candidates := make([]threadArtifactCandidate, 0, 8)
	seen := make(map[string]struct{}, 8)
	addCandidate := func(kind, path string) {
		cleaned := strings.TrimSpace(path)
		if cleaned == "" {
			return
		}
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		candidates = append(candidates, threadArtifactCandidate{Kind: kind, Path: cleaned})
	}

	resolvedRollout := strings.TrimSpace(rolloutPath)
	if resolvedRollout == "" && strings.TrimSpace(codexThreadID) != "" {
		if found, err := codex.FindRolloutPath(codexThreadID); err == nil {
			resolvedRollout = found
		}
	}
	if resolvedRollout != "" {
		addCandidate("rollout", resolvedRollout)
	}
	if strings.TrimSpace(codexThreadID) == "" {
		return candidates
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return candidates
	}
	addCandidate("shell_snapshot", filepath.Join(homeDir, ".codex", "shell_snapshots", codexThreadID+".sh"))

	searchRoots := []string{
		filepath.Join(homeDir, ".codex", "sessions"),
		filepath.Join(homeDir, ".codex", "shell_snapshots"),
		filepath.Join(homeDir, ".codex", "archived_sessions"),
		filepath.Join(homeDir, ".codex", "tmp"),
	}
	for _, root := range searchRoots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d == nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.Contains(name, codexThreadID) {
				return nil
			}
			addCandidate(inferThreadArtifactKind(name), path)
			return nil
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].Path < candidates[j].Path
	})
	return candidates
}

func inferThreadArtifactKind(filename string) string {
	lower := strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasPrefix(lower, "rollout-") && strings.HasSuffix(lower, ".jsonl"):
		return "rollout"
	case strings.Contains(lower, "bp"):
		return "breakpoint"
	case strings.HasSuffix(lower, ".sh"):
		return "shell_snapshot"
	case strings.HasSuffix(lower, ".jsonl"):
		return "jsonl"
	default:
		return "artifact"
	}
}

func sanitizeArchiveName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._")
}

func sanitizeArchiveNameStrict(raw string) (string, error) {
	sanitized := sanitizeArchiveName(raw)
	if sanitized == "" {
		return "", apperrors.Newf("sanitizeArchiveNameStrict", "invalid archive name from %q", raw)
	}
	return sanitized, nil
}

func nextArchiveFilePath(dir, filename string) (string, error) {
	base, err := sanitizeArchiveNameStrict(filepath.Base(filename))
	if err != nil {
		return "", apperrors.Wrap(err, "nextArchiveFilePath", "sanitize filename")
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	candidate := filepath.Join(dir, base)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, nil
	} else if err != nil {
		return "", apperrors.Wrap(err, "nextArchiveFilePath", "stat archive target")
	}
	for i := 2; i <= 9999; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", apperrors.Wrap(err, "nextArchiveFilePath", "stat archive target candidate")
		}
	}
	return "", apperrors.New("nextArchiveFilePath", "unable to allocate unique archive filename")
}

func copyFile(srcPath, targetPath string) error {
	if _, err := os.Stat(targetPath); err == nil {
		return apperrors.Newf("copyFile", "target already exists: %s", targetPath)
	} else if !os.IsNotExist(err) {
		return apperrors.Wrap(err, "copyFile", "stat target path")
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	info, err := src.Stat()
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}

	tmpPath := targetPath + ".tmp"
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, targetPath)
}

func copyFileOverwrite(srcPath, targetPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	info, err := src.Stat()
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}

	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(targetDir, "."+filepath.Base(targetPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := io.Copy(tmpFile, src); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func pruneArchivedCodexSourceFiles(threadID string, files []threadArchiveFile, archiveDir string) {
	if len(files) == 0 {
		return
	}
	codexRoot, err := resolveCodexRootDir()
	if err != nil {
		logger.Error("thread/archive: resolve codex root failed",
			logger.FieldThreadID, threadID,
			logger.FieldError, err,
		)
		return
	}

	archiveRoot := strings.TrimSpace(archiveDir)
	seen := make(map[string]struct{}, len(files))
	deleted := 0
	for _, meta := range files {
		srcPath := strings.TrimSpace(meta.SourcePath)
		if srcPath == "" {
			continue
		}
		if _, ok := seen[srcPath]; ok {
			continue
		}
		seen[srcPath] = struct{}{}

		withinCodex, err := pathWithinRoot(codexRoot, srcPath)
		if err != nil || !withinCodex {
			continue
		}
		if archiveRoot != "" {
			if withinArchive, err := pathWithinRoot(archiveRoot, srcPath); err == nil && withinArchive {
				continue
			}
		}

		info, err := os.Stat(srcPath)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Error("thread/archive: stat source artifact failed",
					logger.FieldThreadID, threadID,
					"source_path", srcPath,
					logger.FieldError, err,
				)
			}
			continue
		}
		if info.IsDir() {
			continue
		}

		expectedSHA256 := strings.TrimSpace(meta.SHA256)
		if expectedSHA256 == "" {
			continue
		}
		sourceSHA256, err := fileSHA256(srcPath)
		if err != nil {
			logger.Error("thread/archive: source artifact checksum failed",
				logger.FieldThreadID, threadID,
				"source_path", srcPath,
				logger.FieldError, err,
			)
			continue
		}
		if !strings.EqualFold(expectedSHA256, sourceSHA256) {
			logger.Error("thread/archive: source artifact changed after backup, skip delete",
				logger.FieldThreadID, threadID,
				"source_path", srcPath,
				"expected_sha256", expectedSHA256,
				"actual_sha256", sourceSHA256,
			)
			continue
		}

		if err := os.Remove(srcPath); err != nil {
			logger.Error("thread/archive: remove source artifact failed",
				logger.FieldThreadID, threadID,
				"source_path", srcPath,
				logger.FieldError, err,
			)
			continue
		}
		deleted++
		removeEmptyCodexParentDirs(filepath.Dir(srcPath), codexRoot)
	}

	if deleted > 0 {
		logger.Info("thread/archive: pruned codex source artifacts",
			logger.FieldThreadID, threadID,
			"deleted_count", deleted,
		)
	}
}

func resolveCodexRootDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", apperrors.Wrap(err, "resolveCodexRootDir", "resolve user home")
	}
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return "", apperrors.New("resolveCodexRootDir", "user home is empty")
	}
	return filepath.Join(homeDir, ".codex"), nil
}

func removeEmptyCodexParentDirs(startDir string, codexRoot string) {
	current := strings.TrimSpace(startDir)
	root := strings.TrimSpace(codexRoot)
	if current == "" || root == "" {
		return
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for current != "" {
		currentAbs, err := filepath.Abs(current)
		if err != nil {
			return
		}
		if currentAbs == rootAbs {
			return
		}
		withinRoot, err := pathWithinRoot(rootAbs, currentAbs)
		if err != nil || !withinRoot {
			return
		}
		entries, err := os.ReadDir(currentAbs)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(currentAbs); err != nil {
			return
		}
		parent := filepath.Dir(currentAbs)
		if parent == currentAbs {
			return
		}
		current = parent
	}
}

func restoreThreadArchiveSources(threadID string) ([]string, []string, error) {
	restored := []string{}
	skipped := []string{}

	id := strings.TrimSpace(threadID)
	if id == "" {
		return restored, skipped, nil
	}
	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "resolve archive root")
	}
	safeThreadID, err := sanitizeArchiveNameStrict(id)
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	manifestPath, err := findLatestThreadArchiveManifestPath(threadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return restored, skipped, nil
		}
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "find latest manifest")
	}
	manifest, err := readThreadArchiveManifest(manifestPath)
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "read manifest")
	}
	codexRoot, err := resolveCodexRootDir()
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "resolve codex root")
	}

	restoredSet := map[string]struct{}{}
	skippedSet := map[string]struct{}{}
	appendSkipped := func(sourcePath string, archivedPath string, reason string, skipErr error) {
		value := strings.TrimSpace(sourcePath)
		if value == "" {
			return
		}
		if skipErr != nil {
			logger.Error("thread/unarchive: restore artifact skipped",
				logger.FieldThreadID, id,
				"source_path", value,
				"archived_path", strings.TrimSpace(archivedPath),
				"reason", reason,
				logger.FieldError, skipErr,
			)
		} else {
			logger.Error("thread/unarchive: restore artifact skipped",
				logger.FieldThreadID, id,
				"source_path", value,
				"archived_path", strings.TrimSpace(archivedPath),
				"reason", reason,
			)
		}
		if _, ok := skippedSet[value]; ok {
			return
		}
		skippedSet[value] = struct{}{}
		skipped = append(skipped, value)
	}

	for _, meta := range manifest.Files {
		srcPath := strings.TrimSpace(meta.SourcePath)
		if srcPath == "" {
			continue
		}
		withinCodex, err := pathWithinRoot(codexRoot, srcPath)
		if err != nil {
			appendSkipped(srcPath, "", "validate source path scope", err)
			continue
		}
		if !withinCodex {
			appendSkipped(srcPath, "", "source path is outside codex root", nil)
			continue
		}

		archivedPath := strings.TrimSpace(meta.ArchivedPath)
		if archivedPath == "" {
			appendSkipped(srcPath, "", "archived path is empty", nil)
			continue
		}
		if !filepath.IsAbs(archivedPath) && strings.TrimSpace(manifest.ArchiveDir) != "" {
			archivedPath = filepath.Join(strings.TrimSpace(manifest.ArchiveDir), archivedPath)
		}
		if strings.TrimSpace(manifest.ArchiveDir) != "" {
			withinArchive, err := pathWithinRoot(manifest.ArchiveDir, archivedPath)
			if err != nil {
				appendSkipped(srcPath, archivedPath, "validate archived path scope", err)
				continue
			}
			if !withinArchive {
				appendSkipped(srcPath, archivedPath, "archived path is outside archive root", nil)
				continue
			}
		}

		info, err := os.Stat(archivedPath)
		if err != nil {
			appendSkipped(srcPath, archivedPath, "stat archived file", err)
			continue
		}
		if info.IsDir() {
			appendSkipped(srcPath, archivedPath, "archived path is a directory", nil)
			continue
		}

		expectedSHA256 := strings.TrimSpace(meta.SHA256)
		if expectedSHA256 != "" {
			actualArchiveSHA256, err := fileSHA256(archivedPath)
			if err != nil {
				appendSkipped(srcPath, archivedPath, "compute archived checksum", err)
				continue
			}
			if !strings.EqualFold(expectedSHA256, actualArchiveSHA256) {
				appendSkipped(srcPath, archivedPath, "archived checksum mismatch", nil)
				continue
			}
		}

		if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
			appendSkipped(srcPath, archivedPath, "ensure source parent dir", err)
			continue
		}
		if err := copyFileOverwrite(archivedPath, srcPath); err != nil {
			appendSkipped(srcPath, archivedPath, "restore file to source path", err)
			continue
		}
		if expectedSHA256 != "" {
			actualSourceSHA256, err := fileSHA256(srcPath)
			if err != nil {
				_ = os.Remove(srcPath)
				appendSkipped(srcPath, archivedPath, "compute restored source checksum", err)
				continue
			}
			if !strings.EqualFold(expectedSHA256, actualSourceSHA256) {
				_ = os.Remove(srcPath)
				appendSkipped(srcPath, archivedPath, "restored source checksum mismatch", nil)
				continue
			}
		}
		if _, ok := restoredSet[srcPath]; !ok {
			restoredSet[srcPath] = struct{}{}
			restored = append(restored, srcPath)
		}
	}
	sort.Strings(restored)
	sort.Strings(skipped)
	return restored, skipped, nil
}

type threadArchiveRestoreNotice struct {
	Modified      bool
	ManifestPath  string
	ModifiedFiles []string
}

type manifestPathCandidate struct {
	Path       string
	ModifiedAt time.Time
}

func inspectThreadArchiveForRestore(threadID string) (threadArchiveRestoreNotice, error) {
	notice := threadArchiveRestoreNotice{
		Modified:      false,
		ManifestPath:  "",
		ModifiedFiles: []string{},
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return notice, nil
	}
	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		return notice, err
	}
	safeThreadID, err := sanitizeArchiveNameStrict(id)
	if err != nil {
		return notice, apperrors.Wrap(err, "inspectThreadArchiveForRestore", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	manifestPath, err := findLatestThreadArchiveManifestPath(threadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return notice, nil
		}
		return notice, apperrors.Wrap(err, "inspectThreadArchiveForRestore", "find latest manifest")
	}
	manifest, err := readThreadArchiveManifest(manifestPath)
	if err != nil {
		return notice, apperrors.Wrap(err, "inspectThreadArchiveForRestore", "read manifest")
	}
	notice.ManifestPath = manifestPath

	modified := make([]string, 0, len(manifest.Files))
	for _, meta := range manifest.Files {
		archivedPath := strings.TrimSpace(meta.ArchivedPath)
		if archivedPath == "" {
			continue
		}
		if !filepath.IsAbs(archivedPath) && strings.TrimSpace(manifest.ArchiveDir) != "" {
			archivedPath = filepath.Join(strings.TrimSpace(manifest.ArchiveDir), archivedPath)
		}
		if strings.TrimSpace(manifest.ArchiveDir) != "" {
			withinRoot, err := pathWithinRoot(manifest.ArchiveDir, archivedPath)
			if err != nil || !withinRoot {
				modified = append(modified, archivedPath)
				continue
			}
		}
		info, err := os.Stat(archivedPath)
		if err != nil || info.IsDir() {
			modified = append(modified, archivedPath)
			continue
		}
		if meta.SizeBytes > 0 && info.Size() != meta.SizeBytes {
			modified = append(modified, archivedPath)
			continue
		}
		if checksum := strings.TrimSpace(meta.SHA256); checksum != "" {
			actualSHA256, err := fileSHA256(archivedPath)
			if err != nil || !strings.EqualFold(checksum, actualSHA256) {
				modified = append(modified, archivedPath)
				continue
			}
		}
	}
	if len(modified) > 0 {
		sort.Strings(modified)
		notice.Modified = true
		notice.ModifiedFiles = modified
	}
	return notice, nil
}

func findLatestThreadArchiveManifestPath(threadDir string) (string, error) {
	info, err := os.Stat(threadDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", apperrors.Newf("findLatestThreadArchiveManifestPath", "thread archive path is not a directory: %s", threadDir)
	}

	candidates := make([]manifestPathCandidate, 0, 8)
	appendCandidate := func(path string) error {
		fileInfo, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if fileInfo.IsDir() {
			return nil
		}
		candidates = append(candidates, manifestPathCandidate{
			Path:       path,
			ModifiedAt: fileInfo.ModTime(),
		})
		return nil
	}

	if err := appendCandidate(filepath.Join(threadDir, "manifest.json")); err != nil {
		return "", apperrors.Wrap(err, "findLatestThreadArchiveManifestPath", "stat legacy manifest")
	}
	entries, err := os.ReadDir(threadDir)
	if err != nil {
		return "", apperrors.Wrap(err, "findLatestThreadArchiveManifestPath", "read thread archive dir")
	}
	for _, entry := range entries {
		if entry == nil || !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(threadDir, entry.Name(), "manifest.json")
		if err := appendCandidate(manifestPath); err != nil {
			return "", apperrors.Wrapf(err, "findLatestThreadArchiveManifestPath", "stat manifest for snapshot %s", entry.Name())
		}
	}
	if len(candidates) == 0 {
		return "", os.ErrNotExist
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].ModifiedAt.Equal(candidates[j].ModifiedAt) {
			return candidates[i].ModifiedAt.After(candidates[j].ModifiedAt)
		}
		return candidates[i].Path > candidates[j].Path
	})
	return candidates[0].Path, nil
}

func readThreadArchiveManifest(manifestPath string) (threadArchiveManifest, error) {
	manifest := threadArchiveManifest{}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func pathWithinRoot(root string, path string) (bool, error) {
	rootAbs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return false, err
	}
	pathAbs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false, err
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true, nil
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..", nil
}

func writeThreadArchiveManifest(manifest threadArchiveManifest) error {
	if strings.TrimSpace(manifest.ArchiveDir) == "" {
		return apperrors.New("writeThreadArchiveManifest", "archive dir is empty")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(manifest.ArchiveDir, "manifest.json")
	return os.WriteFile(manifestPath, data, 0o644)
}

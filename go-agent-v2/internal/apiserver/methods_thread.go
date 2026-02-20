// methods_thread.go — thread/* JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

	if err := s.mgr.Launch(ctx, id, id, "", p.Cwd, dynamicTools); err != nil {
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
		candidates := buildResumeCandidates(p.ThreadID, s.resolveHistoricalCodexThreadIDs(ctx, p.ThreadID))
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
		codexThreadID = strings.TrimSpace(s.resolveHistoricalCodexThreadID(ctx, id))
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

	if proc := s.mgr.Get(id); proc != nil && proc.Client != nil {
		candidate := normalizeCodexThreadID(proc.Client.GetThreadID())
		if candidate != "" {
			return candidate, ""
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

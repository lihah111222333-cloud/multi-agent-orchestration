// methods.go — JSON-RPC 方法注册与实现。
//
// 完整对标 codex app-server-protocol v2 API + SOCKS 独有斜杠命令。
// 参考: APP-SERVER-PROTOCOL.md
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

var codexThreadIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

const (
	defaultLSPUsagePromptHint = "已注入LSP工具。凡是源代码的分析、定位、修改与解释，必须调用lsp_open_file、lsp_hover、lsp_diagnostics；未使用上述工具前，不得基于猜测给出结论"
	prefKeyLSPUsagePromptHint = "settings.lspUsagePromptHint"
	maxLSPUsagePromptHintLen  = 4000
)

// registerMethods 注册所有 JSON-RPC 方法 (完整对标 APP-SERVER-PROTOCOL.md)。
func (s *Server) registerMethods() {
	noop := noopHandler()

	// § 1. 初始化
	s.methods["initialize"] = s.initialize
	s.methods["initialized"] = noop

	// § 2. 线程生命周期 (12 methods)
	s.methods["thread/start"] = typedHandler(s.threadStartTyped)
	s.methods["thread/resume"] = typedHandler(s.threadResumeTyped)
	s.methods["thread/fork"] = typedHandler(s.threadForkTyped)
	s.methods["thread/archive"] = noop
	s.methods["thread/unarchive"] = noop
	s.methods["thread/name/set"] = typedHandler(s.threadNameSetTyped)
	s.methods["thread/compact/start"] = s.threadCompact
	s.methods["thread/rollback"] = typedHandler(s.threadRollbackTyped)
	s.methods["thread/list"] = s.threadList
	s.methods["thread/loaded/list"] = s.threadLoadedList
	s.methods["thread/read"] = typedHandler(s.threadReadTyped)
	s.methods["thread/resolve"] = typedHandler(s.threadResolveTyped)
	s.methods["thread/messages"] = typedHandler(s.threadMessagesTyped)
	s.methods["thread/backgroundTerminals/clean"] = s.threadBgTerminalsClean

	// § 3. 对话控制 (4 methods)
	s.methods["turn/start"] = typedHandler(s.turnStartTyped)
	s.methods["turn/steer"] = typedHandler(s.turnSteerTyped)
	s.methods["turn/interrupt"] = s.turnInterrupt
	s.methods["turn/forceComplete"] = s.turnForceComplete
	s.methods["review/start"] = typedHandler(s.reviewStartTyped)

	// § 4. 文件搜索 (4 methods)
	s.methods["fuzzyFileSearch"] = typedHandler(s.fuzzyFileSearchTyped)
	s.methods["fuzzyFileSearch/sessionStart"] = noop
	s.methods["fuzzyFileSearch/sessionUpdate"] = noop
	s.methods["fuzzyFileSearch/sessionStop"] = noop

	// § 5. Skills / Apps (5 methods)
	s.methods["skills/list"] = s.skillsList
	s.methods["skills/local/read"] = typedHandler(s.skillsLocalReadTyped)
	s.methods["skills/local/importDir"] = typedHandler(s.skillsLocalImportDirTyped)
	s.methods["skills/remote/read"] = typedHandler(s.skillsRemoteReadTyped)
	s.methods["skills/remote/write"] = typedHandler(s.skillsRemoteWriteTyped)
	s.methods["skills/config/read"] = typedHandler(s.skillsConfigReadTyped)
	s.methods["skills/config/write"] = typedHandler(s.skillsConfigWriteTyped)
	s.methods["skills/match/preview"] = typedHandler(s.skillsMatchPreviewTyped)
	s.methods["app/list"] = s.appList

	// § 6. 模型 / 配置 (7 methods)
	s.methods["model/list"] = s.modelList
	s.methods["collaborationMode/list"] = s.collaborationModeList
	s.methods["experimentalFeature/list"] = s.experimentalFeatureList
	s.methods["config/read"] = s.configRead
	s.methods["config/value/write"] = typedHandler(s.configValueWriteTyped)
	s.methods["config/batchWrite"] = typedHandler(s.configBatchWriteTyped)
	s.methods["config/lspPromptHint/read"] = s.configLSPPromptHintRead
	s.methods["config/lspPromptHint/write"] = typedHandler(s.configLSPPromptHintWriteTyped)
	s.methods["configRequirements/read"] = s.configRequirementsRead

	// § 7. 账号 (5 methods)
	s.methods["account/login/start"] = typedHandler(s.accountLoginStartTyped)
	s.methods["account/login/cancel"] = s.accountLoginCancel
	s.methods["account/logout"] = s.accountLogout
	s.methods["account/read"] = s.accountRead
	s.methods["account/rateLimits/read"] = s.accountRateLimitsRead

	// § 8. MCP (3 methods)
	s.methods["mcpServer/oauth/login"] = noop
	s.methods["config/mcpServer/reload"] = s.mcpServerReload
	s.methods["mcpServerStatus/list"] = s.mcpServerStatusList
	s.methods["lsp_diagnostics_query"] = typedHandler(s.lspDiagnosticsQueryTyped)

	// § 9. 命令执行 / 其他 (2 methods)
	s.methods["command/exec"] = typedHandler(s.commandExecTyped)
	s.methods["feedback/upload"] = noop

	// § 10. 斜杠命令 (SOCKS 独有, JSON-RPC 化)
	s.methods["thread/undo"] = s.threadUndo
	s.methods["thread/model/set"] = s.threadModelSet
	s.methods["thread/personality/set"] = s.threadPersonality
	s.methods["thread/approvals/set"] = s.threadApprovals
	s.methods["thread/mcp/list"] = s.threadMCPList
	s.methods["thread/skills/list"] = s.threadSkillsList
	s.methods["thread/debugMemory"] = s.threadDebugMemory

	// § 11. 系统日志查询 (2 methods)
	s.methods["log/list"] = typedHandler(s.logListTyped)
	s.methods["log/filters"] = s.logFilters

	// § 12. Dashboard 数据查询 (12 methods, 替代 Wails Dashboard 绑定)
	s.registerDashboardMethods()

	// § 13. Workspace Run (双通道编排: 虚拟目录 + PG 状态)
	s.methods["workspace/run/create"] = s.workspaceRunCreate
	s.methods["workspace/run/get"] = s.workspaceRunGet
	s.methods["workspace/run/list"] = s.workspaceRunList
	s.methods["workspace/run/merge"] = s.workspaceRunMerge
	s.methods["workspace/run/abort"] = s.workspaceRunAbort

	// § 14. UI State (UI 偏好持久化)
	s.methods["ui/preferences/get"] = typedHandler(s.uiPreferencesGet)
	s.methods["ui/preferences/set"] = typedHandler(s.uiPreferencesSet)
	s.methods["ui/preferences/getAll"] = s.uiPreferencesGetAll
	s.methods["ui/projects/get"] = s.uiProjectsGet
	s.methods["ui/projects/add"] = typedHandler(s.uiProjectsAdd)
	s.methods["ui/projects/remove"] = typedHandler(s.uiProjectsRemove)
	s.methods["ui/projects/setActive"] = typedHandler(s.uiProjectsSetActive)
	s.methods["ui/dashboard/get"] = typedHandler(s.uiDashboardGet)
	s.methods["ui/state/get"] = s.uiStateGet

	// § 15. Debug (运行时诊断)
	s.methods["debug/runtime"] = s.debugRuntime
	s.methods["debug/gc"] = s.debugForceGC
}

// ========================================
// 初始化
// ========================================

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	ClientInfo      any    `json:"clientInfo,omitempty"`
	Capabilities    any    `json:"capabilities,omitempty"`
}

func (s *Server) initialize(_ context.Context, params json.RawMessage) (any, error) {
	var p initializeParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			logger.Debug("initialize: unmarshal params", logger.FieldError, err)
		}
	}
	return map[string]any{
		"protocolVersion": "2.0",
		"serverInfo": map[string]string{
			"name":    "codex-go-app-server",
			"version": "0.1.0",
		},
		"capabilities": map[string]bool{
			"threads":    true,
			"turns":      true,
			"fileSearch": true,
			"skills":     true,
			"exec":       true,
		},
	}, nil
}

// ========================================
// thread/*
// ========================================

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

// ========================================
// turn/*
// ========================================

// UserInput 用户输入 (支持多种类型)。
type UserInput struct {
	Type    string `json:"type"`              // text, image, localImage, skill, mention, fileContent
	Text    string `json:"text,omitempty"`    // type=text
	URL     string `json:"url,omitempty"`     // type=image
	Path    string `json:"path,omitempty"`    // type=localImage/mention/fileContent
	Name    string `json:"name,omitempty"`    // type=skill/mention
	Content string `json:"content,omitempty"` // type=skill/fileContent
}

type turnStartParams struct {
	ThreadID             string          `json:"threadId"`
	Input                []UserInput     `json:"input"`
	SelectedSkills       []string        `json:"selectedSkills,omitempty"`
	ManualSkillSelection bool            `json:"manualSkillSelection,omitempty"`
	Cwd                  string          `json:"cwd,omitempty"`
	ApprovalPolicy       string          `json:"approvalPolicy,omitempty"`
	Model                string          `json:"model,omitempty"`
	OutputSchema         json.RawMessage `json:"outputSchema,omitempty"`
}

// turnInfo 通用 turn 信息。
type turnInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// turnStartResponse turn/start 响应。
type turnStartResponse struct {
	Turn turnInfo `json:"turn"`
}

type activeTurnIDReader interface {
	GetActiveTurnID() string
}

func resolveClientActiveTurnID(client codex.CodexClient) string {
	if client == nil {
		return ""
	}
	reader, ok := client.(activeTurnIDReader)
	if !ok {
		return ""
	}
	return strings.TrimSpace(reader.GetActiveTurnID())
}

func skillInputText(name, content string) string {
	return fmt.Sprintf("[skill:%s] %s", strings.TrimSpace(name), content)
}

func fileContentInputText(name, content string) string {
	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		return ""
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return trimmedContent
	}
	return fmt.Sprintf("[file:%s]\n%s", trimmedName, trimmedContent)
}

func collectInputSkillNames(inputs []UserInput) map[string]struct{} {
	if len(inputs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if !strings.EqualFold(strings.TrimSpace(input.Type), "skill") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(input.Name))
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

func collectSkillNameSet(raw []string) map[string]struct{} {
	if len(raw) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

func mergeSkillNameSets(dst map[string]struct{}, src map[string]struct{}) map[string]struct{} {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]struct{}, len(src))
	}
	for key := range src {
		dst[key] = struct{}{}
	}
	return dst
}

func mergePromptText(prompt, extra string) string {
	trimmedExtra := strings.TrimSpace(extra)
	if trimmedExtra == "" {
		return prompt
	}
	trimmedPrompt := strings.TrimSpace(prompt)
	if trimmedPrompt == "" {
		return extra
	}
	return prompt + "\n" + extra
}

func validateLSPUsagePromptHint(hint string) error {
	if len(hint) > maxLSPUsagePromptHintLen {
		return apperrors.Newf("Server.configLSPPromptHintWrite", "hint length exceeds %d", maxLSPUsagePromptHintLen)
	}
	return nil
}

func (s *Server) resolveLSPUsagePromptHint(ctx context.Context) string {
	if s.prefManager == nil {
		return defaultLSPUsagePromptHint
	}
	value, err := s.prefManager.Get(ctx, prefKeyLSPUsagePromptHint)
	if err != nil {
		logger.Warn("lsp hint: load preference failed", logger.FieldError, err)
		return defaultLSPUsagePromptHint
	}
	hint := strings.TrimSpace(asString(value))
	if hint == "" {
		return defaultLSPUsagePromptHint
	}
	if err := validateLSPUsagePromptHint(hint); err != nil {
		logger.Warn("lsp hint: invalid preference fallback to default", logger.FieldError, err)
		return defaultLSPUsagePromptHint
	}
	return hint
}

func (s *Server) appendLSPUsageHint(ctx context.Context, prompt string) string {
	return mergePromptText(prompt, s.resolveLSPUsagePromptHint(ctx))
}

func appendSkillPlaceholders(input []UserInput, skillNames []string) []UserInput {
	if len(skillNames) == 0 {
		return input
	}
	out := make([]UserInput, 0, len(input)+len(skillNames))
	out = append(out, input...)
	for _, name := range skillNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		out = append(out, UserInput{
			Type: "skill",
			Name: trimmed,
		})
	}
	return out
}

func (s *Server) buildConfiguredSkillPrompt(agentID string, input []UserInput) (string, int) {
	if s.skillSvc == nil {
		return "", 0
	}
	configured := s.GetAgentSkills(agentID)
	if len(configured) == 0 {
		return "", 0
	}

	inputSkillSet := collectInputSkillNames(input)
	texts := make([]string, 0, len(configured))
	for _, name := range configured {
		normalizedName := strings.TrimSpace(name)
		if normalizedName == "" {
			continue
		}
		if _, exists := inputSkillSet[strings.ToLower(normalizedName)]; exists {
			continue
		}
		content, err := s.skillSvc.ReadSkillContent(normalizedName)
		if err != nil {
			logger.Warn("turn/start: configured skill unavailable, skip",
				logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
				logger.FieldSkill, normalizedName,
				logger.FieldError, err,
			)
			continue
		}
		texts = append(texts, skillInputText(normalizedName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

func (s *Server) buildSelectedSkillPrompt(selectedSkills []string, input []UserInput) (string, int) {
	if s.skillSvc == nil || len(selectedSkills) == 0 {
		return "", 0
	}
	inputSkillSet := collectInputSkillNames(input)
	texts := make([]string, 0, len(selectedSkills))
	for _, rawName := range selectedSkills {
		skillName := strings.TrimSpace(rawName)
		if skillName == "" {
			continue
		}
		if _, exists := inputSkillSet[strings.ToLower(skillName)]; exists {
			continue
		}
		content, err := s.skillSvc.ReadSkillContent(skillName)
		if err != nil {
			logger.Warn("turn/start: selected skill unavailable, skip",
				logger.FieldSkill, skillName,
				logger.FieldError, err,
			)
			continue
		}
		texts = append(texts, skillInputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

func lowerMatchedTerms(text string, candidates []string) []string {
	if text == "" || len(candidates) == 0 {
		return nil
	}
	terms := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, raw := range candidates {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		lowerCandidate := strings.ToLower(candidate)
		if _, ok := seen[lowerCandidate]; ok {
			continue
		}
		if !strings.Contains(text, lowerCandidate) {
			continue
		}
		seen[lowerCandidate] = struct{}{}
		terms = append(terms, candidate)
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
}

type autoMatchedSkillMatch struct {
	Name         string
	MatchedBy    string
	MatchedTerms []string
}

type autoSkillMatchOptions struct {
	IncludeConfiguredExplicit bool
}

func explicitSkillMentionTerms(normalizedPrompt, skillName string) []string {
	trimmedName := strings.TrimSpace(skillName)
	if trimmedName == "" {
		return nil
	}
	lowerName := strings.ToLower(trimmedName)
	if lowerName == "" {
		return nil
	}
	terms := make([]string, 0, 2)
	if strings.Contains(normalizedPrompt, "@"+lowerName) {
		terms = append(terms, "@"+trimmedName)
	}
	if strings.Contains(normalizedPrompt, "[skill:"+lowerName+"]") {
		terms = append(terms, "[skill:"+trimmedName+"]")
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
}

func classifyAutoSkillMatch(normalizedPrompt, skillName string, forceWords, triggerWords []string) (string, []string) {
	explicitTerms := explicitSkillMentionTerms(normalizedPrompt, skillName)
	if len(explicitTerms) > 0 {
		return "explicit", explicitTerms
	}
	forceTerms := lowerMatchedTerms(normalizedPrompt, forceWords)
	if len(forceTerms) > 0 {
		return "force", forceTerms
	}
	triggerTerms := lowerMatchedTerms(normalizedPrompt, triggerWords)
	if len(triggerTerms) > 0 {
		return "trigger", triggerTerms
	}
	return "", nil
}

func (s *Server) collectAutoMatchedSkillMatches(agentID, prompt string, input []UserInput, options autoSkillMatchOptions) []autoMatchedSkillMatch {
	if s.skillSvc == nil {
		return nil
	}
	normalizedPrompt := strings.ToLower(strings.TrimSpace(prompt))
	if normalizedPrompt == "" {
		return nil
	}
	allSkills, err := s.skillSvc.ListSkills()
	if err != nil {
		logger.Warn("skills/auto-match: list skills failed",
			logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
			logger.FieldError, err,
		)
		return nil
	}
	if len(allSkills) == 0 {
		return nil
	}

	inputSkillSet := collectInputSkillNames(input)
	configuredSet := collectSkillNameSet(s.GetAgentSkills(agentID))

	matches := make([]autoMatchedSkillMatch, 0, len(allSkills))
	for _, skill := range allSkills {
		skillName := strings.TrimSpace(skill.Name)
		if skillName == "" {
			continue
		}
		skillNameLower := strings.ToLower(skillName)
		if _, exists := inputSkillSet[skillNameLower]; exists {
			continue
		}
		matchedBy, matchedTerms := classifyAutoSkillMatch(normalizedPrompt, skillName, skill.ForceWords, skill.TriggerWords)
		if matchedBy == "" {
			continue
		}
		if _, configured := configuredSet[skillNameLower]; configured {
			if !(options.IncludeConfiguredExplicit && matchedBy == "explicit") {
				continue
			}
		}
		matches = append(matches, autoMatchedSkillMatch{
			Name:         skillName,
			MatchedBy:    matchedBy,
			MatchedTerms: matchedTerms,
		})
	}
	return matches
}

func (s *Server) buildAutoMatchedSkillPrompt(agentID, prompt string, input []UserInput) (string, int) {
	matches := s.collectAutoMatchedSkillMatches(agentID, prompt, input, autoSkillMatchOptions{})
	if len(matches) == 0 {
		return "", 0
	}

	texts := make([]string, 0, len(matches))
	for _, match := range matches {
		skillName := strings.TrimSpace(match.Name)
		if skillName == "" {
			continue
		}
		content, readErr := s.skillSvc.ReadSkillContent(skillName)
		if readErr != nil {
			logger.Warn("turn/start: auto-matched skill unavailable, skip",
				logger.FieldAgentID, agentID, logger.FieldThreadID, agentID,
				logger.FieldSkill, skillName,
				logger.FieldError, readErr,
			)
			continue
		}
		texts = append(texts, skillInputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

func (s *Server) turnStartTyped(ctx context.Context, p turnStartParams) (any, error) {
	logger.Info("turn/start: request received",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		logger.FieldCwd, strings.TrimSpace(p.Cwd),
		"input_count", len(p.Input),
		"selected_skills_count", len(p.SelectedSkills),
	)
	proc, err := s.ensureThreadReadyForTurn(ctx, p.ThreadID, p.Cwd)
	if err != nil {
		return nil, err
	}
	logger.Info("turn/start: thread dispatch resolved",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		logger.FieldPort, proc.Client.GetPort(),
		"codex_thread_id", strings.TrimSpace(proc.Client.GetThreadID()),
	)

	selectedSkills, err := normalizeSkillNames(p.SelectedSkills)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.turnStart", "normalize selected skills")
	}

	prompt, images, files := extractInputs(p.Input)
	matchingInput := appendSkillPlaceholders(p.Input, selectedSkills)
	configuredSkillPrompt, configuredSkillCount := s.buildConfiguredSkillPrompt(p.ThreadID, matchingInput)
	selectedSkillPrompt, selectedSkillCount := s.buildSelectedSkillPrompt(selectedSkills, p.Input)
	autoMatchedSkillPrompt := ""
	autoMatchedSkillCount := 0
	if !p.ManualSkillSelection {
		autoMatchedSkillPrompt, autoMatchedSkillCount = s.buildAutoMatchedSkillPrompt(p.ThreadID, prompt, matchingInput)
	}
	submitPrompt := mergePromptText(prompt, configuredSkillPrompt)
	submitPrompt = mergePromptText(submitPrompt, selectedSkillPrompt)
	submitPrompt = mergePromptText(submitPrompt, autoMatchedSkillPrompt)
	submitPrompt = s.appendLSPUsageHint(ctx, submitPrompt)
	logger.Info("turn/start: input prepared",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		"text_len", len(prompt),
		"images", len(images),
		"files", len(files),
		"configured_skills", configuredSkillCount,
		"selected_skills", selectedSkillCount,
		"manual_skill_selection", p.ManualSkillSelection,
		"auto_matched_skills", autoMatchedSkillCount,
	)
	if err := proc.Client.Submit(submitPrompt, images, files, p.OutputSchema); err != nil {
		return nil, apperrors.Wrap(err, "Server.turnStart", "submit prompt")
	}
	if s.uiRuntime != nil {
		attachments := buildUserTimelineAttachmentsFromInputs(p.Input)
		if len(attachments) == 0 {
			attachments = buildUserTimelineAttachments(images, files)
		}
		s.uiRuntime.AppendUserMessage(p.ThreadID, prompt, attachments)
	}

	resolvedTurnID := resolveClientActiveTurnID(proc.Client)
	if resolvedTurnID == "" {
		logger.Warn("turn/start: active turn id unavailable after submit; tracker will use synthetic id",
			logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		)
	}
	turnID := s.beginTrackedTurn(p.ThreadID, resolvedTurnID)
	return turnStartResponse{
		Turn: turnInfo{ID: turnID, Status: "inProgress"},
	}, nil
}

type turnSteerParams struct {
	ThreadID             string      `json:"threadId"`
	Input                []UserInput `json:"input"`
	SelectedSkills       []string    `json:"selectedSkills,omitempty"`
	ManualSkillSelection bool        `json:"manualSkillSelection,omitempty"`
}

func (s *Server) turnSteerTyped(ctx context.Context, p turnSteerParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		selectedSkills, err := normalizeSkillNames(p.SelectedSkills)
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.turnSteer", "normalize selected skills")
		}
		prompt, images, files := extractInputs(p.Input)
		matchingInput := appendSkillPlaceholders(p.Input, selectedSkills)
		configuredSkillPrompt, _ := s.buildConfiguredSkillPrompt(p.ThreadID, matchingInput)
		selectedSkillPrompt, _ := s.buildSelectedSkillPrompt(selectedSkills, p.Input)
		autoMatchedSkillPrompt := ""
		if !p.ManualSkillSelection {
			autoMatchedSkillPrompt, _ = s.buildAutoMatchedSkillPrompt(p.ThreadID, prompt, matchingInput)
		}
		submitPrompt := mergePromptText(prompt, configuredSkillPrompt)
		submitPrompt = mergePromptText(submitPrompt, selectedSkillPrompt)
		submitPrompt = mergePromptText(submitPrompt, autoMatchedSkillPrompt)
		submitPrompt = s.appendLSPUsageHint(ctx, submitPrompt)
		if err := proc.Client.Submit(submitPrompt, images, files, nil); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

func (s *Server) turnInterrupt(_ context.Context, params json.RawMessage) (any, error) {
	start := time.Now()
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.turnInterrupt", "unmarshal params")
	}
	beforeState := s.readThreadRuntimeState(p.ThreadID)
	activeTrackedBefore := s.hasActiveTrackedTurn(p.ThreadID)
	activeBefore := isInterruptActiveState(beforeState)
	logger.Info("turn/interrupt: request",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
		logger.FieldParamsLen, len(params),
		"state_before", beforeState,
		"active_before", activeBefore,
		"active_tracked_before", activeTrackedBefore,
	)
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand("/interrupt", ""); err != nil {
			if isInterruptNoActiveTurnError(err) {
				if activeBefore || activeTrackedBefore {
					if completion, ok := s.completeTrackedTurn(p.ThreadID, "completed", "interrupt_no_active_turn"); ok {
						s.Notify("turn/completed", completion)
					} else {
						s.Notify("turn/completed", map[string]any{
							"threadId": p.ThreadID,
							"status":   "completed",
							"reason":   "interrupt_no_active_turn",
						})
					}
				}
				logger.Info("turn/interrupt: no active turn",
					logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
					"state_before", beforeState,
					logger.FieldDurationMS, time.Since(start).Milliseconds(),
				)
				return map[string]any{
					"confirmed":     false,
					"mode":          "no_active_turn",
					"interruptSent": false,
					"stateBefore":   beforeState,
					"stateAfter":    beforeState,
				}, nil
			}
			logger.Warn("turn/interrupt: send command failed",
				logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
				logger.FieldError, err,
				logger.FieldDurationMS, time.Since(start).Milliseconds(),
			)
			return nil, err
		}
		logger.Info("turn/interrupt: command sent",
			logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		s.markTrackedTurnInterruptRequested(p.ThreadID)
		confirmed, afterState, waitedMS, observedActive := s.waitInterruptOutcome(
			p.ThreadID,
			6*time.Second,
			activeBefore || activeTrackedBefore,
		)
		mode := interruptSettleMode(confirmed, afterState)
		if !observedActive {
			confirmed = false
			mode = "no_active_turn"
		}
		logger.Info("turn/interrupt: settle",
			logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
			"confirmed", confirmed,
			"mode", mode,
			"active_observed", observedActive,
			"state_before", beforeState,
			"state_after", afterState,
			"waited_ms", waitedMS,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return map[string]any{
			"confirmed":      confirmed,
			"mode":           mode,
			"interruptSent":  true,
			"stateBefore":    beforeState,
			"stateAfter":     afterState,
			"waitedMs":       waitedMS,
			"activeObserved": observedActive,
		}, nil
	})
}

// turnForceComplete 强制完成当前 turn (中断 + 清理跟踪状态)。
func (s *Server) turnForceComplete(_ context.Context, params json.RawMessage) (any, error) {
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.turnForceComplete", "unmarshal params")
	}
	logger.Info("turn/forceComplete: request",
		logger.FieldAgentID, p.ThreadID, logger.FieldThreadID, p.ThreadID,
	)
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		// 尝试发送中断; 忽略 "no active turn" 错误, 但记录其他错误。
		if err := proc.Client.SendCommand("/interrupt", ""); err != nil {
			if isInterruptNoActiveTurnError(err) {
				logger.Info("turn/forceComplete: no active turn (best-effort)",
					logger.FieldAgentID, p.ThreadID)
			} else {
				logger.Warn("turn/forceComplete: interrupt failed (best-effort)",
					logger.FieldAgentID, p.ThreadID, logger.FieldError, err)
			}
		}

		// 无论中断是否成功, 都强制清理 tracked turn 状态。
		if completion, ok := s.completeTrackedTurn(p.ThreadID, "completed", "force_complete"); ok {
			s.Notify("turn/completed", completion)
		} else {
			s.Notify("turn/completed", map[string]any{
				"threadId": p.ThreadID,
				"status":   "completed",
				"reason":   "force_complete",
			})
		}

		return map[string]any{
			"confirmed":      true,
			"forceCompleted": true,
		}, nil
	})
}

func normalizeInterruptState(raw string) string {
	state := strings.ToLower(strings.TrimSpace(raw))
	if state == "" {
		return "idle"
	}
	switch state {
	case "completed", "complete", "done", "success", "succeeded", "ready", "stopped", "ended", "closed":
		return "idle"
	case "failed", "fail":
		return "error"
	default:
		return state
	}
}

func isInterruptActiveState(state string) bool {
	switch normalizeInterruptState(state) {
	case "starting", "thinking", "responding", "running", "editing", "waiting", "syncing":
		return true
	default:
		return false
	}
}

func isInterruptNoActiveTurnError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no active turn") ||
		strings.Contains(message, "nothing to interrupt") ||
		strings.Contains(message, "not interruptible")
}

func (s *Server) readThreadRuntimeState(threadID string) string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "idle"
	}
	if s.uiRuntime == nil {
		if s.hasActiveTrackedTurn(id) {
			return "running"
		}
		return ""
	}
	snapshot := s.uiRuntime.Snapshot()
	state := normalizeInterruptState(snapshot.Statuses[id])
	if state == "idle" && s.hasActiveTrackedTurn(id) {
		return "running"
	}
	return state
}

func (s *Server) waitInterruptSettled(threadID string, timeout time.Duration) (bool, string, int64) {
	confirmed, afterState, waitedMS, _ := s.waitInterruptOutcome(threadID, timeout, true)
	return confirmed, afterState, waitedMS
}

func (s *Server) waitInterruptOutcome(threadID string, timeout time.Duration, activeHint bool) (bool, string, int64, bool) {
	start := time.Now()
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false, "idle", 0, false
	}
	observedActive := activeHint
	if terminalStatus, ok := s.waitTrackedTurnTerminal(id, timeout); ok {
		afterState := normalizeInterruptState(terminalStatus)
		confirmed := strings.EqualFold(terminalStatus, "interrupted")
		return confirmed, afterState, time.Since(start).Milliseconds(), true
	}
	if s.uiRuntime == nil {
		return false, "", time.Since(start).Milliseconds(), observedActive
	}
	deadline := start.Add(timeout)
	lastState := s.readThreadRuntimeState(id)
	if isInterruptActiveState(lastState) {
		observedActive = true
	}
	for {
		if !isInterruptActiveState(lastState) {
			if !observedActive {
				return false, lastState, time.Since(start).Milliseconds(), false
			}
			return true, lastState, time.Since(start).Milliseconds(), true
		}
		observedActive = true
		if time.Now().After(deadline) {
			return false, lastState, time.Since(start).Milliseconds(), true
		}
		time.Sleep(120 * time.Millisecond)
		lastState = s.readThreadRuntimeState(id)
	}
}

func interruptSettleMode(confirmed bool, afterState string) string {
	if confirmed {
		return "interrupt_confirmed"
	}
	switch normalizeInterruptState(afterState) {
	case "error":
		return "interrupt_terminal_failed"
	case "idle":
		return "interrupt_terminal_completed"
	default:
		return "interrupt_timeout"
	}
}

// reviewStartParams review/start 请求参数。
type reviewStartParams struct {
	ThreadID string `json:"threadId"`
	Delivery string `json:"delivery,omitempty"`
}

func (s *Server) reviewStartTyped(_ context.Context, p reviewStartParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand("/review", p.Delivery); err != nil {
			return nil, apperrors.Wrap(err, "Server.reviewStart", "send review command")
		}
		return map[string]any{}, nil
	})
}

// ========================================
// fuzzyFileSearch
// ========================================

type fuzzySearchParams struct {
	Query string   `json:"query"`
	Roots []string `json:"roots"`
}

func (s *Server) fuzzyFileSearchTyped(_ context.Context, p fuzzySearchParams) (any, error) {
	query := strings.ToLower(p.Query)
	results := make([]map[string]any, 0)

	for _, root := range p.Roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				base := filepath.Base(path)
				if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if fuzzyMatch(strings.ToLower(rel), query) {
				results = append(results, map[string]any{
					"root":     root,
					"path":     rel,
					"fileName": info.Name(),
				})
				if len(results) >= 100 {
					return filepath.SkipAll
				}
			}
			return nil
		})
	}

	return map[string]any{"files": results}, nil
}

// fuzzyMatch 子序列模糊匹配。
func fuzzyMatch(text, pattern string) bool {
	pi := 0
	for i := 0; i < len(text) && pi < len(pattern); i++ {
		if text[i] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

func normalizeSkillName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", apperrors.New("normalizeSkillName", "skill name is required")
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", apperrors.Newf("normalizeSkillName", "invalid skill name %q", raw)
	}
	return name, nil
}

func normalizeSkillNames(rawNames []string) ([]string, error) {
	if len(rawNames) == 0 {
		return []string{}, nil
	}
	names := make([]string, 0, len(rawNames))
	seen := make(map[string]struct{}, len(rawNames))
	for _, raw := range rawNames {
		name, err := normalizeSkillName(raw)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}

// ========================================
// skills, apps, model, config, mcp
// ========================================

func (s *Server) skillsList(_ context.Context, _ json.RawMessage) (any, error) {
	var skills []map[string]string
	skillsDir := s.skillsDirectory()
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return map[string]any{"skills": skills}, nil
	}
	for _, e := range entries {
		if e.IsDir() {
			skills = append(skills, map[string]string{"name": e.Name()})
		}
	}
	return map[string]any{"skills": skills}, nil
}

func (s *Server) appList(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"apps": []any{}}, nil
}

type skillsLocalReadParams struct {
	Path string `json:"path"`
}

const (
	maxSkillImportFiles          = 1000
	maxSkillImportSingleFileSize = 4 << 20  // 4MB
	maxSkillImportTotalFileSize  = 20 << 20 // 20MB
)

type skillsLocalImportDirParams struct {
	Path  string   `json:"path"`
	Paths []string `json:"paths,omitempty"`
	Name  string   `json:"name,omitempty"`
}

type skillImportStats struct {
	Files int
	Bytes int64
}

type skillImportFailure struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

type skillImportResult struct {
	Name      string `json:"name"`
	Dir       string `json:"dir"`
	SkillFile string `json:"skill_file"`
	Source    string `json:"source"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
}

func skillImportDirName(rawName, sourceDir string) (string, error) {
	name := strings.TrimSpace(rawName)
	if name != "" {
		return normalizeSkillName(name)
	}
	candidate := strings.TrimSpace(strings.TrimRight(sourceDir, `/\`))
	if candidate == "" {
		return "", apperrors.New("skillImportDirName", "source directory is required")
	}
	base := filepath.Base(candidate)
	return normalizeSkillName(base)
}

func ensureSourceSkillFile(sourceDir string) (string, error) {
	path := filepath.Join(sourceDir, "SKILL.md")
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", apperrors.Newf("ensureSourceSkillFile", "SKILL.md is a directory: %s", path)
	}
	return path, nil
}

func copyRegularFile(srcPath, dstPath string, mode fs.FileMode) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	if mode == 0 {
		mode = 0o644
	}
	dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func copySkillDirectory(sourceDir, targetDir string) (skillImportStats, error) {
	stats := skillImportStats{}
	err := filepath.WalkDir(sourceDir, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceDir, currentPath)
		if err != nil {
			return err
		}
		relative = filepath.Clean(relative)
		if relative == "." {
			return os.MkdirAll(targetDir, 0o755)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return apperrors.Newf("copySkillDirectory", "path escapes source dir: %s", currentPath)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return apperrors.Newf("copySkillDirectory", "symlink is not allowed: %s", relative)
		}
		if entry.IsDir() && strings.EqualFold(entry.Name(), ".git") {
			return filepath.SkipDir
		}
		destinationPath := filepath.Join(targetDir, relative)
		if entry.IsDir() {
			return os.MkdirAll(destinationPath, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > maxSkillImportSingleFileSize {
			return apperrors.Newf(
				"copySkillDirectory",
				"file too large: %s (%d bytes, limit %d bytes)",
				relative,
				info.Size(),
				maxSkillImportSingleFileSize,
			)
		}
		stats.Files++
		if stats.Files > maxSkillImportFiles {
			return apperrors.Newf("copySkillDirectory", "too many files: limit %d", maxSkillImportFiles)
		}
		stats.Bytes += info.Size()
		if stats.Bytes > maxSkillImportTotalFileSize {
			return apperrors.Newf(
				"copySkillDirectory",
				"skill package too large: %d bytes (limit %d bytes)",
				stats.Bytes,
				maxSkillImportTotalFileSize,
			)
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return err
		}
		return copyRegularFile(currentPath, destinationPath, info.Mode().Perm())
	})
	return stats, err
}

func collectSkillImportSources(path string, paths []string) []string {
	candidates := make([]string, 0, len(paths)+1)
	if strings.TrimSpace(path) != "" {
		candidates = append(candidates, path)
	}
	candidates = append(candidates, paths...)

	out := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, raw := range candidates {
		source := strings.TrimSpace(raw)
		if source == "" {
			continue
		}
		abs, err := filepath.Abs(source)
		if err == nil {
			source = abs
		}
		key := strings.ToLower(filepath.Clean(source))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, source)
	}
	return out
}

func (s *Server) importSingleSkillDirectory(sourceDir, name string) (skillImportResult, error) {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "stat source dir")
	}
	if !info.IsDir() {
		return skillImportResult{}, apperrors.Newf("Server.importSingleSkillDirectory", "path is not a directory: %s", sourceDir)
	}
	if _, err := ensureSourceSkillFile(sourceDir); err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "missing SKILL.md in source directory")
	}

	skillName, err := skillImportDirName(name, sourceDir)
	if err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "resolve skill name")
	}
	skillsRoot := s.skillsDirectory()
	targetRoot := filepath.Join(skillsRoot, skillName)
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "mkdir skills root")
	}

	tmpRoot := filepath.Join(skillsRoot, fmt.Sprintf(".%s.import-%d", skillName, time.Now().UnixNano()))
	if err := os.RemoveAll(tmpRoot); err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "clean temp skill dir")
	}
	defer func() {
		_ = os.RemoveAll(tmpRoot)
	}()

	stats, err := copySkillDirectory(sourceDir, tmpRoot)
	if err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "copy skill directory")
	}
	skillFilePath := filepath.Join(tmpRoot, "SKILL.md")
	if _, err := os.Stat(skillFilePath); err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "copied package missing SKILL.md")
	}

	backupRoot := filepath.Join(skillsRoot, fmt.Sprintf(".%s.backup-%d", skillName, time.Now().UnixNano()))
	backupCreated := false
	if _, err := os.Stat(targetRoot); err == nil {
		if err := os.Rename(targetRoot, backupRoot); err != nil {
			return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "backup existing skill dir")
		}
		backupCreated = true
	} else if !os.IsNotExist(err) {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "stat existing skill dir")
	}
	if err := os.Rename(tmpRoot, targetRoot); err != nil {
		if backupCreated {
			_ = os.Rename(backupRoot, targetRoot)
		}
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "activate imported skill dir")
	}
	if backupCreated {
		_ = os.RemoveAll(backupRoot)
	}
	skillFilePath = filepath.Join(targetRoot, "SKILL.md")

	logger.Info("skills/local/importDir: imported",
		logger.FieldSkill, skillName,
		logger.FieldPath, sourceDir,
		"files", stats.Files,
		"bytes", stats.Bytes,
	)
	return skillImportResult{
		Name:      skillName,
		Dir:       targetRoot,
		SkillFile: skillFilePath,
		Source:    sourceDir,
		Files:     stats.Files,
		Bytes:     stats.Bytes,
	}, nil
}

func (s *Server) skillsLocalReadTyped(_ context.Context, p skillsLocalReadParams) (any, error) {
	path := strings.TrimSpace(p.Path)
	if path == "" {
		return nil, apperrors.New("Server.skillsLocalRead", "path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsLocalRead", "stat file")
	}
	if info.IsDir() {
		return nil, apperrors.Newf("Server.skillsLocalRead", "path is directory: %s", path)
	}
	const maxSkillLocalReadBytes = 1 << 20 // 1MB
	if info.Size() > maxSkillLocalReadBytes {
		return nil, apperrors.Newf("Server.skillsLocalRead", "file too large: %d bytes", info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsLocalRead", "read file")
	}
	return map[string]any{
		"skill": map[string]string{
			"path":    path,
			"content": string(data),
		},
	}, nil
}

func (s *Server) skillsLocalImportDirTyped(_ context.Context, p skillsLocalImportDirParams) (any, error) {
	sources := collectSkillImportSources(p.Path, p.Paths)
	if len(sources) == 0 {
		return nil, apperrors.New("Server.skillsLocalImportDir", "path or paths is required")
	}

	if len(sources) == 1 {
		result, err := s.importSingleSkillDirectory(sources[0], p.Name)
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.skillsLocalImportDir", "import directory")
		}
		skillPayload := map[string]any{
			"name":       result.Name,
			"dir":        result.Dir,
			"skill_file": result.SkillFile,
			"source":     result.Source,
			"files":      result.Files,
			"bytes":      result.Bytes,
		}
		return map[string]any{
			"ok": true,
			"summary": map[string]int{
				"requested": 1,
				"imported":  1,
				"failed":    0,
			},
			"skill":    skillPayload,
			"skills":   []map[string]any{skillPayload},
			"failures": []map[string]string{},
		}, nil
	}

	if strings.TrimSpace(p.Name) != "" {
		return nil, apperrors.New("Server.skillsLocalImportDir", "name is only supported for single directory import")
	}

	results := make([]skillImportResult, 0, len(sources))
	failures := make([]skillImportFailure, 0)
	seenNames := make(map[string]string, len(sources))

	for _, source := range sources {
		skillName, nameErr := skillImportDirName("", source)
		if nameErr != nil {
			failures = append(failures, skillImportFailure{
				Source: source,
				Error:  nameErr.Error(),
			})
			continue
		}
		nameKey := strings.ToLower(skillName)
		if previousSource, exists := seenNames[nameKey]; exists {
			failures = append(failures, skillImportFailure{
				Source: source,
				Error:  fmt.Sprintf("duplicate skill name %q with source %s", skillName, previousSource),
			})
			continue
		}
		seenNames[nameKey] = source

		result, err := s.importSingleSkillDirectory(source, "")
		if err != nil {
			failures = append(failures, skillImportFailure{
				Source: source,
				Error:  err.Error(),
			})
			continue
		}
		results = append(results, result)
	}

	skillsPayload := make([]map[string]any, 0, len(results))
	for _, result := range results {
		skillsPayload = append(skillsPayload, map[string]any{
			"name":       result.Name,
			"dir":        result.Dir,
			"skill_file": result.SkillFile,
			"source":     result.Source,
			"files":      result.Files,
			"bytes":      result.Bytes,
		})
	}
	failuresPayload := make([]map[string]string, 0, len(failures))
	for _, failure := range failures {
		failuresPayload = append(failuresPayload, map[string]string{
			"source": failure.Source,
			"error":  failure.Error,
		})
	}

	return map[string]any{
		"ok": len(failures) == 0,
		"summary": map[string]int{
			"requested": len(sources),
			"imported":  len(results),
			"failed":    len(failures),
		},
		"skills":   skillsPayload,
		"failures": failuresPayload,
	}, nil
}

func (s *Server) modelList(_ context.Context, _ json.RawMessage) (any, error) {
	models := []map[string]string{
		{"id": "o4-mini", "name": "O4 Mini"},
		{"id": "o3", "name": "O3"},
		{"id": "gpt-4.1", "name": "GPT-4.1"},
		{"id": "codex-mini", "name": "Codex Mini"},
	}
	return map[string]any{"models": models}, nil
}

func (s *Server) configRead(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"config": s.cfg}, nil
}

// configEnvAllowPrefixes 允许通过 JSON-RPC 设置的环境变量前缀。
//
// 拒绝设置 PATH, HOME, SHELL 等系统关键变量, 防止注入。
var configEnvAllowPrefixes = []string{
	"OPENAI_",
	"ANTHROPIC_",
	"CODEX_",
	"MODEL",
	"LOG_LEVEL",
	"AGENT_",
	"MCP_",
	"APP_",
	"STRESS_TEST_", // 测试用
	"TEST_E2E_",    // 测试用
}

// isAllowedEnvKey 检查环境变量名是否在允许列表中。
func isAllowedEnvKey(key string) bool {
	for _, prefix := range configEnvAllowPrefixes {
		if strings.HasPrefix(strings.ToUpper(key), prefix) {
			return true
		}
	}
	return false
}

// configValueWriteParams config/value/write 请求参数。
type configValueWriteParams struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (s *Server) configValueWriteTyped(_ context.Context, p configValueWriteParams) (any, error) {
	if !isAllowedEnvKey(p.Key) {
		return nil, apperrors.Newf("Server.configValueWrite", "key %q not in allowlist", p.Key)
	}
	if err := os.Setenv(p.Key, p.Value); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// configBatchWriteParams config/batchWrite 请求参数。
type configBatchWriteParams struct {
	Entries []configBatchWriteEntry `json:"entries"`
}

type configBatchWriteEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (s *Server) configBatchWriteTyped(_ context.Context, p configBatchWriteParams) (any, error) {
	var rejected []string
	for _, e := range p.Entries {
		if !isAllowedEnvKey(e.Key) {
			rejected = append(rejected, e.Key)
			continue
		}
		if err := os.Setenv(e.Key, e.Value); err != nil {
			logger.Warn("config/batchWrite: setenv failed", logger.FieldKey, e.Key, logger.FieldError, err)
		}
	}
	result := map[string]any{}
	if len(rejected) > 0 {
		result["rejected"] = rejected
	}
	return result, nil
}

func (s *Server) configLSPPromptHintRead(ctx context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{
		"hint":        s.resolveLSPUsagePromptHint(ctx),
		"defaultHint": defaultLSPUsagePromptHint,
		"prefKey":     prefKeyLSPUsagePromptHint,
	}, nil
}

type configLSPPromptHintWriteParams struct {
	Hint string `json:"hint"`
}

func (s *Server) configLSPPromptHintWriteTyped(ctx context.Context, p configLSPPromptHintWriteParams) (any, error) {
	if s.prefManager == nil {
		return nil, apperrors.New("Server.configLSPPromptHintWrite", "preference manager not initialized")
	}
	normalized := strings.TrimSpace(p.Hint)
	if err := validateLSPUsagePromptHint(normalized); err != nil {
		return nil, err
	}
	if err := s.prefManager.Set(ctx, prefKeyLSPUsagePromptHint, normalized); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":           true,
		"hint":         s.resolveLSPUsagePromptHint(ctx),
		"usingDefault": normalized == "",
	}, nil
}

func (s *Server) mcpServerStatusList(_ context.Context, _ json.RawMessage) (any, error) {
	if s.lsp == nil {
		return map[string]any{"servers": []map[string]any{}}, nil
	}
	statuses := s.lsp.Statuses()
	servers := make([]map[string]any, 0, len(statuses))
	for _, st := range statuses {
		servers = append(servers, map[string]any{
			"language":  st.Language,
			"command":   st.Command,
			"available": st.Available,
			"running":   st.Running,
		})
	}
	return map[string]any{"servers": servers}, nil
}

// mcpServerReload 重载所有 MCP/LSP 语言服务器 (JSON-RPC: config/mcpServer/reload)。
func (s *Server) mcpServerReload(_ context.Context, _ json.RawMessage) (any, error) {
	if s.lsp == nil {
		return map[string]any{"reloaded": false}, nil
	}
	s.lsp.Reload()
	logger.Info("mcpServer/reload: all language servers restarted")
	return map[string]any{"reloaded": true}, nil
}

type lspDiagnosticsQueryParams struct {
	FilePath string `json:"file_path"`
}

func (s *Server) lspDiagnosticsQueryTyped(_ context.Context, p lspDiagnosticsQueryParams) (any, error) {
	if s.lsp == nil {
		return map[string]any{}, nil
	}

	formatDiagnostics := func(diags []lsp.Diagnostic) []map[string]any {
		out := make([]map[string]any, 0, len(diags))
		for _, d := range diags {
			out = append(out, map[string]any{
				"message":  d.Message,
				"severity": d.Severity.String(),
				"line":     d.Range.Start.Line,
				"column":   d.Range.Start.Character,
			})
		}
		return out
	}

	s.diagMu.RLock()
	defer s.diagMu.RUnlock()

	result := map[string]any{}
	if strings.TrimSpace(p.FilePath) != "" {
		uri := p.FilePath
		if !strings.HasPrefix(uri, "file://") {
			if abs, err := filepath.Abs(uri); err == nil {
				uri = "file://" + abs
			}
		}
		if diags, ok := s.diagCache[uri]; ok && len(diags) > 0 {
			result[uri] = formatDiagnostics(diags)
		}
		return result, nil
	}

	for uri, diags := range s.diagCache {
		if len(diags) == 0 {
			continue
		}
		result[uri] = formatDiagnostics(diags)
	}
	return result, nil
}

// skillsConfigWriteParams skills/config/write 请求参数。
//
// 两种模式:
//  1. 写入 SKILL.md 文件: {"name": "skill_name", "content": "..."}
//  2. 为会话配置技能列表: {"agent_id": "thread-xxx", "skills": ["s1", "s2"]}
type skillsConfigWriteParams struct {
	// 模式 1: 写文件
	Name    string `json:"name"`
	Content string `json:"content"`
	// 模式 2: per-session 技能配置
	AgentID string   `json:"agent_id"`
	Skills  []string `json:"skills"`
}

type skillsMatchPreviewParams struct {
	ThreadID string      `json:"threadId"`
	AgentID  string      `json:"agent_id,omitempty"`
	Text     string      `json:"text"`
	Input    []UserInput `json:"input,omitempty"`
}

type skillsMatchPreviewItem struct {
	Name         string   `json:"name"`
	MatchedBy    string   `json:"matched_by"`
	MatchedTerms []string `json:"matched_terms,omitempty"`
}

func resolveSkillMatchPreviewThreadID(p skillsMatchPreviewParams) string {
	threadID := strings.TrimSpace(p.ThreadID)
	if threadID != "" {
		return threadID
	}
	return strings.TrimSpace(p.AgentID)
}

type skillsConfigReadParams struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) skillsMatchPreviewTyped(_ context.Context, p skillsMatchPreviewParams) (any, error) {
	threadID := resolveSkillMatchPreviewThreadID(p)
	matches := s.collectAutoMatchedSkillMatches(threadID, p.Text, p.Input, autoSkillMatchOptions{
		IncludeConfiguredExplicit: true,
	})
	items := make([]skillsMatchPreviewItem, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match.Name)
		if name == "" {
			continue
		}
		item := skillsMatchPreviewItem{
			Name:      name,
			MatchedBy: match.MatchedBy,
		}
		if len(match.MatchedTerms) > 0 {
			item.MatchedTerms = append([]string(nil), match.MatchedTerms...)
		}
		items = append(items, item)
	}
	return map[string]any{
		"thread_id": threadID,
		"matches":   items,
	}, nil
}

func (s *Server) skillsConfigReadTyped(_ context.Context, p skillsConfigReadParams) (any, error) {
	agentID := strings.TrimSpace(p.AgentID)
	if agentID == "" {
		return nil, apperrors.New("Server.skillsConfigRead", "agent_id is required")
	}
	return map[string]any{
		"agent_id": agentID,
		"skills":   s.GetAgentSkills(agentID),
	}, nil
}

func (s *Server) skillsConfigWriteTyped(_ context.Context, p skillsConfigWriteParams) (any, error) {
	// 模式 2: 为指定 agent/session 配置技能列表
	if p.AgentID != "" {
		normalizedSkills, err := normalizeSkillNames(p.Skills)
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "normalize skills")
		}
		s.skillsMu.Lock()
		if len(normalizedSkills) == 0 {
			delete(s.agentSkills, p.AgentID)
		} else {
			s.agentSkills[p.AgentID] = normalizedSkills
		}
		s.skillsMu.Unlock()
		logger.Info("skills/config/write: agent skills configured",
			logger.FieldAgentID, p.AgentID, "skills", normalizedSkills)
		return map[string]any{"ok": true, "agent_id": p.AgentID, "skills": normalizedSkills}, nil
	}

	// 模式 1: 写 SKILL.md 文件
	if strings.TrimSpace(p.Name) == "" {
		return nil, apperrors.New("Server.skillsConfigWrite", "name or agent_id is required")
	}
	skillName, err := normalizeSkillName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "normalize skill name")
	}
	dir := filepath.Join(s.skillsDirectory(), skillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "mkdir")
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "write SKILL.md")
	}
	logger.Info("skills/config/write: saved", logger.FieldSkill, skillName, logger.FieldBytes, len(p.Content))
	return map[string]any{"ok": true, "path": path}, nil
}

// GetAgentSkills 返回指定 agent 配置的技能列表。
func (s *Server) GetAgentSkills(agentID string) []string {
	s.skillsMu.RLock()
	defer s.skillsMu.RUnlock()
	values := s.agentSkills[agentID]
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

// ========================================
// command/exec
// ========================================

type commandExecParams struct {
	Argv []string          `json:"argv"`
	Cwd  string            `json:"cwd,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

// commandBlocklist 禁止通过 command/exec 执行的危险命令。
var commandBlocklist = map[string]bool{
	"rm":       true,
	"rmdir":    true,
	"sudo":     true,
	"su":       true,
	"chmod":    true,
	"chown":    true,
	"mkfs":     true,
	"dd":       true,
	"kill":     true,
	"killall":  true,
	"pkill":    true,
	"shutdown": true,
	"reboot":   true,
	"passwd":   true,
	"useradd":  true,
	"userdel":  true,
	"mount":    true,
	"umount":   true,
	"fdisk":    true,
	"iptables": true,
	"curl":     true, // 防止外部请求
	"wget":     true,
}

const maxOutputSize = 1 << 20 // 1MB 输出限制

// commandExecResponse command/exec 响应。
type commandExecResponse struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func (s *Server) commandExecTyped(ctx context.Context, p commandExecParams) (any, error) {
	if len(p.Argv) == 0 {
		return nil, apperrors.New("Server.commandExec", "argv is required")
	}

	// 安全检查: 提取基础命令名 (去掉路径)
	baseName := filepath.Base(p.Argv[0])
	if commandBlocklist[baseName] {
		return nil, apperrors.Newf("Server.commandExec", "command %q is blocked for security", baseName)
	}

	// 禁止管道/shell 注入: 检查参数中是否有 shell 元字符
	for _, arg := range p.Argv {
		if strings.ContainsAny(arg, "|;&$`") {
			return nil, apperrors.New("Server.commandExec", "shell metacharacters not allowed in arguments")
		}
	}

	logger.Infow("command/exec: starting",
		logger.FieldCommand, baseName,
		logger.FieldCwd, p.Cwd,
		"argc", len(p.Argv),
	)

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, p.Argv[0], p.Argv[1:]...)
	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}
	if len(p.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range p.Env {
			if !isAllowedEnvKey(k) {
				continue // 跳过不允许的环境变量
			}
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// 限制输出大小, 防止内存耗尽
	var stdout, stderr strings.Builder
	stdout.Grow(4096)
	stderr.Grow(4096)
	cmd.Stdout = util.NewLimitedWriter(&stdout, maxOutputSize)
	cmd.Stderr = util.NewLimitedWriter(&stderr, maxOutputSize)

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			logger.Error("command/exec: run failed",
				logger.FieldCommand, baseName,
				logger.FieldError, err,
				logger.FieldDurationMS, elapsed.Milliseconds(),
			)
			return nil, apperrors.Wrap(err, "Server.commandExec", "run command")
		}
	}

	logger.Infow("command/exec: completed",
		logger.FieldCommand, baseName,
		logger.FieldExitCode, exitCode,
		logger.FieldDurationMS, elapsed.Milliseconds(),
	)

	return commandExecResponse{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// ========================================
// account
// ========================================

// accountLoginStartParams account/login/start 请求参数。
type accountLoginStartParams struct {
	AuthMode string `json:"authMode"`
	APIKey   string `json:"apiKey,omitempty"`
}

func (s *Server) accountLoginStartTyped(_ context.Context, p accountLoginStartParams) (any, error) {
	if p.APIKey != "" {
		if err := os.Setenv("OPENAI_API_KEY", p.APIKey); err != nil {
			logger.Warn("account/login: setenv failed", logger.FieldError, err)
			return nil, apperrors.Wrap(err, "Server.accountLoginStart", "setenv OPENAI_API_KEY")
		}
		return map[string]any{}, nil
	}
	return map[string]any{"loginUrl": "https://platform.openai.com/api-keys"}, nil
}

func (s *Server) accountLoginCancel(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{}, nil
}

func (s *Server) accountLogout(_ context.Context, _ json.RawMessage) (any, error) {
	if err := os.Unsetenv("OPENAI_API_KEY"); err != nil {
		logger.Warn("account/logout: unsetenv failed", logger.FieldError, err)
	}
	return map[string]any{}, nil
}

func (s *Server) accountRead(_ context.Context, _ json.RawMessage) (any, error) {
	key := os.Getenv("OPENAI_API_KEY")
	masked := ""
	if len(key) > 8 {
		masked = key[:4] + "..." + key[len(key)-4:]
	}
	return map[string]any{
		"account": map[string]any{
			"hasApiKey": key != "",
			"maskedKey": masked,
		},
	}, nil
}

// ========================================
// helpers
// ========================================

// withThread 查找线程并执行回调 (消除重复的 getThread→notFound 样板)。
func (s *Server) withThread(threadID string, fn func(*runner.AgentProcess) (any, error)) (any, error) {
	proc := s.mgr.Get(threadID)
	if proc == nil {
		return nil, apperrors.Newf("Server.withThread", "thread %s not found", threadID)
	}
	return fn(proc)
}

func (s *Server) threadExistsInHistory(ctx context.Context, threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	if isLikelyCodexThreadID(id) {
		return true
	}

	if s.bindingStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		binding, err := s.bindingStore.FindByAgentID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from agent_codex_binding failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if binding != nil {
			return true
		}
	}

	if s.agentStatusStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		status, err := s.agentStatusStore.Get(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from agent_status failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if status != nil {
			return true
		}
	}

	return false
}

func isLikelyCodexThreadID(raw string) bool {
	id := strings.TrimSpace(raw)
	if id == "" {
		return false
	}
	id = strings.TrimPrefix(strings.ToLower(id), "urn:uuid:")
	return codexThreadIDPattern.MatchString(id)
}

func normalizeCodexThreadID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	id = strings.TrimPrefix(strings.ToLower(id), "urn:uuid:")
	if !codexThreadIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func appendUniqueThreadID(dst []string, seen map[string]struct{}, candidate string) []string {
	id := strings.TrimSpace(candidate)
	if !isLikelyCodexThreadID(id) {
		return dst
	}
	if _, ok := seen[id]; ok {
		return dst
	}
	seen[id] = struct{}{}
	return append(dst, id)
}

func buildResumeCandidates(threadID string, resolved []string) []string {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	if isLikelyCodexThreadID(id) {
		return []string{id}
	}

	candidates := make([]string, 0, len(resolved))
	seen := map[string]struct{}{}
	for _, candidate := range resolved {
		value := strings.TrimSpace(candidate)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}
	if len(candidates) > 0 {
		return candidates
	}
	return []string{id}
}

// tryResumeCandidates 按顺序尝试候选 thread ID 恢复会话。
//
// 行为:
//   - 成功 → 返回 (成功ID, nil)
//   - 候选错误 (isHistoricalResumeCandidateError) → 跳过,尝试下一个
//   - 非候选错误 (网络等) → 立即返回 error
//   - 所有候选耗尽 → 返回 error (避免伪造 resumed 成功)
//   - 无候选 → 返回 error
func tryResumeCandidates(candidates []string, fallbackID string, resumeFn func(string) error) (string, error) {
	if len(candidates) == 0 {
		logger.Warn("thread/resume: no resume candidates available",
			logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
			"reason", "no codex thread ID resolved from history",
		)
		return "", apperrors.Newf("tryResumeCandidates", "no resume candidates available for thread %s", fallbackID)
	}

	var lastErr error
	for _, id := range candidates {
		err := resumeFn(id)
		if err == nil {
			return id, nil
		}
		lastErr = err
		if isHistoricalResumeCandidateError(err) {
			logger.Warn("thread/resume: candidate unavailable, trying next",
				logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
				"resume_thread_id", id,
				logger.FieldError, err,
			)
			continue
		}
		// 非候选错误 (网络断开等) → 直接传播
		return "", err
	}

	// 所有候选都是 candidate error → 返回 error，避免伪装恢复成功
	logger.Warn("thread/resume: all resume candidates exhausted",
		logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
		"candidate_count", len(candidates),
		"last_error", lastErr,
		"reason", "all historical rollouts unavailable",
	)
	if lastErr != nil {
		return "", apperrors.Wrapf(lastErr, "tryResumeCandidates", "all resume candidates unavailable for thread %s after %d attempts", fallbackID, len(candidates))
	}
	return "", apperrors.Newf("tryResumeCandidates", "all resume candidates unavailable for thread %s after %d attempts", fallbackID, len(candidates))
}

func previewResumeCandidates(candidates []string, max int) []string {
	if len(candidates) == 0 || max <= 0 {
		return nil
	}
	if len(candidates) <= max {
		return append([]string(nil), candidates...)
	}
	preview := append([]string(nil), candidates[:max]...)
	preview = append(preview, fmt.Sprintf("...+%d more", len(candidates)-max))
	return preview
}

func (s *Server) resolveHistoricalCodexThreadID(ctx context.Context, agentID string) string {
	ids := s.resolveHistoricalCodexThreadIDs(ctx, agentID)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func (s *Server) resolveHistoricalCodexThreadIDs(ctx context.Context, agentID string) []string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil
	}

	ids := make([]string, 0, 2)
	seen := map[string]struct{}{}
	ids = appendUniqueThreadID(ids, seen, id)

	if s.bindingStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		binding, err := s.bindingStore.FindByAgentID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: resolve codex thread id from binding failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if binding != nil {
			ids = appendUniqueThreadID(ids, seen, binding.CodexThreadID)
		}
	}

	if s.agentStatusStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		status, err := s.agentStatusStore.Get(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: resolve codex thread id from agent_status failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if status != nil {
			ids = appendUniqueThreadID(ids, seen, status.SessionID)
		}
	}

	logger.Info("turn/start: historical resume candidates",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"candidate_count", len(ids),
		"candidates", previewResumeCandidates(ids, 4),
	)
	return ids
}

func isHistoricalResumeCandidateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no rollout found for thread id"),
		strings.Contains(msg, "failed to load rollout"),
		strings.Contains(msg, "invalid thread id"),
		strings.Contains(msg, "thread/resume returned empty thread id"),
		strings.Contains(msg, "thread/resume returned empty response without fallback thread id"),
		strings.Contains(msg, "websocket: close 1006"),
		strings.Contains(msg, "abnormal closure"):
		return true
	default:
		return false
	}
}

// isCodexProcessCrashError 判断错误是否为 codex 进程 crash (需要 re-spawn)。
//
// 与 isHistoricalResumeCandidateError 的区别:
//   - candidateError: rollout 不可用但进程仍健在 → 直接尝试下一个候选
//   - crashError: 进程已死 → 必须 Stop + Launch 新进程后才能继续
func isCodexProcessCrashError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "websocket: close 1006") ||
		strings.Contains(msg, "abnormal closure")
}

// buildSessionLostNotification 构建会话丢失降级通知 (method + payload)。
//
// 使用 ui/state/changed 以复用前端已有的事件监听，无需前端新增处理。
func buildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
	detail := ""
	if lastErr != nil {
		detail = lastErr.Error()
	}
	return "ui/state/changed", map[string]any{
		"source":   "session_lost_warning",
		"agent_id": agentID,
		"warning":  "会话历史已丢失 (codex session 文件不存在)，已自动回退到全新会话",
		"detail":   detail,
	}
}
func (s *Server) ensureThreadReadyForTurn(ctx context.Context, threadID, cwd string) (*runner.AgentProcess, error) {
	// D11: 总超时 45s，避免 Launch(30s)+Resume(30s) 串行导致前端 turn/start 永不回。
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New("Server.ensureThreadReady", "threadId is required")
	}
	launchCwd := strings.TrimSpace(cwd)
	if launchCwd == "" {
		launchCwd = "."
	}

	if proc := s.mgr.Get(id); proc != nil {
		logger.Info("turn/start: using running process",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			logger.FieldPort, proc.Client.GetPort(),
			"codex_thread_id", strings.TrimSpace(proc.Client.GetThreadID()),
		)
		return proc, nil
	}
	hasHistory := s.threadExistsInHistory(ctx, id)
	if !hasHistory {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s not found", id)
	}
	resumeCandidates := make([]string, 0, 4)

	// 优先从 agent_codex_binding 表获取绑定的 codexThreadId (根基约束: 1:1 共生)。
	if s.bindingStore != nil {
		if binding, err := s.bindingStore.FindByAgentID(ctx, id); err == nil && binding != nil {
			resumeCandidates = append(resumeCandidates, binding.CodexThreadID)
			logger.Info("turn/start: found DB binding",
				logger.FieldAgentID, id,
				"bound_codex_thread_id", binding.CodexThreadID,
			)
		}
	}

	// Fallback: 如果 DB 无绑定, 仅尝试将输入 threadId 作为 codexThreadId 使用。
	if len(resumeCandidates) == 0 {
		if isLikelyCodexThreadID(id) {
			resumeCandidates = append(resumeCandidates, id)
		} else {
			resumeCandidates = append(resumeCandidates, s.resolveHistoricalCodexThreadIDs(ctx, id)...)
		}
	}

	logger.Info("turn/start: restoring historical thread",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"has_history", hasHistory,
		logger.FieldCwd, launchCwd,
		"candidate_count", len(resumeCandidates),
		"candidates", previewResumeCandidates(resumeCandidates, 4),
	)

	dynamicTools := s.buildAllDynamicTools()

	if err := s.mgr.Launch(ctx, id, id, "", launchCwd, dynamicTools); err != nil {
		// 并发补加载时可能已被其他请求拉起，二次确认后再报错。
		if proc := s.mgr.Get(id); proc != nil {
			return proc, nil
		}
		return nil, apperrors.Wrapf(err, "Server.ensureThreadReady", "auto-load thread %s", id)
	}

	proc := s.mgr.Get(id)
	if proc == nil {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s loaded but not found", id)
	}
	logger.Info("turn/start: process launched for restore",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		logger.FieldPort, proc.Client.GetPort(),
		"codex_thread_id_before_resume", strings.TrimSpace(proc.Client.GetThreadID()),
	)
	if len(resumeCandidates) == 0 {
		logger.Warn("turn/start: no valid historical codex thread id, continue with fresh session",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
		)
		proc.MarkSessionLost()
		return proc, nil
	}
	var lastResumeErr error
	for _, resumeThreadID := range resumeCandidates {
		err := proc.Client.ResumeThread(codex.ResumeThreadRequest{
			ThreadID: resumeThreadID,
			Cwd:      launchCwd,
		})
		if err == nil {
			logger.Info("turn/start: historical thread auto-loaded",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				"codex_thread_id_after_resume", strings.TrimSpace(proc.Client.GetThreadID()),
				logger.FieldCwd, launchCwd,
			)
			s.registerBinding(ctx, id, proc)
			return proc, nil
		}

		lastResumeErr = err

		// codex 进程 crash → 直接返回错误，不偷偷换 fresh session
		if isCodexProcessCrashError(err) {
			logger.Error("turn/start: codex crashed during resume, returning error",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)
			_ = s.mgr.Stop(id)
			s.broadcastNotification(buildSessionLostNotification(id, err))
			return nil, apperrors.Wrapf(err, "Server.ensureThreadReady",
				"codex crashed while resuming thread %s (rollout=%s)", id, resumeThreadID)
		}

		// 非 crash 的候选错误 (rollout 不存在等) → 尝试下一个候选
		if isHistoricalResumeCandidateError(err) {
			logger.Warn("turn/start: resume candidate unavailable, try next",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)
			continue
		}

		// 不可识别的错误 → 也返回错误
		logger.Error("turn/start: unrecognized resume error",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			"resume_thread_id", resumeThreadID,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrapf(err, "Server.ensureThreadReady",
			"resume failed for thread %s (rollout=%s)", id, resumeThreadID)
	}

	// 所有候选的 rollout 都不可用 (非 crash) → fallback 到 fresh session + 通知前端
	if lastResumeErr != nil {
		logger.Warn("turn/start: all resume candidates exhausted, fallback to fresh session",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			"candidate_count", len(resumeCandidates),
			"last_error", lastResumeErr,
			logger.FieldCwd, launchCwd,
		)
		// proc 在 non-crash 路径中仍然存活，但可能被 mgr 移除
		if s.mgr.Get(id) == nil {
			_ = s.mgr.Stop(id)
			if launchErr := s.mgr.Launch(ctx, id, id, "", launchCwd, dynamicTools); launchErr != nil {
				return nil, apperrors.Wrapf(launchErr, "Server.ensureThreadReady", "final re-spawn thread %s", id)
			}
			proc = s.mgr.Get(id)
			if proc == nil {
				return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s final re-spawn failed", id)
			}
		}
		proc.MarkSessionLost()
		s.broadcastNotification(buildSessionLostNotification(id, lastResumeErr))
		s.registerBinding(ctx, id, proc)
		return proc, nil
	}

	logger.Warn("turn/start: no available historical rollout, continue with fresh session",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"candidate_count", len(resumeCandidates),
		logger.FieldCwd, launchCwd,
	)
	proc.MarkSessionLost()
	s.registerBinding(ctx, id, proc)
	return proc, nil
}

// registerBinding 注册 agentId ↔ codexThreadId 绑定。
//
// ⚠️  根基约束: agent_id 与 codex_thread_id 1:1 共生。
// 此函数在每次 ensureThreadReadyForTurn 成功后调用,
// 确保 DB 绑定记录始终与运行时状态一致。
func (s *Server) registerBinding(ctx context.Context, agentID string, proc *runner.AgentProcess) {
	if s.bindingStore == nil || proc == nil || proc.Client == nil {
		return
	}
	codexThreadID := strings.TrimSpace(proc.Client.GetThreadID())
	if codexThreadID == "" {
		return
	}
	if err := s.bindingStore.Bind(ctx, agentID, codexThreadID, ""); err != nil {
		logger.Warn("turn/start: failed to register binding",
			logger.FieldAgentID, agentID,
			"codex_thread_id", codexThreadID,
			logger.FieldError, err,
		)
	}
}

// sendSlashCommand 通用斜杠命令发送 (compact, interrupt 等)。
func resolveSlashCommandThread(
	ctx context.Context,
	threadID string,
	getProc func(string) *runner.AgentProcess,
	ensureReady func(context.Context, string, string) (*runner.AgentProcess, error),
) (*runner.AgentProcess, error) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New("Server.sendSlashCommand", "threadId is required")
	}
	if getProc != nil {
		if proc := getProc(id); proc != nil {
			return proc, nil
		}
	}
	if ensureReady == nil {
		return nil, apperrors.Newf("Server.sendSlashCommand", "thread %s not found", id)
	}
	proc, err := ensureReady(ctx, id, "")
	if err != nil {
		return nil, err
	}
	if proc == nil {
		return nil, apperrors.Newf("Server.sendSlashCommand", "thread %s not found", id)
	}
	return proc, nil
}

func (s *Server) resolveThreadForSlashCommand(ctx context.Context, threadID string) (*runner.AgentProcess, error) {
	if s == nil || s.mgr == nil {
		return nil, apperrors.New("Server.sendSlashCommand", "thread manager is not initialized")
	}
	return resolveSlashCommandThread(ctx, threadID, s.mgr.Get, s.ensureThreadReadyForTurn)
}

func (s *Server) sendSlashCommand(ctx context.Context, params json.RawMessage, command string) (any, error) {
	start := time.Now()
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.sendSlashCommand", "unmarshal params")
	}
	stateBefore := s.readThreadRuntimeState(p.ThreadID)
	activeTrackedBefore := s.hasActiveTrackedTurn(p.ThreadID)
	activeBefore := isInterruptActiveState(stateBefore)
	logger.Info("slash/command: request",
		logger.FieldAgentID, p.ThreadID,
		logger.FieldThreadID, p.ThreadID,
		logger.FieldCommand, command,
		logger.FieldParamsLen, len(params),
		"state_before", stateBefore,
		"active_before", activeBefore,
		"active_tracked_before", activeTrackedBefore,
	)
	if strings.EqualFold(strings.TrimSpace(command), "/compact") && (activeBefore || activeTrackedBefore) {
		logger.Warn("thread/compact/start requested while turn active; compact may be ignored by codex",
			logger.FieldAgentID, p.ThreadID,
			logger.FieldThreadID, p.ThreadID,
			"state_before", stateBefore,
			"active_before", activeBefore,
			"active_tracked_before", activeTrackedBefore,
		)
	}
	proc, err := s.resolveThreadForSlashCommand(ctx, p.ThreadID)
	if err != nil {
		logger.Warn("slash/command: resolve thread failed",
			logger.FieldAgentID, p.ThreadID,
			logger.FieldThreadID, p.ThreadID,
			logger.FieldCommand, command,
			logger.FieldError, err,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return nil, err
	}
	if err := proc.Client.SendCommand(command, ""); err != nil {
		logger.Warn("slash/command: send failed",
			logger.FieldAgentID, p.ThreadID,
			logger.FieldThreadID, p.ThreadID,
			logger.FieldCommand, command,
			logger.FieldError, err,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return nil, err
	}
	logger.Info("slash/command: sent",
		logger.FieldAgentID, p.ThreadID,
		logger.FieldThreadID, p.ThreadID,
		logger.FieldCommand, command,
		logger.FieldDurationMS, time.Since(start).Milliseconds(),
	)
	return map[string]any{}, nil
}

// sendSlashCommandWithArgs 带参数的斜杠命令。
func (s *Server) sendSlashCommandWithArgs(params json.RawMessage, command string, argsField string) (any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(params, &raw); err != nil {
		return nil, apperrors.Wrap(err, "Server.sendSlashCommandWithArgs", "unmarshal params")
	}

	var threadID string
	if v, ok := raw["threadId"]; ok {
		if err := json.Unmarshal(v, &threadID); err != nil {
			return nil, apperrors.Wrap(err, "Server.sendSlashCommandWithArgs", "unmarshal threadId")
		}
	}
	if threadID == "" {
		return nil, apperrors.New("Server.sendSlashCommandWithArgs", "threadId is required")
	}

	var args string
	if v, ok := raw[argsField]; ok {
		_ = json.Unmarshal(v, &args)
	}

	return s.withThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand(command, args); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

// extractInputs 从 UserInput 数组提取 prompt/images/files。
func extractInputs(inputs []UserInput) (prompt string, images, files []string) {
	var texts []string
	isRemoteImageURL := func(raw string) bool {
		value := strings.ToLower(strings.TrimSpace(raw))
		return strings.HasPrefix(value, "http://") ||
			strings.HasPrefix(value, "https://") ||
			strings.HasPrefix(value, "data:image/")
	}
	for _, inp := range inputs {
		switch strings.ToLower(strings.TrimSpace(inp.Type)) {
		case "text":
			texts = append(texts, inp.Text)
		case "image":
			if value := strings.TrimSpace(inp.URL); value != "" {
				images = append(images, value)
				continue
			}
			if value := strings.TrimSpace(inp.Path); value != "" {
				images = append(images, value)
			}
		case "localimage":
			if value := strings.TrimSpace(inp.URL); isRemoteImageURL(value) {
				images = append(images, value)
				continue
			}
			if value := strings.TrimSpace(inp.Path); value != "" {
				images = append(images, value)
			}
		case "filecontent":
			if value := strings.TrimSpace(inp.Path); value != "" {
				files = append(files, value)
				continue
			}
			if inline := fileContentInputText(inp.Name, inp.Content); inline != "" {
				texts = append(texts, inline)
			}
		case "mention", "file":
			if value := strings.TrimSpace(inp.Path); value != "" {
				files = append(files, value)
			}
		case "skill":
			texts = append(texts, skillInputText(inp.Name, inp.Content))
		}
	}
	prompt = strings.Join(texts, "\n")
	return
}

func buildAttachmentName(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:image/") {
		ext := strings.TrimSpace(strings.TrimPrefix(lower, "data:image/"))
		if idx := strings.Index(ext, ";"); idx >= 0 {
			ext = ext[:idx]
		}
		ext = strings.TrimSpace(ext)
		if ext == "" {
			return "image"
		}
		return "image." + ext
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if parsed, err := url.Parse(value); err == nil {
			base := strings.TrimSpace(filepath.Base(parsed.Path))
			if base != "" && base != "." && base != string(filepath.Separator) {
				return base
			}
		}
		return value
	}
	base := strings.TrimSpace(filepath.Base(value))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return value
	}
	return base
}

func buildAttachmentPreviewURL(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:image/") ||
		strings.HasPrefix(lower, "file://") {
		return value
	}
	return (&url.URL{Scheme: "file", Path: value}).String()
}

func buildUserTimelineAttachments(images, files []string) []uistate.TimelineAttachment {
	attachments := make([]uistate.TimelineAttachment, 0, len(images)+len(files))
	for _, raw := range images {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = append(attachments, uistate.TimelineAttachment{
			Kind:       "image",
			Name:       buildAttachmentName(path),
			Path:       path,
			PreviewURL: buildAttachmentPreviewURL(path),
		})
	}
	for _, raw := range files {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = append(attachments, uistate.TimelineAttachment{
			Kind: "file",
			Name: buildAttachmentName(path),
			Path: path,
		})
	}
	return attachments
}

func buildUserTimelineAttachmentsFromInputs(inputs []UserInput) []uistate.TimelineAttachment {
	if len(inputs) == 0 {
		return nil
	}
	attachments := make([]uistate.TimelineAttachment, 0, len(inputs))
	for _, input := range inputs {
		kind := strings.ToLower(strings.TrimSpace(input.Type))
		switch kind {
		case "image":
			imageURL := strings.TrimSpace(input.URL)
			if imageURL == "" {
				imageURL = strings.TrimSpace(input.Path)
			}
			if imageURL == "" {
				continue
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind:       "image",
				Name:       buildAttachmentName(imageURL),
				Path:       imageURL,
				PreviewURL: buildAttachmentPreviewURL(imageURL),
			})
		case "localimage":
			imagePath := strings.TrimSpace(input.Path)
			preview := strings.TrimSpace(input.URL)
			if preview == "" {
				preview = imagePath
			}
			if preview == "" {
				continue
			}
			nameSource := imagePath
			if nameSource == "" {
				nameSource = preview
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind:       "image",
				Name:       buildAttachmentName(nameSource),
				Path:       imagePath,
				PreviewURL: buildAttachmentPreviewURL(preview),
			})
		case "mention", "file":
			path := strings.TrimSpace(input.Path)
			if path == "" {
				continue
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind: "file",
				Name: buildAttachmentName(path),
				Path: path,
			})
		case "filecontent":
			path := strings.TrimSpace(input.Path)
			if path != "" {
				attachments = append(attachments, uistate.TimelineAttachment{
					Kind: "file",
					Name: buildAttachmentName(path),
					Path: path,
				})
				continue
			}
			if strings.TrimSpace(input.Content) == "" {
				continue
			}
			name := strings.TrimSpace(input.Name)
			if name == "" {
				name = "inline-file"
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind: "file",
				Name: name,
			})
		}
	}
	return attachments
}

// ========================================
// 补全方法 (§ 1–9 需实现项)
// ========================================

// threadBgTerminalsClean 清理后台终端 (experimental)。
func (s *Server) threadBgTerminalsClean(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/clean")
}

// skillsRemoteReadParams skills/remote/read 请求参数。
type skillsRemoteReadParams struct {
	URL string `json:"url"`
}

// skillsRemoteReadTyped 读取远程 Skill。
func (s *Server) skillsRemoteReadTyped(_ context.Context, p skillsRemoteReadParams) (any, error) {
	logger.Infow("skills/remote/read: fetching", logger.FieldURL, p.URL)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(p.URL)
	if err != nil {
		logger.Warn("skills/remote/read: fetch failed", logger.FieldURL, p.URL, logger.FieldError, err)
		return nil, apperrors.Wrap(err, "Server.skillsRemoteRead", "fetch remote skill")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, apperrors.Newf(
			"Server.skillsRemoteRead",
			"fetch remote skill failed status=%d body=%s",
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsRemoteRead", "read response body")
	}
	return map[string]any{
		"skill": map[string]string{"url": p.URL, "content": string(body)},
	}, nil
}

// skillsRemoteWriteParams skills/remote/write 请求参数。
type skillsRemoteWriteParams struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// skillsRemoteWriteTyped 写入远程 Skill 到本地。
func (s *Server) skillsRemoteWriteTyped(_ context.Context, p skillsRemoteWriteParams) (any, error) {
	skillName, err := normalizeSkillName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsRemoteWrite", "normalize skill name")
	}
	skillsDir := filepath.Join(s.skillsDirectory(), skillName)
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(p.Content), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// collaborationModeList 列出协作模式 (experimental)。
func (s *Server) collaborationModeList(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"modes": []map[string]string{
		{"id": "default", "name": "Default"},
		{"id": "pair", "name": "Pair Programming"},
	}}, nil
}

// experimentalFeatureList 列出实验性功能。
func (s *Server) experimentalFeatureList(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"features": map[string]bool{
		"backgroundTerminals": true,
		"collaborationMode":   true,
		"fuzzySearchSession":  true,
	}}, nil
}

// configRequirementsRead 读取配置需求。
func (s *Server) configRequirementsRead(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"requirements": map[string]any{
		"apiKey": map[string]string{
			"status":  boolToStatus(os.Getenv("OPENAI_API_KEY") != ""),
			"message": "OPENAI_API_KEY environment variable",
		},
	}}, nil
}

// accountRateLimitsRead 读取速率限制。
func (s *Server) accountRateLimitsRead(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"rateLimits": map[string]any{}}, nil
}

// boolToStatus bool → "met" / "unmet"。
func boolToStatus(ok bool) string {
	if ok {
		return "met"
	}
	return "unmet"
}

// ========================================
// § 10. 斜杠命令 (SOCKS 独有, JSON-RPC 化)
// ========================================

// threadUndo 撤销上一步 (/undo)。
func (s *Server) threadUndo(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/undo")
}

// threadModelSet 切换模型 (/model <name>)。
func (s *Server) threadModelSet(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommandWithArgs(params, "/model", "model")
}

// threadPersonality 设置人格 (/personality <type>)。
func (s *Server) threadPersonality(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommandWithArgs(params, "/personality", "personality")
}

// threadApprovals 设置审批策略 (/approvals <policy>)。
func (s *Server) threadApprovals(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommandWithArgs(params, "/approvals", "policy")
}

// threadMCPList 列出 MCP 工具 (/mcp)。
func (s *Server) threadMCPList(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/mcp")
}

// threadSkillsList 列出 Skills (/skills)。
func (s *Server) threadSkillsList(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/skills")
}

// threadDebugMemory 调试记忆 (/debug-m-drop 或 /debug-m-update)。
func (s *Server) threadDebugMemory(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommandWithArgs(params, "/debug-m-drop", "action")
}

// ========================================
// § 11. 系统日志
// ========================================

// logListParams log/list 请求参数。
type logListParams struct {
	Level     string `json:"level"`
	Logger    string `json:"logger"`
	Source    string `json:"source"`
	Component string `json:"component"`
	AgentID   string `json:"agent_id"`
	ThreadID  string `json:"thread_id"`
	EventType string `json:"event_type"`
	ToolName  string `json:"tool_name"`
	Keyword   string `json:"keyword"`
	Limit     int    `json:"limit"`
}

// logListTyped 查询系统日志 (JSON-RPC: log/list)。
func (s *Server) logListTyped(ctx context.Context, p logListParams) (any, error) {
	if s.sysLogStore == nil {
		return nil, apperrors.New("Server.logList", "log store not initialized")
	}
	if p.Limit <= 0 || p.Limit > 2000 {
		p.Limit = 100
	}
	return s.sysLogStore.ListV2(ctx, store.ListParams{
		Level:     p.Level,
		Logger:    p.Logger,
		Source:    p.Source,
		Component: p.Component,
		AgentID:   p.AgentID,
		ThreadID:  p.ThreadID,
		EventType: p.EventType,
		ToolName:  p.ToolName,
		Keyword:   p.Keyword,
		Limit:     p.Limit,
	})
}

// logFilters 返回日志筛选器可选值 (JSON-RPC: log/filters)。
func (s *Server) logFilters(ctx context.Context, _ json.RawMessage) (any, error) {
	if s.sysLogStore == nil {
		return nil, apperrors.New("Server.logFilters", "log store not initialized")
	}
	return s.sysLogStore.ListFilterValues(ctx)
}

// ========================================
// UI State (Preferences)
// ========================================

const prefThreadAliases = "threads.aliases"

type uiPrefGetParams struct {
	Key string `json:"key"`
}

func (s *Server) uiPreferencesGet(ctx context.Context, p uiPrefGetParams) (any, error) {
	return s.prefManager.Get(ctx, p.Key)
}

type uiPrefSetParams struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func (s *Server) uiPreferencesSet(ctx context.Context, p uiPrefSetParams) (any, error) {
	if err := s.prefManager.Set(ctx, p.Key, p.Value); err != nil {
		return nil, err
	}
	if s.uiRuntime != nil {
		if p.Key == "mainAgentId" {
			s.uiRuntime.SetMainAgent(asString(p.Value))
		}
	}
	// stall 参数运行时热调
	switch p.Key {
	case "stallThresholdSec":
		if sec := asPositiveInt(p.Value, 30); sec > 0 {
			s.stallThreshold = time.Duration(sec) * time.Second
			logger.Info("stall threshold updated via ui/preferences/set", "seconds", sec)
		}
	case "stallHeartbeatSec":
		if sec := asPositiveInt(p.Value, 10); sec > 0 {
			s.stallHeartbeat = time.Duration(sec) * time.Second
			logger.Info("stall heartbeat updated via ui/preferences/set", "seconds", sec)
		}
	}
	return map[string]any{"ok": true}, nil
}

func (s *Server) uiPreferencesGetAll(ctx context.Context, _ json.RawMessage) (any, error) {
	return s.prefManager.GetAll(ctx)
}

func (s *Server) uiStateGet(ctx context.Context, _ json.RawMessage) (any, error) {
	if s.uiRuntime == nil {
		return map[string]any{}, nil
	}
	snapshot := s.uiRuntime.SnapshotLight()
	prefs := map[string]any{}
	if s.prefManager != nil {
		loaded, err := s.prefManager.GetAll(ctx)
		if err != nil {
			logger.Warn("ui/state/get: load preferences failed", logger.FieldError, err)
		} else {
			prefs = loaded
		}
	}
	applyThreadAliasesSnapshot(&snapshot, loadThreadAliasesFromPrefs(prefs))

	resolvedMain := resolveMainAgentPreference(snapshot, prefs)
	if resolvedMain != asString(prefs["mainAgentId"]) {
		s.uiRuntime.SetMainAgent(resolvedMain)
		snapshot = s.uiRuntime.SnapshotLight()
		applyThreadAliasesSnapshot(&snapshot, loadThreadAliasesFromPrefs(prefs))
		pm := s.prefManager
		prev := prefs["mainAgentId"]
		util.SafeGo(func() { persistResolvedUIPreference(context.Background(), pm, "mainAgentId", resolvedMain, prev) })
		prefs["mainAgentId"] = resolvedMain
	}

	resolvedActiveThreadID := resolvePreferredThreadID(snapshot.Threads, asString(prefs["activeThreadId"]))
	prevActive := prefs["activeThreadId"]
	util.SafeGo(func() {
		persistResolvedUIPreference(context.Background(), s.prefManager, "activeThreadId", resolvedActiveThreadID, prevActive)
	})
	prefs["activeThreadId"] = resolvedActiveThreadID

	resolvedActiveCmdThreadID := resolvePreferredCmdThreadID(snapshot.Threads, resolvedMain, asString(prefs["activeCmdThreadId"]))
	prevCmd := prefs["activeCmdThreadId"]
	util.SafeGo(func() {
		persistResolvedUIPreference(context.Background(), s.prefManager, "activeCmdThreadId", resolvedActiveCmdThreadID, prevCmd)
	})
	prefs["activeCmdThreadId"] = resolvedActiveCmdThreadID

	// 按需获取活跃线程的 timeline/diff, 避免深拷贝所有线程
	timelinesByThread := map[string][]uistate.TimelineItem{}
	diffTextByThread := map[string]string{}
	activeIDs := []string{resolvedActiveThreadID, resolvedActiveCmdThreadID}
	for _, tid := range activeIDs {
		tid = strings.TrimSpace(tid)
		if tid == "" {
			continue
		}
		if _, ok := timelinesByThread[tid]; ok {
			continue
		}
		timelinesByThread[tid] = s.uiRuntime.ThreadTimeline(tid)
		diffTextByThread[tid] = s.uiRuntime.ThreadDiff(tid)
	}

	result := map[string]any{
		"threads":               snapshot.Threads,
		"statuses":              snapshot.Statuses,
		"interruptibleByThread": snapshot.InterruptibleByThread,
		"statusHeadersByThread": snapshot.StatusHeadersByThread,
		"statusDetailsByThread": snapshot.StatusDetailsByThread,
		"timelinesByThread":     timelinesByThread,
		"diffTextByThread":      diffTextByThread,
		"tokenUsageByThread":    snapshot.TokenUsageByThread,
		"agentMetaById":         snapshot.AgentMetaByID,
		"workspaceRunsByKey":    snapshot.WorkspaceRunsByKey,
		"activeThreadId":        resolvedActiveThreadID,
		"activeCmdThreadId":     resolvedActiveCmdThreadID,
		"mainAgentId":           resolvedMain,
		"activityStatsByThread": snapshot.ActivityStatsByThread,
		"alertsByThread":        snapshot.AlertsByThread,
	}
	agentRuntimeByID := map[string]map[string]any{}
	if s.mgr != nil {
		for _, info := range s.mgr.List() {
			id := strings.TrimSpace(info.ID)
			if id == "" {
				continue
			}
			item := map[string]any{
				"state": string(info.State),
			}
			if port := info.Port; port > 0 {
				item["port"] = port
			}
			if codexThreadID := strings.TrimSpace(info.ThreadID); codexThreadID != "" {
				item["codexThreadId"] = codexThreadID
			}
			agentRuntimeByID[id] = item
		}
	}
	result["agentRuntimeById"] = agentRuntimeByID
	if snapshot.WorkspaceFeatureEnabled != nil {
		result["workspaceFeatureEnabled"] = *snapshot.WorkspaceFeatureEnabled
	}
	if snapshot.WorkspaceLastError != "" {
		result["workspaceLastError"] = snapshot.WorkspaceLastError
	}
	if value, ok := prefs["viewPrefs.chat"]; ok {
		result["viewPrefs.chat"] = value
	}
	if value, ok := prefs["viewPrefs.cmd"]; ok {
		result["viewPrefs.cmd"] = value
	}
	if value, ok := prefs["threadPins.chat"]; ok {
		result["threadPins.chat"] = value
	}

	return result, nil
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

// asPositiveInt 从 any 提取正整数，低于 minVal 返回 0。
func asPositiveInt(value any, minVal int) int {
	var n int
	switch v := value.(type) {
	case float64:
		n = int(v)
	case int:
		n = v
	case int64:
		n = int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			n = int(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			n = i
		}
	default:
		return 0
	}
	if n < minVal {
		return 0
	}
	return n
}

func (s *Server) persistThreadAlias(ctx context.Context, threadID, alias string) error {
	s.threadAliasMu.Lock()
	defer s.threadAliasMu.Unlock()
	return persistThreadAliasPreference(ctx, s.prefManager, threadID, alias)
}

func persistThreadAliasPreference(ctx context.Context, manager *uistate.PreferenceManager, threadID, alias string) error {
	if manager == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}

	value, err := manager.Get(ctx, prefThreadAliases)
	if err != nil {
		return err
	}
	aliases := normalizeThreadAliases(value)
	nextAlias := strings.TrimSpace(alias)
	if nextAlias == "" || nextAlias == id {
		delete(aliases, id)
	} else {
		aliases[id] = nextAlias
	}
	return manager.Set(ctx, prefThreadAliases, aliases)
}

func (s *Server) loadThreadAliases(ctx context.Context) map[string]string {
	if s.prefManager == nil {
		return map[string]string{}
	}
	value, err := s.prefManager.Get(ctx, prefThreadAliases)
	if err != nil {
		logger.Warn("thread aliases: load preference failed", logger.FieldError, err)
		return map[string]string{}
	}
	return normalizeThreadAliases(value)
}

func loadThreadAliasesFromPrefs(prefs map[string]any) map[string]string {
	if prefs == nil {
		return map[string]string{}
	}
	return normalizeThreadAliases(prefs[prefThreadAliases])
}

func normalizeThreadAliases(value any) map[string]string {
	aliases := map[string]string{}
	addAlias := func(threadID string, alias any) {
		id := strings.TrimSpace(threadID)
		if id == "" {
			return
		}
		name := strings.TrimSpace(asString(alias))
		if name == "" || name == id {
			return
		}
		aliases[id] = name
	}

	switch typed := value.(type) {
	case map[string]string:
		for threadID, alias := range typed {
			addAlias(threadID, alias)
		}
	case map[string]any:
		for threadID, alias := range typed {
			addAlias(threadID, alias)
		}
	case string:
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &decoded); err == nil {
			for threadID, alias := range decoded {
				addAlias(threadID, alias)
			}
		}
	case json.RawMessage:
		decoded := map[string]any{}
		if err := json.Unmarshal(typed, &decoded); err == nil {
			for threadID, alias := range decoded {
				addAlias(threadID, alias)
			}
		}
	}

	return aliases
}

func applyThreadAliases(threads []threadListItem, aliases map[string]string) {
	if len(threads) == 0 || len(aliases) == 0 {
		return
	}
	for i := range threads {
		id := strings.TrimSpace(threads[i].ID)
		if id == "" {
			continue
		}
		alias := strings.TrimSpace(aliases[id])
		if alias == "" {
			continue
		}
		threads[i].Name = alias
	}
}

func applyThreadAliasesSnapshot(snapshot *uistate.RuntimeSnapshot, aliases map[string]string) {
	if snapshot == nil || len(snapshot.Threads) == 0 || len(aliases) == 0 {
		return
	}
	for i := range snapshot.Threads {
		id := strings.TrimSpace(snapshot.Threads[i].ID)
		if id == "" {
			continue
		}
		alias := strings.TrimSpace(aliases[id])
		if alias == "" {
			continue
		}
		snapshot.Threads[i].Name = alias
		meta := snapshot.AgentMetaByID[id]
		meta.Alias = alias
		snapshot.AgentMetaByID[id] = meta
	}
}

func persistResolvedUIPreference(ctx context.Context, manager *uistate.PreferenceManager, key, resolved string, original any) {
	if manager == nil {
		return
	}
	if resolved == asString(original) {
		return
	}
	if err := manager.Set(ctx, key, resolved); err != nil {
		logger.Warn("ui/state/get: persist resolved preference failed",
			logger.FieldKey, key,
			logger.FieldError, err,
		)
	}
}

func resolveMainAgentPreference(snapshot uistate.RuntimeSnapshot, prefs map[string]any) string {
	preferred := strings.TrimSpace(asString(prefs["mainAgentId"]))
	if hasThread(snapshot.Threads, preferred) {
		return preferred
	}

	for _, thread := range snapshot.Threads {
		id := strings.TrimSpace(thread.ID)
		if id == "" {
			continue
		}
		meta := snapshot.AgentMetaByID[id]
		if meta.IsMain {
			return id
		}
	}

	for _, thread := range snapshot.Threads {
		id := strings.TrimSpace(thread.ID)
		if id == "" {
			continue
		}
		meta := snapshot.AgentMetaByID[id]
		if looksLikeMainAgent(thread.Name) || looksLikeMainAgent(meta.Alias) {
			return id
		}
	}
	return ""
}

func resolvePreferredThreadID(threads []uistate.ThreadSnapshot, preferred string) string {
	id := strings.TrimSpace(preferred)
	if hasThread(threads, id) {
		return id
	}
	return firstThreadID(threads)
}

func resolvePreferredCmdThreadID(threads []uistate.ThreadSnapshot, mainAgentID, preferred string) string {
	mainID := strings.TrimSpace(mainAgentID)
	candidates := make([]uistate.ThreadSnapshot, 0, len(threads))
	for _, thread := range threads {
		id := strings.TrimSpace(thread.ID)
		if id == "" {
			continue
		}
		if mainID != "" && id == mainID {
			continue
		}
		candidates = append(candidates, thread)
	}
	if len(candidates) == 0 {
		candidates = threads
	}
	return resolvePreferredThreadID(candidates, preferred)
}

func hasThread(threads []uistate.ThreadSnapshot, id string) bool {
	target := strings.TrimSpace(id)
	if target == "" {
		return false
	}
	for _, thread := range threads {
		if strings.TrimSpace(thread.ID) == target {
			return true
		}
	}
	return false
}

func firstThreadID(threads []uistate.ThreadSnapshot) string {
	for _, thread := range threads {
		id := strings.TrimSpace(thread.ID)
		if id != "" {
			return id
		}
	}
	return ""
}

func looksLikeMainAgent(name string) bool {
	value := strings.ToLower(strings.TrimSpace(name))
	if value == "" {
		return false
	}
	return strings.Contains(value, "主agent") ||
		strings.Contains(value, "主 agent") ||
		strings.Contains(value, "main agent") ||
		value == "main"
}

// ========================================
// Debug 运行时诊断
// ========================================

func (s *Server) debugRuntime(_ context.Context, _ json.RawMessage) (any, error) {
	var mem goruntime.MemStats
	goruntime.ReadMemStats(&mem)

	result := map[string]any{
		"go": map[string]any{
			"goroutines":     goruntime.NumGoroutine(),
			"heapAllocMB":    float64(mem.HeapAlloc) / 1024 / 1024,
			"heapSysMB":      float64(mem.HeapSys) / 1024 / 1024,
			"heapInuseMB":    float64(mem.HeapInuse) / 1024 / 1024,
			"heapObjects":    mem.HeapObjects,
			"sysMB":          float64(mem.Sys) / 1024 / 1024,
			"gcCycles":       mem.NumGC,
			"gcTotalPauseMs": float64(mem.PauseTotalNs) / 1e6,
			"gcLastPauseMs":  float64(mem.PauseNs[(mem.NumGC+255)%256]) / 1e6,
			"stackInuseMB":   float64(mem.StackInuse) / 1024 / 1024,
			"mallocs":        mem.Mallocs,
			"frees":          mem.Frees,
			"liveObjects":    mem.Mallocs - mem.Frees,
			"nextGCMB":       float64(mem.NextGC) / 1024 / 1024,
			"gcCPUPercent":   mem.GCCPUFraction * 100,
		},
	}

	if s.uiRuntime != nil {
		result["timeline"] = s.uiRuntime.TimelineStats()
	}

	return result, nil
}

func (s *Server) debugForceGC(_ context.Context, _ json.RawMessage) (any, error) {
	var before goruntime.MemStats
	goruntime.ReadMemStats(&before)

	goruntime.GC()

	var after goruntime.MemStats
	goruntime.ReadMemStats(&after)

	return map[string]any{
		"before": map[string]any{
			"heapAllocMB": float64(before.HeapAlloc) / 1024 / 1024,
			"heapObjects": before.HeapObjects,
			"liveObjects": before.Mallocs - before.Frees,
		},
		"after": map[string]any{
			"heapAllocMB": float64(after.HeapAlloc) / 1024 / 1024,
			"heapObjects": after.HeapObjects,
			"liveObjects": after.Mallocs - after.Frees,
		},
		"freedMB":      float64(before.HeapAlloc-after.HeapAlloc) / 1024 / 1024,
		"freedObjects": int64(before.HeapObjects) - int64(after.HeapObjects),
		"gcCycles":     after.NumGC,
	}, nil
}

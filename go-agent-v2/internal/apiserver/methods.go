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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	s.methods["thread/messages"] = typedHandler(s.threadMessagesTyped)
	s.methods["thread/backgroundTerminals/clean"] = s.threadBgTerminalsClean

	// § 3. 对话控制 (4 methods)
	s.methods["turn/start"] = typedHandler(s.turnStartTyped)
	s.methods["turn/steer"] = typedHandler(s.turnSteerTyped)
	s.methods["turn/interrupt"] = s.turnInterrupt
	s.methods["review/start"] = typedHandler(s.reviewStartTyped)

	// § 4. 文件搜索 (4 methods)
	s.methods["fuzzyFileSearch"] = typedHandler(s.fuzzyFileSearchTyped)
	s.methods["fuzzyFileSearch/sessionStart"] = noop
	s.methods["fuzzyFileSearch/sessionUpdate"] = noop
	s.methods["fuzzyFileSearch/sessionStop"] = noop

	// § 5. Skills / Apps (5 methods)
	s.methods["skills/list"] = s.skillsList
	s.methods["skills/remote/read"] = typedHandler(s.skillsRemoteReadTyped)
	s.methods["skills/remote/write"] = typedHandler(s.skillsRemoteWriteTyped)
	s.methods["skills/config/write"] = typedHandler(s.skillsConfigWriteTyped)
	s.methods["app/list"] = s.appList

	// § 6. 模型 / 配置 (7 methods)
	s.methods["model/list"] = s.modelList
	s.methods["collaborationMode/list"] = s.collaborationModeList
	s.methods["experimentalFeature/list"] = s.experimentalFeatureList
	s.methods["config/read"] = s.configRead
	s.methods["config/value/write"] = typedHandler(s.configValueWriteTyped)
	s.methods["config/batchWrite"] = typedHandler(s.configBatchWriteTyped)
	s.methods["configRequirements/read"] = s.configRequirementsRead

	// § 7. 账号 (5 methods)
	s.methods["account/login/start"] = typedHandler(s.accountLoginStartTyped)
	s.methods["account/login/cancel"] = noop
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
		resumeThreadID := strings.TrimSpace(p.ThreadID)
		if !isLikelyCodexThreadID(resumeThreadID) {
			if resolved := s.resolveHistoricalCodexThreadID(ctx, p.ThreadID); resolved != "" {
				resumeThreadID = resolved
			}
		}
		if !isLikelyCodexThreadID(resumeThreadID) {
			return nil, apperrors.Newf("Server.threadResume", "unable to resolve codex thread id for %s", p.ThreadID)
		}
		if err := proc.Client.ResumeThread(codex.ResumeThreadRequest{
			ThreadID: resumeThreadID,
			Path:     p.Path,
			Cwd:      p.Cwd,
		}); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadResume", "resume thread")
		}
		return threadResumeResponse{
			Thread: threadInfo{ID: p.ThreadID, Status: "resumed"},
			Model:  p.Model, // model is optional in request, reflect back if needed or empty
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

func (s *Server) threadNameSetTyped(_ context.Context, p threadNameSetParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand("/rename", p.Name); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "send rename command")
		}
		if s.uiRuntime != nil {
			s.uiRuntime.SetThreadName(p.ThreadID, p.Name)
		}
		return map[string]any{}, nil
	})
}

func (s *Server) threadCompact(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(params, "/compact")
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

func (s *Server) threadList(ctx context.Context, _ json.RawMessage) (any, error) {
	agents := s.mgr.List()

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

	// DB 历史兜底: 重启后 mgr 为空时, 仍可从 agent_messages 恢复会话列表。
	if s.msgStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		historyThreads, err := s.msgStore.ListDistinctAgentIDs(dbCtx, 0)
		if err != nil {
			logger.Warn("thread/list: load history threads failed", logger.FieldError, err)
		} else {
			for _, t := range historyThreads {
				if t.AgentID == "" {
					continue
				}
				if _, ok := seen[t.AgentID]; ok {
					continue
				}
				threads = append(threads, threadListItem{
					ID:    t.AgentID,
					Name:  t.AgentID,
					State: "idle",
				})
				seen[t.AgentID] = struct{}{}
			}
		}
	}
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
	agents := s.mgr.List()
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

	if s.msgStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		historyThreads, err := s.msgStore.ListDistinctAgentIDs(dbCtx, 0)
		if err != nil {
			logger.Warn("thread/loaded/list: load history threads failed", logger.FieldError, err)
		} else {
			for _, t := range historyThreads {
				if t.AgentID == "" {
					continue
				}
				if _, ok := seen[t.AgentID]; ok {
					continue
				}
				threads = append(threads, threadListItem{
					ID:    t.AgentID,
					Name:  t.AgentID,
					State: "idle",
				})
				seen[t.AgentID] = struct{}{}
			}
		}
	}

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

// threadMessagesParams thread/messages 请求参数。
type threadMessagesParams struct {
	ThreadID string `json:"threadId"`
	Limit    int    `json:"limit,omitempty"`
	Before   int64  `json:"before,omitempty"` // cursor: id < before
}

func (s *Server) threadMessagesTyped(ctx context.Context, p threadMessagesParams) (any, error) {
	if p.ThreadID == "" {
		return nil, apperrors.New("Server.threadMessages", "threadId is required")
	}

	if s.msgStore == nil {
		return map[string]any{"messages": []any{}, "total": 0}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	msgs, err := s.msgStore.ListByAgent(ctx, p.ThreadID, p.Limit, p.Before)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadMessages", "list messages by agent")
	}
	if s.uiRuntime != nil {
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
		s.uiRuntime.HydrateHistory(p.ThreadID, records)
	}

	total, _ := s.msgStore.CountByAgent(ctx, p.ThreadID)

	return map[string]any{
		"messages": msgs,
		"total":    total,
	}, nil
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
	ThreadID       string          `json:"threadId"`
	Input          []UserInput     `json:"input"`
	Cwd            string          `json:"cwd,omitempty"`
	ApprovalPolicy string          `json:"approvalPolicy,omitempty"`
	Model          string          `json:"model,omitempty"`
	OutputSchema   json.RawMessage `json:"outputSchema,omitempty"`
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

func (s *Server) turnStartTyped(ctx context.Context, p turnStartParams) (any, error) {
	proc, err := s.ensureThreadReadyForTurn(ctx, p.ThreadID, p.Cwd)
	if err != nil {
		return nil, err
	}

	prompt, images, files := extractInputs(p.Input)
	if err := proc.Client.Submit(prompt, images, files, p.OutputSchema); err != nil {
		return nil, apperrors.Wrap(err, "Server.turnStart", "submit prompt")
	}
	if s.uiRuntime != nil {
		s.uiRuntime.AppendUserMessage(p.ThreadID, prompt, nil)
	}

	// 持久化用户消息
	util.SafeGo(func() { s.PersistUserMessage(p.ThreadID, prompt) })

	turnID := fmt.Sprintf("turn-%d", time.Now().UnixMilli())
	return turnStartResponse{
		Turn: turnInfo{ID: turnID, Status: "started"},
	}, nil
}

type turnSteerParams struct {
	ThreadID string      `json:"threadId"`
	Input    []UserInput `json:"input"`
}

func (s *Server) turnSteerTyped(_ context.Context, p turnSteerParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		prompt, images, files := extractInputs(p.Input)
		if err := proc.Client.Submit(prompt, images, files, nil); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

func (s *Server) turnInterrupt(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(params, "/interrupt")
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

// ========================================
// skills, apps, model, config, mcp
// ========================================

func (s *Server) skillsList(_ context.Context, _ json.RawMessage) (any, error) {
	var skills []map[string]string
	skillsDir := filepath.Join(".", ".agent", "skills")
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

func (s *Server) skillsConfigWriteTyped(_ context.Context, p skillsConfigWriteParams) (any, error) {
	// 模式 2: 为指定 agent/session 配置技能列表
	if p.AgentID != "" && len(p.Skills) > 0 {
		s.skillsMu.Lock()
		s.agentSkills[p.AgentID] = p.Skills
		s.skillsMu.Unlock()
		logger.Info("skills/config/write: agent skills configured",
			logger.FieldAgentID, p.AgentID, "skills", p.Skills)
		return map[string]any{"ok": true, "agent_id": p.AgentID, "skills": p.Skills}, nil
	}

	// 模式 1: 写 SKILL.md 文件
	if p.Name == "" {
		return nil, apperrors.New("Server.skillsConfigWrite", "name or agent_id is required")
	}
	if strings.ContainsAny(p.Name, "/\\") || strings.Contains(p.Name, "..") {
		return nil, apperrors.Newf("Server.skillsConfigWrite", "invalid skill name %q", p.Name)
	}
	dir := filepath.Join(".", ".agent", "skills", p.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "mkdir")
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "write SKILL.md")
	}
	logger.Info("skills/config/write: saved", logger.FieldSkill, p.Name, logger.FieldBytes, len(p.Content))
	return map[string]any{"ok": true, "path": path}, nil
}

// GetAgentSkills 返回指定 agent 配置的技能列表。
func (s *Server) GetAgentSkills(agentID string) []string {
	s.skillsMu.RLock()
	defer s.skillsMu.RUnlock()
	return s.agentSkills[agentID]
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
	if s.msgStore == nil {
		return false
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}

	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	count, err := s.msgStore.CountByAgent(dbCtx, id)
	if err != nil {
		logger.Warn("turn/start: check thread history failed",
			logger.FieldThreadID, id,
			logger.FieldError, err,
		)
		return false
	}
	return count > 0
}

func isLikelyCodexThreadID(raw string) bool {
	id := strings.TrimSpace(raw)
	if id == "" {
		return false
	}
	id = strings.TrimPrefix(strings.ToLower(id), "urn:uuid:")
	return codexThreadIDPattern.MatchString(id)
}

func metadataThreadID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}

	candidates := []string{
		nestedString(payload, "thread", "id"),
		nestedString(payload, "threadId"),
		nestedString(payload, "thread_id"),
		nestedString(payload, "conversationId"),
		nestedString(payload, "conversation_id"),
		nestedString(payload, "sessionId"),
		nestedString(payload, "session_id"),
	}
	for _, candidate := range candidates {
		if isLikelyCodexThreadID(candidate) {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

func nestedString(root map[string]any, path ...string) string {
	if len(path) == 0 {
		return ""
	}
	var current any = root
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		next, ok := obj[key]
		if !ok {
			return ""
		}
		current = next
	}
	value, ok := current.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func isMeaningfulSessionMessage(msg store.AgentMessage) bool {
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	if role == "user" || role == "assistant" {
		return true
	}

	eventType := strings.ToLower(strings.TrimSpace(msg.EventType))
	method := strings.ToLower(strings.TrimSpace(msg.Method))
	switch {
	case eventType == codex.EventTurnComplete,
		eventType == codex.EventAgentMessage,
		eventType == codex.EventAgentMessageDelta,
		eventType == codex.EventAgentMessageCompleted,
		method == "turn/completed",
		method == "item/agentmessage/delta":
		return true
	}
	return false
}

func resolveResumeThreadIDFromMessages(messages []store.AgentMessage) string {
	type sessionCandidate struct {
		threadID   string
		meaningful bool
	}

	sessions := make([]sessionCandidate, 0, 8)
	current := sessionCandidate{}
	flush := func() {
		if current.threadID == "" {
			return
		}
		sessions = append(sessions, current)
	}

	// ListByAgent 返回倒序；这里改为时间正序遍历。
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.EqualFold(msg.EventType, codex.EventSessionConfigured) || strings.EqualFold(msg.Method, "thread/started") {
			flush()
			current = sessionCandidate{
				threadID: metadataThreadID(msg.Metadata),
			}
			continue
		}
		if current.threadID == "" {
			continue
		}
		if isMeaningfulSessionMessage(msg) {
			current.meaningful = true
		}
	}
	flush()

	for i := len(sessions) - 1; i >= 0; i-- {
		if sessions[i].meaningful {
			return sessions[i].threadID
		}
	}
	if len(sessions) > 0 {
		return sessions[len(sessions)-1].threadID
	}

	// 回退：无明确会话边界时，直接扫描任意 metadata 中的可用线程 ID。
	for _, msg := range messages {
		if tid := metadataThreadID(msg.Metadata); tid != "" {
			return tid
		}
	}
	return ""
}

func (s *Server) resolveHistoricalCodexThreadID(ctx context.Context, agentID string) string {
	if s.msgStore == nil {
		return ""
	}
	id := strings.TrimSpace(agentID)
	if id == "" {
		return ""
	}

	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	msgs, err := s.msgStore.ListByAgent(dbCtx, id, 500, 0)
	if err != nil {
		logger.Warn("turn/start: resolve historical codex thread id failed",
			logger.FieldThreadID, id,
			logger.FieldError, err,
		)
		return ""
	}
	return resolveResumeThreadIDFromMessages(msgs)
}

func (s *Server) ensureThreadReadyForTurn(ctx context.Context, threadID, cwd string) (*runner.AgentProcess, error) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New("Server.ensureThreadReady", "threadId is required")
	}

	if proc := s.mgr.Get(id); proc != nil {
		return proc, nil
	}
	if !s.threadExistsInHistory(ctx, id) {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s not found", id)
	}
	resumeThreadID := id
	if !isLikelyCodexThreadID(resumeThreadID) {
		if resolved := s.resolveHistoricalCodexThreadID(ctx, id); resolved != "" {
			resumeThreadID = resolved
		}
	}

	launchCwd := strings.TrimSpace(cwd)
	if launchCwd == "" {
		launchCwd = "."
	}
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
	if !isLikelyCodexThreadID(resumeThreadID) {
		logger.Warn("turn/start: no valid historical codex thread id, continue with fresh session",
			logger.FieldThreadID, id,
		)
		return proc, nil
	}
	if err := proc.Client.ResumeThread(codex.ResumeThreadRequest{
		ThreadID: resumeThreadID,
		Cwd:      launchCwd,
	}); err != nil {
		if stopErr := s.mgr.Stop(id); stopErr != nil {
			logger.Warn("turn/start: auto-loaded thread stop after resume failed",
				logger.FieldThreadID, id,
				logger.FieldError, stopErr,
			)
		}
		return nil, apperrors.Wrapf(err, "Server.ensureThreadReady", "resume historical thread %s", id)
	}

	logger.Info("turn/start: historical thread auto-loaded",
		logger.FieldThreadID, id,
		"resume_thread_id", resumeThreadID,
		logger.FieldCwd, launchCwd,
	)
	return proc, nil
}

// sendSlashCommand 通用斜杠命令发送 (compact, interrupt 等)。
func (s *Server) sendSlashCommand(params json.RawMessage, command string) (any, error) {
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.sendSlashCommand", "unmarshal params")
	}
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand(command, ""); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

// sendSlashCommandWithArgs 带参数的斜杠命令。
func (s *Server) sendSlashCommandWithArgs(params json.RawMessage, command string, argsField string) (any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(params, &raw); err != nil {
		return nil, apperrors.Wrap(err, "Server.sendSlashCommandWithArgs", "unmarshal params")
	}

	var threadID string
	if v, ok := raw["threadId"]; ok {
		_ = json.Unmarshal(v, &threadID)
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
	for _, inp := range inputs {
		switch inp.Type {
		case "text":
			texts = append(texts, inp.Text)
		case "image":
			images = append(images, inp.URL)
		case "localImage":
			images = append(images, inp.Path)
		case "fileContent", "mention":
			files = append(files, inp.Path)
		case "skill":
			texts = append(texts, fmt.Sprintf("[skill:%s] %s", inp.Name, inp.Content))
		}
	}
	prompt = strings.Join(texts, "\n")
	return
}

// ========================================
// 补全方法 (§ 1–9 需实现项)
// ========================================

// threadBgTerminalsClean 清理后台终端 (experimental)。
func (s *Server) threadBgTerminalsClean(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(params, "/clean")
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
	skillsDir := filepath.Join(".", ".agent", "skills", p.Name)
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
func (s *Server) threadUndo(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(params, "/undo")
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
func (s *Server) threadMCPList(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(params, "/mcp")
}

// threadSkillsList 列出 Skills (/skills)。
func (s *Server) threadSkillsList(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(params, "/skills")
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
	return map[string]any{"ok": true}, nil
}

func (s *Server) uiPreferencesGetAll(ctx context.Context, _ json.RawMessage) (any, error) {
	return s.prefManager.GetAll(ctx)
}

func (s *Server) uiStateGet(ctx context.Context, _ json.RawMessage) (any, error) {
	if s.uiRuntime == nil {
		return map[string]any{}, nil
	}
	snapshot := s.uiRuntime.Snapshot()
	prefs := map[string]any{}
	if s.prefManager != nil {
		loaded, err := s.prefManager.GetAll(ctx)
		if err != nil {
			logger.Warn("ui/state/get: load preferences failed", logger.FieldError, err)
		} else {
			prefs = loaded
		}
	}

	resolvedMain := resolveMainAgentPreference(snapshot, prefs)
	if resolvedMain != asString(prefs["mainAgentId"]) {
		s.uiRuntime.SetMainAgent(resolvedMain)
		snapshot = s.uiRuntime.Snapshot()
		persistResolvedUIPreference(ctx, s.prefManager, "mainAgentId", resolvedMain, prefs["mainAgentId"])
		prefs["mainAgentId"] = resolvedMain
	}

	resolvedActiveThreadID := resolvePreferredThreadID(snapshot.Threads, asString(prefs["activeThreadId"]))
	persistResolvedUIPreference(ctx, s.prefManager, "activeThreadId", resolvedActiveThreadID, prefs["activeThreadId"])
	prefs["activeThreadId"] = resolvedActiveThreadID

	resolvedActiveCmdThreadID := resolvePreferredCmdThreadID(snapshot.Threads, resolvedMain, asString(prefs["activeCmdThreadId"]))
	persistResolvedUIPreference(ctx, s.prefManager, "activeCmdThreadId", resolvedActiveCmdThreadID, prefs["activeCmdThreadId"])
	prefs["activeCmdThreadId"] = resolvedActiveCmdThreadID

	result := map[string]any{
		"threads":            snapshot.Threads,
		"statuses":           snapshot.Statuses,
		"timelinesByThread":  snapshot.TimelinesByThread,
		"diffTextByThread":   snapshot.DiffTextByThread,
		"agentMetaById":      snapshot.AgentMetaByID,
		"workspaceRunsByKey": snapshot.WorkspaceRunsByKey,
		"activeThreadId":     resolvedActiveThreadID,
		"activeCmdThreadId":  resolvedActiveCmdThreadID,
		"mainAgentId":        resolvedMain,
	}
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

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
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

// registerMethods 注册所有 JSON-RPC 方法 (完整对标 APP-SERVER-PROTOCOL.md)。
func (s *Server) registerMethods() {
	noop := noopHandler()

	// § 1. 初始化
	s.methods["initialize"] = s.initialize
	s.methods["initialized"] = noop

	// § 2. 线程生命周期 (12 methods)
	s.methods["thread/start"] = s.threadStart
	s.methods["thread/resume"] = typedHandler(s.threadResumeTyped)
	s.methods["thread/fork"] = s.threadFork
	s.methods["thread/archive"] = noop
	s.methods["thread/unarchive"] = noop
	s.methods["thread/name/set"] = s.threadNameSet
	s.methods["thread/compact/start"] = s.threadCompact
	s.methods["thread/rollback"] = s.threadRollback
	s.methods["thread/list"] = s.threadList
	s.methods["thread/loaded/list"] = s.threadLoadedList
	s.methods["thread/read"] = typedHandler(s.threadReadTyped)
	s.methods["thread/messages"] = typedHandler(s.threadMessagesTyped)
	s.methods["thread/backgroundTerminals/clean"] = s.threadBgTerminalsClean

	// § 3. 对话控制 (4 methods)
	s.methods["turn/start"] = s.turnStart
	s.methods["turn/steer"] = s.turnSteer
	s.methods["turn/interrupt"] = s.turnInterrupt
	s.methods["review/start"] = s.reviewStart

	// § 4. 文件搜索 (4 methods)
	s.methods["fuzzyFileSearch"] = typedHandler(s.fuzzyFileSearchTyped)
	s.methods["fuzzyFileSearch/sessionStart"] = noop
	s.methods["fuzzyFileSearch/sessionUpdate"] = noop
	s.methods["fuzzyFileSearch/sessionStop"] = noop

	// § 5. Skills / Apps (5 methods)
	s.methods["skills/list"] = s.skillsList
	s.methods["skills/remote/read"] = s.skillsRemoteRead
	s.methods["skills/remote/write"] = s.skillsRemoteWrite
	s.methods["skills/config/write"] = s.skillsConfigWrite
	s.methods["app/list"] = s.appList

	// § 6. 模型 / 配置 (7 methods)
	s.methods["model/list"] = s.modelList
	s.methods["collaborationMode/list"] = s.collaborationModeList
	s.methods["experimentalFeature/list"] = s.experimentalFeatureList
	s.methods["config/read"] = s.configRead
	s.methods["config/value/write"] = typedHandler(s.configValueWriteTyped)
	s.methods["config/batchWrite"] = s.configBatchWrite
	s.methods["configRequirements/read"] = s.configRequirementsRead

	// § 7. 账号 (5 methods)
	s.methods["account/login/start"] = s.accountLoginStart
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
	s.methods["command/exec"] = s.commandExec
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
	s.methods["log/list"] = s.logList
	s.methods["log/filters"] = s.logFilters

	// § 12. Dashboard 数据查询 (12 methods, 替代 Wails Dashboard 绑定)
	s.methods["dashboard/agentStatus"] = s.dashAgentStatus
	s.methods["dashboard/dags"] = s.dashDAGs
	s.methods["dashboard/dagDetail"] = s.dashDAGDetail
	s.methods["dashboard/taskAcks"] = s.dashTaskAcks
	s.methods["dashboard/taskTraces"] = s.dashTaskTraces
	s.methods["dashboard/commandCards"] = s.dashCommandCards
	s.methods["dashboard/prompts"] = s.dashPrompts
	s.methods["dashboard/sharedFiles"] = s.dashSharedFiles
	s.methods["dashboard/skills"] = s.dashSkills
	s.methods["dashboard/auditLogs"] = s.dashAuditLogs
	s.methods["dashboard/aiLogs"] = s.dashAILogs
	s.methods["dashboard/busLogs"] = s.dashBusLogs

	// § 13. Workspace Run (双通道编排: 虚拟目录 + PG 状态)
	s.methods["workspace/run/create"] = s.workspaceRunCreate
	s.methods["workspace/run/get"] = s.workspaceRunGet
	s.methods["workspace/run/list"] = s.workspaceRunList
	s.methods["workspace/run/merge"] = s.workspaceRunMerge
	s.methods["workspace/run/abort"] = s.workspaceRunAbort
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
		_ = json.Unmarshal(params, &p)
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

func (s *Server) threadStart(ctx context.Context, params json.RawMessage) (any, error) {
	var p threadStartParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.Cwd == "" {
		p.Cwd = "."
	}

	id := fmt.Sprintf("thread-%d-%d", time.Now().UnixMilli(), s.threadSeq.Add(1))

	// 构建全部动态工具注入 agent (LSP + 编排 + 资源)
	dynamicTools := s.buildAllDynamicTools()

	if err := s.mgr.Launch(ctx, id, id, "", p.Cwd, dynamicTools); err != nil {
		return nil, fmt.Errorf("thread/start: %w", err)
	}

	return map[string]any{
		"thread": map[string]any{
			"id":     id,
			"status": "running",
		},
		"model":          p.Model,
		"modelProvider":  p.ModelProvider,
		"cwd":            p.Cwd,
		"approvalPolicy": p.ApprovalPolicy,
	}, nil
}

// threadResumeParams thread/resume 请求参数。
type threadResumeParams struct {
	ThreadID string `json:"threadId"`
	Path     string `json:"path"`
	Model    string `json:"model,omitempty"`
}

func (s *Server) threadResumeTyped(ctx context.Context, p threadResumeParams) (any, error) {
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.ResumeThread(codex.ResumeThreadRequest{
			ThreadID: p.ThreadID,
			Cwd:      p.Path,
		}); err != nil {
			return nil, fmt.Errorf("thread/resume: %w", err)
		}
		return map[string]any{
			"thread": map[string]any{"id": p.ThreadID, "status": "resumed"},
			"model":  p.Model,
		}, nil
	})
}

type threadIDParams struct {
	ThreadID string `json:"threadId"`
}

func (s *Server) threadFork(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		ThreadID  string `json:"threadId"`
		TurnIndex *int   `json:"turnIndex,omitempty"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		resp, err := proc.Client.ForkThread(codex.ForkThreadRequest{
			SourceThreadID: p.ThreadID,
		})
		if err != nil {
			return nil, fmt.Errorf("thread/fork: %w", err)
		}
		newID := resp.ThreadID
		if newID == "" {
			newID = fmt.Sprintf("thread-%d", time.Now().UnixMilli())
		}
		return map[string]any{
			"thread": map[string]any{"id": newID, "forkedFrom": p.ThreadID},
		}, nil
	})
}

func (s *Server) threadNameSet(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		ThreadID string `json:"threadId"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand("/rename", p.Name); err != nil {
			return nil, fmt.Errorf("thread/name/set: %w", err)
		}
		return map[string]any{}, nil
	})
}

func (s *Server) threadCompact(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(params, "/compact")
}

func (s *Server) threadRollback(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		ThreadID  string `json:"threadId"`
		TurnIndex int    `json:"turnIndex"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand("/undo", fmt.Sprintf("%d", p.TurnIndex)); err != nil {
			return nil, fmt.Errorf("thread/rollback: %w", err)
		}
		return map[string]any{}, nil
	})
}

func (s *Server) threadList(ctx context.Context, _ json.RawMessage) (any, error) {
	agents := s.mgr.List()

	threads := make([]map[string]any, 0, len(agents)+32)
	seen := make(map[string]struct{}, len(agents)+32)

	for _, a := range agents {
		if a.ID == "" {
			continue
		}
		threads = append(threads, map[string]any{
			"id":    a.ID,
			"name":  a.Name,
			"state": string(a.State),
		})
		seen[a.ID] = struct{}{}
	}

	// DB 历史兜底: 重启后 mgr 为空时, 仍可从 agent_messages 恢复会话列表。
	if s.msgStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		rows, err := s.msgStore.Pool().Query(dbCtx,
			`SELECT agent_id, MAX(created_at) AS last_at
			 FROM agent_messages
			 GROUP BY agent_id
			 ORDER BY last_at DESC
			 LIMIT 500`)
		if err != nil {
			slog.Warn("thread/list: load history threads failed", "error", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var agentID string
				var lastAt time.Time
				if err := rows.Scan(&agentID, &lastAt); err != nil {
					slog.Warn("thread/list: scan history thread failed", "error", err)
					continue
				}
				if agentID == "" {
					continue
				}
				if _, ok := seen[agentID]; ok {
					continue
				}

				threads = append(threads, map[string]any{
					"id":    agentID,
					"name":  agentID,
					"state": "idle",
				})
				seen[agentID] = struct{}{}
			}
			if err := rows.Err(); err != nil {
				slog.Warn("thread/list: iterate history threads failed", "error", err)
			}
		}
	}

	return map[string]any{"threads": threads}, nil
}

func (s *Server) threadLoadedList(ctx context.Context, _ json.RawMessage) (any, error) {
	// 仅返回当前加载/运行中的线程。
	agents := s.mgr.List()
	threads := make([]map[string]any, 0, len(agents))
	for _, a := range agents {
		if a.ID == "" {
			continue
		}
		threads = append(threads, map[string]any{
			"id":    a.ID,
			"name":  a.Name,
			"state": string(a.State),
		})
	}
	return map[string]any{"threads": threads}, nil
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
		return nil, fmt.Errorf("threadId is required")
	}

	if s.msgStore == nil {
		return map[string]any{"messages": []any{}, "total": 0}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	msgs, err := s.msgStore.ListByAgent(ctx, p.ThreadID, p.Limit, p.Before)
	if err != nil {
		return nil, fmt.Errorf("thread/messages: %w", err)
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

func (s *Server) turnStart(_ context.Context, params json.RawMessage) (any, error) {
	var p turnStartParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		prompt, images, files := extractInputs(p.Input)
		if err := proc.Client.Submit(prompt, images, files, p.OutputSchema); err != nil {
			return nil, fmt.Errorf("turn/start: %w", err)
		}

		// 持久化用户消息
		go s.PersistUserMessage(p.ThreadID, prompt)

		turnID := fmt.Sprintf("turn-%d", time.Now().UnixMilli())
		return map[string]any{
			"turn": map[string]any{"id": turnID, "status": "started"},
		}, nil
	})
}

type turnSteerParams struct {
	ThreadID string      `json:"threadId"`
	Input    []UserInput `json:"input"`
}

func (s *Server) turnSteer(_ context.Context, params json.RawMessage) (any, error) {
	var p turnSteerParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
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

func (s *Server) reviewStart(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		ThreadID string `json:"threadId"`
		Delivery string `json:"delivery,omitempty"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return s.withThread(p.ThreadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand("/review", p.Delivery); err != nil {
			return nil, fmt.Errorf("review/start: %w", err)
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
		return nil, fmt.Errorf("config/value/write: key %q not in allowlist", p.Key)
	}
	if err := os.Setenv(p.Key, p.Value); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (s *Server) configBatchWrite(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Entries []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	var rejected []string
	for _, e := range p.Entries {
		if !isAllowedEnvKey(e.Key) {
			rejected = append(rejected, e.Key)
			continue
		}
		_ = os.Setenv(e.Key, e.Value)
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
	slog.Info("mcpServer/reload: all language servers restarted")
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

// skillsConfigWrite 写入 Skill 配置 (JSON-RPC: skills/config/write)。
//
// 两种模式:
//  1. 写入 SKILL.md 文件: {"name": "skill_name", "content": "..."}
//  2. 为会话配置技能列表: {"agent_id": "thread-xxx", "skills": ["s1", "s2"]}
func (s *Server) skillsConfigWrite(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		// 模式 1: 写文件
		Name    string `json:"name"`
		Content string `json:"content"`
		// 模式 2: per-session 技能配置
		AgentID string   `json:"agent_id"`
		Skills  []string `json:"skills"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// 模式 2: 为指定 agent/session 配置技能列表
	if p.AgentID != "" && len(p.Skills) > 0 {
		s.skillsMu.Lock()
		s.agentSkills[p.AgentID] = p.Skills
		s.skillsMu.Unlock()
		slog.Info("skills/config/write: agent skills configured",
			"agent_id", p.AgentID, "skills", p.Skills)
		return map[string]any{"ok": true, "agent_id": p.AgentID, "skills": p.Skills}, nil
	}

	// 模式 1: 写 SKILL.md 文件
	if p.Name == "" {
		return nil, fmt.Errorf("skills/config/write: name or agent_id is required")
	}
	if strings.ContainsAny(p.Name, "/\\") || strings.Contains(p.Name, "..") {
		return nil, fmt.Errorf("skills/config/write: invalid skill name %q", p.Name)
	}
	dir := filepath.Join(".", ".agent", "skills", p.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("skills/config/write: mkdir: %w", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
		return nil, fmt.Errorf("skills/config/write: write: %w", err)
	}
	slog.Info("skills/config/write: saved", "skill", p.Name, "bytes", len(p.Content))
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

func (s *Server) commandExec(ctx context.Context, params json.RawMessage) (any, error) {
	var p commandExecParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if len(p.Argv) == 0 {
		return nil, fmt.Errorf("argv is required")
	}

	// 安全检查: 提取基础命令名 (去掉路径)
	baseName := filepath.Base(p.Argv[0])
	if commandBlocklist[baseName] {
		return nil, fmt.Errorf("command/exec: %q is blocked for security", baseName)
	}

	// 禁止管道/shell 注入: 检查参数中是否有 shell 元字符
	for _, arg := range p.Argv {
		if strings.ContainsAny(arg, "|;&$`") {
			return nil, fmt.Errorf("command/exec: shell metacharacters not allowed in arguments")
		}
	}

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
	cmd.Stdout = &limitedWriter{w: &stdout, limit: maxOutputSize}
	cmd.Stderr = &limitedWriter{w: &stderr, limit: maxOutputSize}

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("command/exec: %w", err)
		}
	}

	return map[string]any{
		"exitCode": exitCode,
		"stdout":   stdout.String(),
		"stderr":   stderr.String(),
	}, nil
}

// limitedWriter 限制写入字节数, 超出后静默丢弃。
type limitedWriter struct {
	w       *strings.Builder
	limit   int
	written int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	remain := lw.limit - lw.written
	if remain <= 0 {
		return len(p), nil // 静默丢弃
	}
	if len(p) > remain {
		p = p[:remain]
	}
	n, err := lw.w.Write(p)
	lw.written += n
	return n, err
}

// ========================================
// account
// ========================================

func (s *Server) accountLoginStart(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		AuthMode string `json:"authMode"`
		APIKey   string `json:"apiKey,omitempty"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.APIKey != "" {
		_ = os.Setenv("OPENAI_API_KEY", p.APIKey)
		return map[string]any{}, nil
	}
	return map[string]any{"loginUrl": "https://platform.openai.com/api-keys"}, nil
}

func (s *Server) accountLoginCancel(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{}, nil
}

func (s *Server) accountLogout(_ context.Context, _ json.RawMessage) (any, error) {
	_ = os.Unsetenv("OPENAI_API_KEY")
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
		return nil, fmt.Errorf("thread %s not found", threadID)
	}
	return fn(proc)
}

// sendSlashCommand 通用斜杠命令发送 (compact, interrupt 等)。
func (s *Server) sendSlashCommand(params json.RawMessage, command string) (any, error) {
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
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
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	var threadID string
	if v, ok := raw["threadId"]; ok {
		_ = json.Unmarshal(v, &threadID)
	}
	if threadID == "" {
		return nil, fmt.Errorf("threadId is required")
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

// skillsRemoteRead 读取远程 Skill。
func (s *Server) skillsRemoteRead(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(p.URL)
	if err != nil {
		return nil, fmt.Errorf("skills/remote/read: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("skills/remote/read: %w", err)
	}
	return map[string]any{
		"skill": map[string]string{"url": p.URL, "content": string(body)},
	}, nil
}

// skillsRemoteWrite 写入远程 Skill 到本地。
func (s *Server) skillsRemoteWrite(_ context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
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

// logList 查询系统日志 (JSON-RPC: log/list)。
func (s *Server) logList(ctx context.Context, params json.RawMessage) (any, error) {
	if s.sysLogStore == nil {
		return nil, fmt.Errorf("log store not initialized")
	}
	var req struct {
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
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Limit <= 0 || req.Limit > 2000 {
		req.Limit = 100
	}
	return s.sysLogStore.ListV2(ctx, store.ListParams{
		Level:     req.Level,
		Logger:    req.Logger,
		Source:    req.Source,
		Component: req.Component,
		AgentID:   req.AgentID,
		ThreadID:  req.ThreadID,
		EventType: req.EventType,
		ToolName:  req.ToolName,
		Keyword:   req.Keyword,
		Limit:     req.Limit,
	})
}

// logFilters 返回日志筛选器可选值 (JSON-RPC: log/filters)。
func (s *Server) logFilters(ctx context.Context, _ json.RawMessage) (any, error) {
	if s.sysLogStore == nil {
		return nil, fmt.Errorf("log store not initialized")
	}
	return s.sysLogStore.ListFilterValues(ctx)
}

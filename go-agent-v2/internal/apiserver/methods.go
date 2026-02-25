package apiserver

import (
	"context"
	"encoding/json"
	"os"
	"runtime"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	// DB-only: 无内置默认提示词，未配置 settings.lspUsagePromptHint 时不注入。
	defaultLSPUsagePromptHint       = ""
	prefKeyLSPUsagePromptHint       = "settings.lspUsagePromptHint"
	prefKeyShowInjectedPromptInChat = "settings.showInjectedPromptInChat"
	maxLSPUsagePromptHintLen        = 16000
)

func bindRaw(s *Server, fn func(*Server, context.Context, json.RawMessage) (any, error)) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		return fn(s, ctx, params)
	}
}

func bindTyped[P any](s *Server, fn func(*Server, context.Context, P) (any, error)) Handler {
	return typedHandler(func(ctx context.Context, p P) (any, error) {
		return fn(s, ctx, p)
	})
}

// registerMethods 注册所有 JSON-RPC 方法 (完整对标 APP-SERVER-PROTOCOL.md)。
func (s *Server) registerMethods() {
	noop := noopHandler()

	s.methods["initialize"] = bindRaw(s, initialize)
	s.methods["initialized"] = noop

	s.methods["thread/start"] = typedHandler(s.threadStartTyped)
	s.methods["thread/resume"] = typedHandler(s.threadResumeTyped)
	s.methods["thread/recover"] = typedHandler(s.threadRecoverTyped)
	s.methods["thread/fork"] = typedHandler(s.threadForkTyped)
	s.methods["thread/archive"] = typedHandler(s.threadArchiveTyped)
	s.methods["thread/unarchive"] = typedHandler(s.threadUnarchiveTyped)
	s.methods["thread/name/set"] = typedHandler(s.threadNameSetTyped)
	s.methods["thread/compact/start"] = s.threadCompact
	s.methods["thread/rollback"] = typedHandler(s.threadRollbackTyped)
	s.methods["thread/list"] = s.threadList
	s.methods["thread/loaded/list"] = s.threadLoadedList
	s.methods["thread/read"] = typedHandler(s.threadReadTyped)
	s.methods["thread/resolve"] = typedHandler(s.threadResolveTyped)
	s.methods["thread/messages"] = typedHandler(s.threadMessagesTyped)
	s.methods["thread/backgroundTerminals/clean"] = s.threadBgTerminalsClean

	s.methods["turn/start"] = typedHandler(s.turnStartTyped)
	s.methods["turn/steer"] = typedHandler(s.turnSteerTyped)
	s.methods["turn/interrupt"] = typedHandler(s.turnInterrupt)
	s.methods["turn/forceComplete"] = typedHandler(s.turnForceComplete)
	s.methods["review/start"] = typedHandler(s.reviewStartTyped)

	s.methods["fuzzyFileSearch"] = bindTyped(s, fuzzyFileSearchTyped)
	s.methods["fuzzyFileSearch/sessionStart"] = noop
	s.methods["fuzzyFileSearch/sessionUpdate"] = noop
	s.methods["fuzzyFileSearch/sessionStop"] = noop

	s.methods["skills/list"] = bindRaw(s, skillsList)
	s.methods["skills/local/read"] = bindTyped(s, skillsLocalReadTyped)
	s.methods["skills/local/importDir"] = bindTyped(s, skillsLocalImportDirTyped)
	s.methods["skills/local/delete"] = bindTyped(s, skillsLocalDeleteTyped)
	s.methods["skills/remote/read"] = bindTyped(s, skillsRemoteReadTyped)
	s.methods["skills/remote/write"] = bindTyped(s, skillsRemoteWriteTyped)
	s.methods["skills/config/read"] = bindTyped(s, skillsConfigReadTyped)
	s.methods["skills/config/write"] = bindTyped(s, skillsConfigWriteTyped)
	s.methods["skills/summary/write"] = bindTyped(s, skillsSummaryWriteTyped)
	s.methods["skills/match/preview"] = bindTyped(s, skillsMatchPreviewTyped)
	s.methods["app/list"] = bindRaw(s, appList)

	s.methods["model/list"] = bindRaw(s, modelList)
	s.methods["collaborationMode/list"] = bindRaw(s, collaborationModeList)
	s.methods["experimentalFeature/list"] = bindRaw(s, experimentalFeatureList)
	s.methods["config/read"] = bindRaw(s, configRead)
	s.methods["config/value/write"] = bindTyped(s, configValueWriteTyped)
	s.methods["config/batchWrite"] = bindTyped(s, configBatchWriteTyped)
	s.methods["config/lspPromptHint/read"] = bindRaw(s, configLSPPromptHintRead)
	s.methods["config/lspPromptHint/write"] = bindTyped(s, configLSPPromptHintWriteTyped)
	s.methods["configRequirements/read"] = bindRaw(s, configRequirementsRead)

	s.methods["account/login/start"] = bindTyped(s, accountLoginStartTyped)
	s.methods["account/login/cancel"] = bindRaw(s, accountLoginCancel)
	s.methods["account/logout"] = bindRaw(s, accountLogout)
	s.methods["account/read"] = bindRaw(s, accountRead)
	s.methods["account/rateLimits/read"] = bindRaw(s, accountRateLimitsRead)

	s.methods["mcpServer/oauth/login"] = noop
	s.methods["config/mcpServer/reload"] = bindRaw(s, mcpServerReload)
	s.methods["mcpServerStatus/list"] = bindRaw(s, mcpServerStatusList)
	s.methods["lsp_diagnostics_query"] = typedHandler(s.lspDiagnosticsQueryTyped)

	s.methods["command/exec"] = bindTyped(s, commandExecTyped)
	s.methods["approval/respond"] = bindTyped(s, approvalRespondTyped)
	s.methods["feedback/upload"] = noop

	s.methods["thread/undo"] = s.threadUndo
	s.methods["thread/model/set"] = s.threadModelSet
	s.methods["thread/personality/set"] = s.threadPersonality
	s.methods["thread/approvals/set"] = s.threadApprovals
	s.methods["thread/mcp/list"] = s.threadMCPList
	s.methods["thread/skills/list"] = s.threadSkillsList
	s.methods["thread/debugMemory"] = s.threadDebugMemory

	s.methods["log/list"] = bindTyped(s, logListTyped)
	s.methods["log/filters"] = bindRaw(s, logFilters)

	registerDashboardMethods(s)

	s.methods["workspace/run/create"] = bindRaw(s, workspaceRunCreate)
	s.methods["workspace/run/get"] = bindRaw(s, workspaceRunGet)
	s.methods["workspace/run/list"] = bindRaw(s, workspaceRunList)
	s.methods["workspace/run/merge"] = bindRaw(s, workspaceRunMerge)
	s.methods["workspace/run/abort"] = bindRaw(s, workspaceRunAbort)

	s.methods["ui/preferences/get"] = bindTyped(s, uiPreferencesGet)
	s.methods["ui/preferences/set"] = bindTyped(s, uiPreferencesSet)
	s.methods["ui/preferences/getAll"] = bindRaw(s, uiPreferencesGetAll)
	s.methods["ui/projects/get"] = bindRaw(s, uiProjectsGet)
	s.methods["ui/projects/add"] = bindTyped(s, uiProjectsAdd)
	s.methods["ui/projects/remove"] = bindTyped(s, uiProjectsRemove)
	s.methods["ui/projects/setActive"] = bindTyped(s, uiProjectsSetActive)
	s.methods["ui/code/open"] = bindTyped(s, uiCodeOpenTyped)
	s.methods["ui/dashboard/get"] = bindTyped(s, uiDashboardGet)
	s.methods["ui/state/get"] = bindRaw(s, uiStateGet)

	s.methods["debug/runtime"] = bindRaw(s, debugRuntime)
	s.methods["debug/gc"] = bindRaw(s, debugForceGC)

	s.methods["workspace-root-options"] = stubHandler(map[string]any{"roots": []any{}, "labels": map[string]any{}})
	s.methods["codex-home"] = stubHandler(map[string]any{"codexHome": ""})
	s.methods["git-origins"] = stubHandler(map[string]any{"origins": []any{}})
	s.methods["mcp-servers"] = stubHandler(map[string]any{"servers": []any{}})
	s.methods["platform-info"] = stubHandler(map[string]any{"platform": runtime.GOOS, "arch": runtime.GOARCH})
	s.methods["open-in-targets"] = stubHandler(map[string]any{"targets": []any{}})
	s.methods["codex-agents-md"] = stubHandler(map[string]any{})
	s.methods["local-environments/list"] = stubHandler(map[string]any{"environments": []any{}})
	s.methods["worktrees/list"] = stubHandler(map[string]any{"worktrees": []any{}})
	s.methods["tasks/list"] = stubHandler([]any{})
	s.methods["tasks/get"] = stubHandler(map[string]any{})
	s.methods["inbox-items"] = stubHandler(map[string]any{"items": []any{}})
	s.methods["inbox-items/get"] = stubHandler(map[string]any{})
	s.methods["pending-automation-runs"] = stubHandler(map[string]any{"runs": []any{}})
	s.methods["mcp/status"] = stubHandler(map[string]any{})
	s.methods["config/read-all"] = stubHandler(map[string]any{})
	s.methods["diff/get"] = stubHandler(map[string]any{})

	if s.cfg != nil && s.cfg.DisableOffline52Methods {
		for _, method := range offline52MethodList() {
			delete(s.methods, method)
		}
	}
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	ClientInfo      any    `json:"clientInfo,omitempty"`
	Capabilities    any    `json:"capabilities,omitempty"`
}

func initialize(_ *Server, _ context.Context, params json.RawMessage) (any, error) {
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

// accountLoginStartParams account/login/start 请求参数。
type accountLoginStartParams struct {
	AuthMode string `json:"authMode"`
	APIKey   string `json:"apiKey,omitempty"`
}

func accountLoginStartTyped(_ *Server, _ context.Context, p accountLoginStartParams) (any, error) {
	if p.APIKey != "" {
		if err := os.Setenv("OPENAI_API_KEY", p.APIKey); err != nil {
			logger.Warn("account/login: setenv failed", logger.FieldError, err)
			return nil, apperrors.Wrap(err, "Server.accountLoginStart", "setenv OPENAI_API_KEY")
		}
		return map[string]any{}, nil
	}
	return map[string]any{"loginUrl": "https://platform.openai.com/api-keys"}, nil
}

func accountLoginCancel(_ *Server, _ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{}, nil
}

func accountLogout(_ *Server, _ context.Context, _ json.RawMessage) (any, error) {
	if err := os.Unsetenv("OPENAI_API_KEY"); err != nil {
		logger.Warn("account/logout: unsetenv failed", logger.FieldError, err)
	}
	return map[string]any{}, nil
}

func accountRead(_ *Server, _ context.Context, _ json.RawMessage) (any, error) {
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

// accountRateLimitsRead 读取速率限制。
func accountRateLimitsRead(_ *Server, _ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"rateLimits": map[string]any{}}, nil
}
func offline52MethodList() []string {
	return []string{
		"initialize",
		"thread/resume",
		"thread/fork",
		"thread/rollback",
		"thread/loaded/list",
		"thread/read",
		"thread/resolve",
		"thread/backgroundTerminals/clean",
		"turn/steer",
		"turn/forceComplete",
		"review/start",
		"fuzzyFileSearch",
		"skills/list",
		"skills/remote/read",
		"skills/remote/write",
		"app/list",
		"model/list",
		"collaborationMode/list",
		"experimentalFeature/list",
		"config/read",
		"config/value/write",
		"config/batchWrite",
		"configRequirements/read",
		"account/login/start",
		"account/login/cancel",
		"account/logout",
		"account/read",
		"account/rateLimits/read",
		"config/mcpServer/reload",
		"mcpServerStatus/list",
		"lsp_diagnostics_query",
		"command/exec",
		"thread/undo",
		"thread/model/set",
		"thread/personality/set",
		"thread/approvals/set",
		"thread/mcp/list",
		"thread/skills/list",
		"thread/debugMemory",
		"log/list",
		"log/filters",
		"workspace/run/create",
		"workspace/run/get",
		"workspace/run/list",
		"workspace/run/merge",
		"workspace/run/abort",
		"ui/preferences/getAll",
		"ui/projects/add",
		"ui/projects/remove",
		"debug/runtime",
		"debug/gc",
	}
}

// threadBgTerminalsClean 清理后台终端 (experimental)。
func (s *Server) threadBgTerminalsClean(ctx context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, "/clean")
}

// threadUndo 撤销上一步 (/undo)。
func (s *Server) threadUndo(ctx context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, "/undo")
}

// threadModelSet 切换模型 (/model <name>)。
func (s *Server) threadModelSet(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandWithArgs(params, "/model", "model")
}

// threadPersonality 设置人格 (/personality <type>)。
func (s *Server) threadPersonality(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandWithArgs(params, "/personality", "personality")
}

// threadApprovals 设置审批策略 (/approvals <policy>)。
func (s *Server) threadApprovals(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandWithArgs(params, "/approvals", "policy")
}

// threadMCPList 列出 MCP 工具 (/mcp)。
func (s *Server) threadMCPList(ctx context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, "/mcp")
}

// threadSkillsList 列出 Skills（统一走本地 SkillService 缓存，不透传外部 /skills）。
func (s *Server) threadSkillsList(_ context.Context, _ json.RawMessage) (any, error) {
	return s.codexAdapter.ThreadSkillsList()
}

// threadDebugMemory 调试记忆 (/debug-m-drop 或 /debug-m-update)。
func (s *Server) threadDebugMemory(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandWithArgs(params, "/debug-m-drop", "action")
}

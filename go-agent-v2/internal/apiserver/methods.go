package apiserver

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	defaultLSPUsagePromptHint       = ""
	prefKeyLSPUsagePromptHint       = "settings.lspUsagePromptHint"
	prefKeyShowInjectedPromptInChat = "settings.showInjectedPromptInChat"
	maxLSPUsagePromptHintLen        = 16000
)

func bindRaw(s *Server, fn func(*Server, context.Context, json.RawMessage) (any, error)) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) { return fn(s, ctx, params) }
}

func bindTyped[P any](s *Server, fn func(*Server, context.Context, P) (any, error)) Handler {
	return typedHandler(func(ctx context.Context, p P) (any, error) { return fn(s, ctx, p) })
}

func (s *Server) registerMethods() {
	noop := noopHandler()
	registerNoop := func(methods ...string) {
		for _, method := range methods {
			s.methods[method] = noop
		}
	}

	s.methods["initialize"] = bindRaw(s, initialize)
	registerNoop("initialized")

	s.methods["thread/start"] = typedHandler(s.threadStartTyped)
	s.methods["thread/resume"] = typedHandler(s.threadResumeTyped)
	s.methods["thread/recover"] = typedHandler(s.threadRecoverTyped)
	s.methods["thread/fork"] = typedHandler(s.threadForkTyped)
	s.methods["thread/archive"] = typedHandler(func(ctx context.Context, p threadIDParams) (any, error) {
		return s.codexAdapter.ThreadArchive(ctx, p.ThreadID)
	})
	s.methods["thread/unarchive"] = typedHandler(func(ctx context.Context, p threadIDParams) (any, error) {
		return s.codexAdapter.ThreadUnarchive(ctx, p.ThreadID)
	})
	s.methods["thread/name/set"] = typedHandler(func(ctx context.Context, p threadNameSetParams) (any, error) {
		return s.codexAdapter.ThreadNameSet(ctx, p.ThreadID, p.Name)
	})
	s.methods["thread/compact/start"] = func(ctx context.Context, params json.RawMessage) (any, error) {
		return s.codexAdapter.SendSlashCommandFromRawParamsRequireThreadID(ctx, params, "/compact")
	}
	s.methods["thread/rollback"] = typedHandler(s.threadRollbackTyped)
	s.methods["thread/list"] = s.threadList
	s.methods["thread/loaded/list"] = s.threadLoadedList
	s.methods["thread/read"] = typedHandler(func(ctx context.Context, p threadIDParams) (any, error) {
		return s.codexAdapter.ThreadRead(ctx, p.ThreadID)
	})
	s.methods["thread/resolve"] = typedHandler(func(ctx context.Context, p threadIDParams) (any, error) {
		return s.codexAdapter.ThreadResolve(ctx, p.ThreadID)
	})
	s.methods["thread/messages"] = typedHandler(func(ctx context.Context, p threadMessagesParams) (any, error) {
		return s.codexAdapter.ThreadMessages(ctx, p.ThreadID, p.Limit, p.Before)
	})
	s.methods["thread/backgroundTerminals/clean"] = func(ctx context.Context, params json.RawMessage) (any, error) {
		return s.codexAdapter.SendSlashCommandFromRawParamsRequireThreadID(ctx, params, "/clean")
	}

	s.methods["turn/start"] = typedHandler(s.turnStartTyped)
	s.methods["turn/steer"] = typedHandler(s.turnSteerTyped)
	s.methods["turn/interrupt"] = typedHandler(func(_ context.Context, p turnInterruptParams) (any, error) {
		return s.codexAdapter.TurnInterrupt(p.ThreadID)
	})
	s.methods["turn/forceComplete"] = typedHandler(func(_ context.Context, p turnForceCompleteParams) (any, error) {
		return s.codexAdapter.TurnForceComplete(p.ThreadID)
	})
	s.methods["thread/realtime/start"] = typedHandler(func(_ context.Context, p threadRealtimeStartParams) (any, error) {
		return s.codexAdapter.ThreadRealtimeStart(p.ThreadID, p.Prompt, p.SessionID)
	})
	s.methods["thread/realtime/appendAudio"] = typedHandler(func(_ context.Context, p threadRealtimeAppendAudioParams) (any, error) {
		return s.codexAdapter.ThreadRealtimeAppendAudio(p.ThreadID, p.Audio)
	})
	s.methods["thread/realtime/appendText"] = typedHandler(func(_ context.Context, p threadRealtimeAppendTextParams) (any, error) {
		return s.codexAdapter.ThreadRealtimeAppendText(p.ThreadID, p.Text)
	})
	s.methods["thread/realtime/stop"] = typedHandler(func(_ context.Context, p threadRealtimeStopParams) (any, error) {
		return s.codexAdapter.ThreadRealtimeStop(p.ThreadID)
	})
	s.methods["review/start"] = typedHandler(s.reviewStartTyped)

	s.methods["fuzzyFileSearch"] = bindTyped(s, fuzzyFileSearchTyped)
	registerNoop(
		"fuzzyFileSearch/sessionStart",
		"fuzzyFileSearch/sessionUpdate",
		"fuzzyFileSearch/sessionStop",
	)

	s.methods["skills/list"] = bindRaw(s, skillsList)
	s.methods["skills/local/read"] = bindTyped(s, skillsLocalReadTyped)
	s.methods["skills/local/importDir"] = bindTyped(s, skillsLocalImportDirTyped)
	s.methods["skills/local/delete"] = bindTyped(s, skillsLocalDeleteTyped)
	s.methods["skills/remote/list"] = bindTyped(s, skillsRemoteReadTyped)
	s.methods["skills/remote/export"] = bindTyped(s, skillsRemoteWriteTyped)
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
	s.methods["externalAgentConfig/detect"] = stubHandler(map[string]any{})
	s.methods["externalAgentConfig/import"] = stubHandler(map[string]any{})
	s.methods["config/value/write"] = bindTyped(s, configValueWriteTyped)
	s.methods["config/batchWrite"] = bindTyped(s, configBatchWriteTyped)
	s.methods["config/lspPromptHint/read"] = bindRaw(s, configLSPPromptHintRead)
	s.methods["config/lspPromptHint/write"] = bindTyped(s, configLSPPromptHintWriteTyped)
	s.methods["configRequirements/read"] = bindRaw(s, configRequirementsRead)

	s.methods["account/login/start"] = bindTyped(s, accountLoginStartTyped)
	s.methods["account/login/cancel"] = stubHandler(map[string]any{})
	s.methods["account/logout"] = bindRaw(s, accountLogout)
	s.methods["account/read"] = bindRaw(s, accountRead)
	s.methods["account/rateLimits/read"] = stubHandler(map[string]any{"rateLimits": map[string]any{}})

	registerNoop("mcpServer/oauth/login")
	s.methods["config/mcpServer/reload"] = bindRaw(s, mcpServerReload)
	s.methods["mcpServerStatus/list"] = bindRaw(s, mcpServerStatusList)
	s.methods["windowsSandbox/setupStart"] = stubHandler(map[string]any{})
	s.methods["lsp_diagnostics_query"] = typedHandler(s.lspDiagnosticsQueryTyped)

	s.methods["command/exec"] = bindTyped(s, commandExecTyped)
	s.methods["approval/respond"] = bindTyped(s, approvalRespondTyped)
	registerNoop("feedback/upload")

	s.methods["thread/undo"] = func(ctx context.Context, params json.RawMessage) (any, error) {
		return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, "/undo")
	}
	s.methods["thread/model/set"] = func(_ context.Context, params json.RawMessage) (any, error) {
		return s.codexAdapter.SendSlashCommandWithArgs(params, "/model", "model")
	}
	s.methods["thread/personality/set"] = func(_ context.Context, params json.RawMessage) (any, error) {
		return s.codexAdapter.SendSlashCommandWithArgs(params, "/personality", "personality")
	}
	s.methods["thread/approvals/set"] = func(_ context.Context, params json.RawMessage) (any, error) {
		return s.codexAdapter.SendSlashCommandWithArgs(params, "/approvals", "policy")
	}
	s.methods["thread/mcp/list"] = func(ctx context.Context, params json.RawMessage) (any, error) {
		return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, "/mcp")
	}
	s.methods["thread/skills/list"] = func(_ context.Context, _ json.RawMessage) (any, error) { return s.codexAdapter.ThreadSkillsList() }
	s.methods["thread/debugMemory"] = s.threadDebugMemory
	s.methods["mock/experimentalMethod"] = stubHandler(map[string]any{})

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

	for method, payload := range map[string]any{
		"workspace-root-options":  map[string]any{"roots": []any{}, "labels": map[string]any{}},
		"codex-home":              map[string]any{"codexHome": ""},
		"git-origins":             map[string]any{"origins": []any{}},
		"mcp-servers":             map[string]any{"servers": []any{}},
		"platform-info":           map[string]any{"platform": runtime.GOOS, "arch": runtime.GOARCH},
		"open-in-targets":         map[string]any{"targets": []any{}},
		"codex-agents-md":         map[string]any{},
		"local-environments/list": map[string]any{"environments": []any{}},
		"worktrees/list":          map[string]any{"worktrees": []any{}},
		"tasks/list":              []any{},
		"tasks/get":               map[string]any{},
		"inbox-items":             map[string]any{"items": []any{}},
		"inbox-items/get":         map[string]any{},
		"pending-automation-runs": map[string]any{"runs": []any{}},
		"mcp/status":              map[string]any{},
		"config/read-all":         map[string]any{},
		"diff/get":                map[string]any{},
	} {
		s.methods[method] = stubHandler(payload)
	}

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

func offline52MethodList() []string {
	return []string{"initialize", "thread/resume", "thread/fork", "thread/rollback", "thread/loaded/list", "thread/read", "thread/resolve", "thread/backgroundTerminals/clean", "thread/realtime/start", "thread/realtime/appendAudio", "thread/realtime/appendText", "thread/realtime/stop", "turn/steer", "turn/forceComplete", "review/start", "fuzzyFileSearch", "skills/list", "skills/remote/list", "skills/remote/export", "skills/remote/read", "skills/remote/write", "app/list", "model/list", "collaborationMode/list", "experimentalFeature/list", "config/read", "externalAgentConfig/detect", "externalAgentConfig/import", "config/value/write", "config/batchWrite", "configRequirements/read", "account/login/start", "account/login/cancel", "account/logout", "account/read", "account/rateLimits/read", "config/mcpServer/reload", "mcpServerStatus/list", "windowsSandbox/setupStart", "lsp_diagnostics_query", "command/exec", "thread/undo", "thread/model/set", "thread/personality/set", "thread/approvals/set", "thread/mcp/list", "thread/skills/list", "thread/debugMemory", "mock/experimentalMethod", "log/list", "log/filters", "workspace/run/create", "workspace/run/get", "workspace/run/list", "workspace/run/merge", "workspace/run/abort", "ui/preferences/getAll", "ui/projects/add", "ui/projects/remove", "debug/runtime", "debug/gc"}
}

type threadDebugMemoryParams struct {
	Action string `json:"action,omitempty"`
}

func (s *Server) threadDebugMemory(ctx context.Context, params json.RawMessage) (any, error) {
	command := "/debug-m-drop"
	if len(params) > 0 && string(params) != "null" {
		var p threadDebugMemoryParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadDebugMemory", "invalid params")
		}
		switch strings.ToLower(strings.TrimSpace(p.Action)) {
		case "", "drop":
			command = "/debug-m-drop"
		case "update":
			command = "/debug-m-update"
		default:
			return nil, apperrors.New("Server.threadDebugMemory", "action must be one of: drop, update")
		}
	}
	return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, command)
}

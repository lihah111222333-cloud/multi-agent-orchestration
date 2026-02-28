package apiserver

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/store"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func modelList(_ *Server, _ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"models": []map[string]string{
		{"id": "o4-mini", "name": "O4 Mini"},
		{"id": "o3", "name": "O3"},
		{"id": "gpt-4.1", "name": "GPT-4.1"},
		{"id": "codex-mini", "name": "Codex Mini"},
	}}, nil
}

func configRead(s *Server, _ context.Context, _ json.RawMessage) (any, error) {
	model := "o4-mini"
	if s.cfg != nil && s.cfg.LLMModel != "" {
		model = s.cfg.LLMModel
	}
	toolRoutingMode := "legacy"
	toolRouterProvider := "openai_compatible"
	toolRouterModel := ""
	toolRouterBaseURL := ""
	toolRouterConfidenceThreshold := 0.65
	toolRouterTimeoutSec := 8
	toolRouterHasAPIKey := false
	if s.cfg != nil {
		if strings.TrimSpace(s.cfg.DynToolRoutingMode) != "" {
			toolRoutingMode = strings.TrimSpace(s.cfg.DynToolRoutingMode)
		}
		if strings.TrimSpace(s.cfg.DynToolRouterProvider) != "" {
			toolRouterProvider = strings.TrimSpace(s.cfg.DynToolRouterProvider)
		}
		toolRouterModel = strings.TrimSpace(s.cfg.DynToolRouterModel)
		toolRouterBaseURL = strings.TrimSpace(s.cfg.DynToolRouterBaseURL)
		toolRouterConfidenceThreshold = s.cfg.DynToolRouterConfidenceThreshold
		if toolRouterConfidenceThreshold <= 0 {
			toolRouterConfidenceThreshold = 0.65
		}
		toolRouterTimeoutSec = s.cfg.DynToolRouterTimeoutSec
		if toolRouterTimeoutSec <= 0 {
			toolRouterTimeoutSec = 8
		}
		toolRouterHasAPIKey = strings.TrimSpace(s.cfg.DynToolRouterAPIKey) != ""
	}
	cwd, _ := os.Getwd()
	return map[string]any{
		"model":                 model,
		"modelProvider":         nil,
		"cwd":                   cwd,
		"approvalPolicy":        "on-failure",
		"sandbox":               nil,
		"config":                nil,
		"baseInstructions":      nil,
		"developerInstructions": nil,
		"personality":           nil,
		"toolRouting": map[string]any{
			"mode":                toolRoutingMode,
			"routerModel":         toolRouterModel,
			"routerProvider":      toolRouterProvider,
			"routerBaseURL":       toolRouterBaseURL,
			"routerHasAPIKey":     toolRouterHasAPIKey,
			"confidenceThreshold": toolRouterConfidenceThreshold,
			"timeoutSec":          toolRouterTimeoutSec,
		},
	}, nil
}

var configEnvAllowPrefixes = []string{
	"OPENAI_",
	"ANTHROPIC_",
	"CODEX_",
	"DYN_TOOL_",
	"MODEL",
	"LOG_LEVEL",
	"AGENT_",
	"MCP_",
	"APP_",
	"STRESS_TEST_", // 测试用
	"TEST_E2E_",    // 测试用
}

func isAllowedEnvKey(key string) bool {
	for _, prefix := range configEnvAllowPrefixes {
		if strings.HasPrefix(strings.ToUpper(key), prefix) {
			return true
		}
	}
	return false
}

type configValueWriteParams struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func configValueWriteTyped(_ *Server, _ context.Context, p configValueWriteParams) (any, error) {
	if !isAllowedEnvKey(p.Key) {
		return nil, apperrors.Newf("Server.configValueWrite", "key %q not in allowlist", p.Key)
	}
	if err := os.Setenv(p.Key, p.Value); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

type configBatchWriteParams struct {
	Entries []configBatchWriteEntry `json:"entries"`
}

type configBatchWriteEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func configBatchWriteTyped(_ *Server, _ context.Context, p configBatchWriteParams) (any, error) {
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
	if len(rejected) == 0 { return map[string]any{}, nil }
	return map[string]any{"rejected": rejected}, nil
}

func configLSPPromptHintRead(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	overrideHint := ""
	usingDefault := true
	if s.prefManager != nil {
		value, err := s.prefManager.Get(ctx, prefKeyLSPUsagePromptHint)
		if err != nil {
			logger.Warn("config/lspPromptHint/read: load override failed", logger.FieldError, err)
		} else {
			overrideHint = strings.TrimSpace(asString(value))
			usingDefault = overrideHint == ""
		}
	}
	return map[string]any{
		"hint":            resolveLSPUsagePromptHint(s, ctx),
		"defaultHint":     defaultLSPUsagePromptHint,
		"overrideHint":    overrideHint,
		"usingDefault":    usingDefault,
		"prefKey":         prefKeyLSPUsagePromptHint,
		"lspAvailability": s.lspTools.AvailabilitySummary(),
	}, nil
}

type configLSPPromptHintWriteParams struct {
	Hint string `json:"hint"`
}

func validateLSPUsagePromptHint(hint string) error {
	if len(hint) > maxLSPUsagePromptHintLen {
		return apperrors.Newf("Server.configLSPPromptHintWrite", "hint length exceeds %d", maxLSPUsagePromptHintLen)
	}
	return nil
}

func resolveLSPUsagePromptHint(s *Server, ctx context.Context) string {
	if s != nil && s.codexAdapter != nil {
		return s.codexAdapter.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen)
	}
	return defaultLSPUsagePromptHint
}

func configLSPPromptHintWriteTyped(s *Server, ctx context.Context, p configLSPPromptHintWriteParams) (any, error) {
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
		"hint":         resolveLSPUsagePromptHint(s, ctx),
		"defaultHint":  defaultLSPUsagePromptHint,
		"overrideHint": normalized,
		"usingDefault": normalized == "",
	}, nil
}

func mcpServerStatusList(s *Server, _ context.Context, _ json.RawMessage) (any, error) {
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

func mcpServerReload(s *Server, _ context.Context, _ json.RawMessage) (any, error) {
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

func collaborationModeList(_ *Server, _ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"modes": []map[string]string{
		{"id": "default", "name": "Default"},
		{"id": "pair", "name": "Pair Programming"},
	}}, nil
}

func experimentalFeatureList(_ *Server, _ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"features": map[string]bool{
		"backgroundTerminals": true,
		"collaborationMode":   true,
		"fuzzySearchSession":  true,
	}}, nil
}

func configRequirementsRead(_ *Server, _ context.Context, _ json.RawMessage) (any, error) {
	routerModel := strings.TrimSpace(os.Getenv("DYN_TOOL_ROUTER_MODEL"))
	routerBaseURL := strings.TrimSpace(os.Getenv("DYN_TOOL_ROUTER_BASE_URL"))
	return map[string]any{"requirements": map[string]any{
		"apiKey": map[string]string{
			"status":  boolToStatus(os.Getenv("OPENAI_API_KEY") != ""),
			"message": "OPENAI_API_KEY environment variable",
		},
		"toolRouterEndpoint": map[string]string{
			"status":  boolToStatus(routerModel == "" || routerBaseURL != ""),
			"message": "When DYN_TOOL_ROUTER_MODEL is set, DYN_TOOL_ROUTER_BASE_URL is required",
		},
	}}, nil
}

func boolToStatus(ok bool) string {
	if ok { return "met" }
	return "unmet"
}

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

func logListTyped(s *Server, ctx context.Context, p logListParams) (any, error) {
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

func logFilters(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	if s.sysLogStore == nil {
		return nil, apperrors.New("Server.logFilters", "log store not initialized")
	}
	return s.sysLogStore.ListFilterValues(ctx)
}

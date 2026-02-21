// methods_config.go — 配置、模型、MCP、LSP 诊断、日志查询 JSON-RPC 方法。
package apiserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/store"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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
	model := "o4-mini"
	if s.cfg != nil && s.cfg.LLMModel != "" {
		model = s.cfg.LLMModel
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
	}, nil
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

// boolToStatus bool → "met" / "unmet"。
func boolToStatus(ok bool) string {
	if ok {
		return "met"
	}
	return "unmet"
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

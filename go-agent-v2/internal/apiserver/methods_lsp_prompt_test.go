package apiserver

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestConfigLSPPromptHintRead_Default(t *testing.T) {
	srv := &Server{prefManager: uistate.NewPreferenceManager(nil)}
	raw, err := srv.configLSPPromptHintRead(context.Background(), nil)
	if err != nil {
		t.Fatalf("configLSPPromptHintRead error: %v", err)
	}
	resp := raw.(map[string]any)
	if got, _ := resp["hint"].(string); got != defaultLSPUsagePromptHint {
		t.Fatalf("hint = %q, want default", got)
	}
	if got, _ := resp["defaultHint"].(string); got != defaultLSPUsagePromptHint {
		t.Fatalf("defaultHint = %q, want default", got)
	}
	if got, _ := resp["overrideHint"].(string); got != "" {
		t.Fatalf("overrideHint = %q, want empty", got)
	}
	if usingDefault, _ := resp["usingDefault"].(bool); !usingDefault {
		t.Fatalf("usingDefault = %v, want true", usingDefault)
	}
	availability := mustMapValue(t, resp["lspAvailability"], "lspAvailability")
	if hasManager, _ := availability["hasManager"].(bool); hasManager {
		t.Fatalf("hasManager = %v, want false", hasManager)
	}
	if hasAvailable, _ := availability["hasAvailableServer"].(bool); hasAvailable {
		t.Fatalf("hasAvailableServer = %v, want false", hasAvailable)
	}
	if availableCount, _ := availability["availableServerCount"].(int); availableCount != 0 {
		t.Fatalf("availableServerCount = %d, want 0", availableCount)
	}
	if servers := mustServerRows(t, availability["servers"]); len(servers) != 0 {
		t.Fatalf("len(servers) = %d, want 0", len(servers))
	}
}

func TestConfigLSPPromptHintWriteAndRead(t *testing.T) {
	srv := &Server{prefManager: uistate.NewPreferenceManager(nil)}
	custom := "请强制先调用LSP再回答。"

	if _, err := srv.configLSPPromptHintWriteTyped(context.Background(), configLSPPromptHintWriteParams{
		Hint: custom,
	}); err != nil {
		t.Fatalf("configLSPPromptHintWriteTyped error: %v", err)
	}

	if got := srv.resolveLSPUsagePromptHint(context.Background()); got != custom {
		t.Fatalf("resolveLSPUsagePromptHint = %q, want %q", got, custom)
	}

	raw, err := srv.configLSPPromptHintRead(context.Background(), nil)
	if err != nil {
		t.Fatalf("configLSPPromptHintRead error: %v", err)
	}
	resp := raw.(map[string]any)
	if got, _ := resp["hint"].(string); got != custom {
		t.Fatalf("hint = %q, want %q", got, custom)
	}
	if got, _ := resp["overrideHint"].(string); got != custom {
		t.Fatalf("overrideHint = %q, want %q", got, custom)
	}
	if usingDefault, _ := resp["usingDefault"].(bool); usingDefault {
		t.Fatalf("usingDefault = %v, want false", usingDefault)
	}
}

func TestConfigLSPPromptHintWrite_ResetDefault(t *testing.T) {
	srv := &Server{prefManager: uistate.NewPreferenceManager(nil)}

	if _, err := srv.configLSPPromptHintWriteTyped(context.Background(), configLSPPromptHintWriteParams{
		Hint: "临时提示词",
	}); err != nil {
		t.Fatalf("write custom hint error: %v", err)
	}
	raw, err := srv.configLSPPromptHintWriteTyped(context.Background(), configLSPPromptHintWriteParams{
		Hint: "   ",
	})
	if err != nil {
		t.Fatalf("write empty hint error: %v", err)
	}
	resp := raw.(map[string]any)
	if usingDefault, _ := resp["usingDefault"].(bool); !usingDefault {
		t.Fatalf("usingDefault = %v, want true", usingDefault)
	}
	if got, _ := resp["hint"].(string); got != defaultLSPUsagePromptHint {
		t.Fatalf("hint = %q, want default", got)
	}
	if got, _ := resp["overrideHint"].(string); got != "" {
		t.Fatalf("overrideHint = %q, want empty", got)
	}
}

func TestConfigLSPPromptHintWrite_TooLong(t *testing.T) {
	srv := &Server{prefManager: uistate.NewPreferenceManager(nil)}
	longHint := strings.Repeat("a", maxLSPUsagePromptHintLen+1)
	if _, err := srv.configLSPPromptHintWriteTyped(context.Background(), configLSPPromptHintWriteParams{
		Hint: longHint,
	}); err == nil {
		t.Fatal("expected error for overlong hint")
	}
}

func TestConfigLSPPromptHintRead_LSPAvailability_WithManager(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable error: %v", err)
	}
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
		lsp: lsp.NewManager([]lsp.ServerConfig{
			{
				Language:   "unit-test",
				Command:    executable,
				Extensions: []string{"unit_test"},
			},
		}),
	}
	raw, err := srv.configLSPPromptHintRead(context.Background(), nil)
	if err != nil {
		t.Fatalf("configLSPPromptHintRead error: %v", err)
	}
	resp := raw.(map[string]any)
	availability := mustMapValue(t, resp["lspAvailability"], "lspAvailability")
	if hasManager, _ := availability["hasManager"].(bool); !hasManager {
		t.Fatalf("hasManager = %v, want true", hasManager)
	}
	if hasAvailable, _ := availability["hasAvailableServer"].(bool); !hasAvailable {
		t.Fatalf("hasAvailableServer = %v, want true", hasAvailable)
	}
	if availableCount, _ := availability["availableServerCount"].(int); availableCount != 1 {
		t.Fatalf("availableServerCount = %d, want 1", availableCount)
	}
	servers := mustServerRows(t, availability["servers"])
	if len(servers) != 1 {
		t.Fatalf("len(servers) = %d, want 1", len(servers))
	}
	if language, _ := servers[0]["language"].(string); language != "unit-test" {
		t.Fatalf("language = %q, want unit-test", language)
	}
	if available, _ := servers[0]["available"].(bool); !available {
		t.Fatalf("server available = %v, want true", available)
	}
}

func TestConfigLSPPromptHintRead_LSPAvailability_NoAvailableServer(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
		lsp: lsp.NewManager([]lsp.ServerConfig{
			{
				Language:   "missing",
				Command:    "go-agent-v2-definitely-missing-lsp",
				Extensions: []string{"missing_ext"},
			},
		}),
	}
	raw, err := srv.configLSPPromptHintRead(context.Background(), nil)
	if err != nil {
		t.Fatalf("configLSPPromptHintRead error: %v", err)
	}
	resp := raw.(map[string]any)
	availability := mustMapValue(t, resp["lspAvailability"], "lspAvailability")
	if hasManager, _ := availability["hasManager"].(bool); !hasManager {
		t.Fatalf("hasManager = %v, want true", hasManager)
	}
	if hasAvailable, _ := availability["hasAvailableServer"].(bool); hasAvailable {
		t.Fatalf("hasAvailableServer = %v, want false", hasAvailable)
	}
	if availableCount, _ := availability["availableServerCount"].(int); availableCount != 0 {
		t.Fatalf("availableServerCount = %d, want 0", availableCount)
	}
	servers := mustServerRows(t, availability["servers"])
	if len(servers) != 1 {
		t.Fatalf("len(servers) = %d, want 1", len(servers))
	}
	if available, _ := servers[0]["available"].(bool); available {
		t.Fatalf("server available = %v, want false", available)
	}
}

func TestPrependLSPAvailabilityWarning_NoMissingTool(t *testing.T) {
	hint := "先调用 `lsp_open_file` 再分析。"
	instructions, missing := prependLSPAvailabilityWarning(hint, []codex.DynamicTool{
		{Name: "lsp_open_file"},
		{Name: "code_run"},
	})
	if instructions != hint {
		t.Fatalf("instructions changed unexpectedly: %q", instructions)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want empty", missing)
	}
}

func TestPrependLSPAvailabilityWarning_AddWarningWhenMissingTool(t *testing.T) {
	hint := "请先调用 `lsp_hover`、`lsp_definition`，最后再输出。"
	instructions, missing := prependLSPAvailabilityWarning(hint, []codex.DynamicTool{
		{Name: "code_run"},
	})
	wantMissing := []string{"lsp_definition", "lsp_hover"}
	if !reflect.DeepEqual(missing, wantMissing) {
		t.Fatalf("missing = %v, want %v", missing, wantMissing)
	}
	if !strings.Contains(instructions, "当前会话未注入以下 LSP 工具") {
		t.Fatalf("instructions missing warning prefix: %q", instructions)
	}
	if !strings.HasSuffix(instructions, hint) {
		t.Fatalf("instructions should keep original hint suffix, got: %q", instructions)
	}
}

func TestResolveStartInstructionsForLaunch_UsesPreferenceAndValidation(t *testing.T) {
	srv := &Server{prefManager: uistate.NewPreferenceManager(nil)}
	if _, err := srv.configLSPPromptHintWriteTyped(context.Background(), configLSPPromptHintWriteParams{
		Hint: "请优先用 `lsp_hover`。",
	}); err != nil {
		t.Fatalf("configLSPPromptHintWriteTyped error: %v", err)
	}
	instructions := srv.resolveStartInstructionsForLaunch(context.Background(), []codex.DynamicTool{
		{Name: "code_run"},
	})
	if !strings.Contains(instructions, "当前会话未注入以下 LSP 工具") {
		t.Fatalf("instructions should include availability warning: %q", instructions)
	}
	if !strings.Contains(instructions, "`lsp_hover`") {
		t.Fatalf("instructions should include original hint content: %q", instructions)
	}
}

func mustMapValue(t *testing.T, raw any, field string) map[string]any {
	t.Helper()
	value, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("%s type = %T, want map[string]any", field, raw)
	}
	return value
}

func mustServerRows(t *testing.T, raw any) []map[string]any {
	t.Helper()
	rows, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("servers type = %T, want []map[string]any", raw)
	}
	return rows
}

package apiserver

import (
	"context"
	"strings"
	"testing"

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

package apiserver

import (
	"os"
	"strings"
	"testing"
)

func p0PreModeAPIServerRuntime() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("LSP_P0_MODE")))
	if mode == "" {
		return "pre"
	}
	return mode
}

func TestP0PreDynamicToolRuntimeGuard(t *testing.T) {
	if p0PreModeAPIServerRuntime() != "pre" {
		t.Skip("skip p0-pre guard when LSP_P0_MODE is not pre")
	}

	if !shouldCaptureDynamicToolDiff("lsp_did_change", map[string]any{"persist_to_disk": true}) {
		t.Fatalf("p0-pre expects lsp_did_change persist_to_disk=true to capture diff")
	}
	if shouldCaptureDynamicToolDiff("lsp_did_change", map[string]any{"persist_to_disk": false}) {
		t.Fatalf("p0-pre expects lsp_did_change persist_to_disk=false to skip diff")
	}
	if shouldCaptureDynamicToolDiff("lsp_did_change", map[string]any{"action": "change"}) {
		t.Fatalf("p0-pre expects action-only payload not to trigger diff")
	}
	if !shouldCaptureDynamicToolDiff("code_run", map[string]any{}) {
		t.Fatalf("p0-pre expects code_run to capture diff")
	}
	if !shouldCaptureDynamicToolDiff("functions.code_run", map[string]any{}) {
		t.Fatalf("p0-pre expects prefixed code_run to capture diff")
	}
	if shouldCaptureDynamicToolDiff("lsp_file", map[string]any{"action": "change", "persist_to_disk": true}) {
		t.Fatalf("p0-pre expects lsp_file route not to capture diff yet")
	}
}

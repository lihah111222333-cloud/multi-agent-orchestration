package apiserver

import (
	"os"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/tooladapter"
)

func p0PostModeAPIServerRuntime() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("LSP_P0_MODE")))
	if mode == "" {
		return "pre"
	}
	return mode
}

type p0PostEmptyLookup struct{}

func (p0PostEmptyLookup) LookupRuntimeTool(string) (tooladapter.RuntimeToolHandler, bool) {
	return nil, false
}

type p0PostCounter struct{}

func (p0PostCounter) IncrementToolCall(string) int64 { return 1 }

func TestP0PostDynamicToolRuntimeGuard(t *testing.T) {
	if p0PostModeAPIServerRuntime() != "post" {
		t.Skip("skip p0-post guard when LSP_P0_MODE is not post")
	}

	if !shouldCaptureDynamicToolDiff("lsp_file", map[string]any{"action": "did_change", "persist_to_disk": true}) {
		t.Fatalf("p0-post expects lsp_file action=did_change persist_to_disk=true to capture diff")
	}
	if shouldCaptureDynamicToolDiff("lsp_file", map[string]any{"action": "did_change", "persist_to_disk": false}) {
		t.Fatalf("p0-post expects lsp_file action=did_change persist_to_disk=false to skip diff")
	}
	if shouldCaptureDynamicToolDiff("lsp_file", map[string]any{"action": "open_file", "persist_to_disk": true}) {
		t.Fatalf("p0-post expects lsp_file action=open_file to skip diff")
	}
	if !shouldCaptureDynamicToolDiff("lsp_edit", map[string]any{"action": "did_change", "persist_to_disk": true}) {
		t.Fatalf("p0-post expects lsp_edit action=did_change persist_to_disk=true to capture diff")
	}

	_, err := tooladapter.Dispatch(tooladapter.DynamicToolCall{Tool: "lsp_legacy_removed"}, tooladapter.Providers{
		Lookup:  p0PostEmptyLookup{},
		Counter: p0PostCounter{},
	})
	if err == nil {
		t.Fatalf("p0-post expects unknown legacy-shaped tool to fail")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "UNKNOWN_TOOL") {
		t.Fatalf("p0-post expects UNKNOWN_TOOL error code, got: %v", err)
	}
}

var _ tooladapter.RuntimeLookup = p0PostEmptyLookup{}
var _ tooladapter.ToolCallCounter = p0PostCounter{}

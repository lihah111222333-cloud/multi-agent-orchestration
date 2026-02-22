package apiserver

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
)

func TestWorkspaceSymbolToolSchema_OneOfFileOrLanguage(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_workspace_symbol")

	oneOf, ok := tool.InputSchema["oneOf"].([]map[string]any)
	if !ok {
		t.Fatalf("workspace symbol oneOf schema missing: %#v", tool.InputSchema["oneOf"])
	}
	if len(oneOf) != 2 {
		t.Fatalf("workspace symbol oneOf len = %d, want 2", len(oneOf))
	}
}

func TestImplementationToolSchema_RequiresPosition(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_implementation")

	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("required schema missing: %#v", tool.InputSchema["required"])
	}
	if len(required) != 3 || required[0] != "file_path" || required[1] != "line" || required[2] != "column" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestTypeDefinitionToolSchema_RequiresPosition(t *testing.T) {
	tool := findExtendedToolByName(t, "lsp_type_definition")

	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("required schema missing: %#v", tool.InputSchema["required"])
	}
	if len(required) != 3 || required[0] != "file_path" || required[1] != "line" || required[2] != "column" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestWorkspaceSymbolResultLimit(t *testing.T) {
	in := make([]lsp.WorkspaceSymbolResult, 0, lsp.XRefResultLimit+7)
	for range lsp.XRefResultLimit + 7 {
		in = append(in, lsp.WorkspaceSymbolResult{WorkspaceSymbol: &lsp.WorkspaceSymbol{Name: "S"}})
	}

	out := limitWorkspaceSymbolResults(in)
	if len(out) != lsp.XRefResultLimit {
		t.Fatalf("len(out) = %d, want %d", len(out), lsp.XRefResultLimit)
	}
}

func TestImplementationResultLimit(t *testing.T) {
	in := make([]lsp.LocationResult, 0, lsp.XRefResultLimit+5)
	for range lsp.XRefResultLimit + 5 {
		in = append(in, lsp.LocationResult{Location: &lsp.Location{URI: "file:///a.go"}})
	}

	out := limitLocationResults(in)
	if len(out) != lsp.XRefResultLimit {
		t.Fatalf("len(out) = %d, want %d", len(out), lsp.XRefResultLimit)
	}
}

func TestTypeDefinitionResultLimit(t *testing.T) {
	in := make([]lsp.LocationResult, 0, lsp.XRefResultLimit+3)
	for range lsp.XRefResultLimit + 3 {
		in = append(in, lsp.LocationResult{Canonical: &lsp.Location{URI: "file:///a.go"}})
	}

	out := limitLocationResults(in)
	if len(out) != lsp.XRefResultLimit {
		t.Fatalf("len(out) = %d, want %d", len(out), lsp.XRefResultLimit)
	}
}

func findExtendedToolByName(t *testing.T, name string) codex.DynamicTool {
	t.Helper()

	s := &Server{}
	tools := s.buildExtendedLSPDynamicTools()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return codex.DynamicTool{}
}

package apiserver

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

func TestOrdering_DedupeDynamicToolsByName(t *testing.T) {
	in := []codex.DynamicTool{
		{Name: "b"},
		{Name: "a"},
		{Name: "a"},
		{Name: " "},
		{Name: "c"},
	}

	out := dedupeDynamicToolsByName(in)
	if len(out) != 3 {
		t.Fatalf("dedupe len = %d, want 3", len(out))
	}
	if out[0].Name != "b" || out[1].Name != "a" || out[2].Name != "c" {
		t.Fatalf("dedupe order mismatch: %#v", out)
	}
}

func TestOrdering_WorkspaceSymbolSchemaOneOf(t *testing.T) {
	s := &Server{}
	tools := s.buildExtendedLSPDynamicTools()
	if len(tools) == 0 {
		t.Fatal("buildExtendedLSPDynamicTools returned no tools")
	}

	var ws *codex.DynamicTool
	for i := range tools {
		if tools[i].Name == "lsp_workspace_symbol" {
			ws = &tools[i]
			break
		}
	}
	if ws == nil {
		t.Fatal("lsp_workspace_symbol tool not found in extended tools")
	}

	oneOf, ok := ws.InputSchema["oneOf"].([]map[string]any)
	if !ok || len(oneOf) != 2 {
		t.Fatalf("workspace_symbol oneOf schema missing or invalid: %#v", ws.InputSchema["oneOf"])
	}
	firstRequired, ok := oneOf[0]["required"].([]string)
	if !ok || len(firstRequired) != 2 {
		t.Fatalf("workspace_symbol file_path branch missing: %#v", oneOf[0])
	}
	secondRequired, ok := oneOf[1]["required"].([]string)
	if !ok || len(secondRequired) != 2 {
		t.Fatalf("workspace_symbol language branch missing: %#v", oneOf[1])
	}
}

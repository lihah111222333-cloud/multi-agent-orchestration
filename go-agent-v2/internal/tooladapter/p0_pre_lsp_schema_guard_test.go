package tooladapter

import (
	"os"
	"sort"
	"strings"
	"testing"
)

func p0PreModeTooladapter() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("LSP_P0_MODE")))
	if mode == "" {
		return "pre"
	}
	return mode
}

func TestP0PreLSPSchemaGuard(t *testing.T) {
	if p0PreModeTooladapter() != "pre" {
		t.Skip("skip p0-pre guard when LSP_P0_MODE is not pre")
	}

	expectedLegacy := []string{
		"lsp_hover",
		"lsp_open_file",
		"lsp_diagnostics",
		"lsp_definition",
		"lsp_references",
		"lsp_document_symbol",
		"lsp_rename",
		"lsp_completion",
		"lsp_did_change",
		"lsp_code_action",
		"lsp_signature_help",
		"lsp_format",
		"lsp_call_hierarchy",
		"lsp_type_hierarchy",
		"lsp_semantic_tokens",
		"lsp_folding_range",
		"lsp_workspace_symbol",
		"lsp_implementation",
		"lsp_type_definition",
	}

	schemas := AllSchemas(testProviders(true))
	schemaSet := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		schemaSet[schema.Name] = struct{}{}
	}
	for _, name := range expectedLegacy {
		if _, ok := schemaSet[name]; !ok {
			t.Fatalf("p0-pre schema missing legacy tool %q", name)
		}
	}

	mergedNames := []string{"lsp_file", "lsp_inspect", "lsp_xref", "lsp_grep", "lsp_structure", "lsp_edit"}
	for _, name := range mergedNames {
		if _, ok := schemaSet[name]; ok {
			t.Fatalf("p0-pre should not expose merged tool %q", name)
		}
	}

	unavailableSchemas := AllSchemas(testProviders(false))
	lspNames := make([]string, 0, len(unavailableSchemas))
	for _, schema := range unavailableSchemas {
		if strings.HasPrefix(schema.Name, "lsp_") {
			lspNames = append(lspNames, schema.Name)
		}
	}
	sort.Strings(lspNames)
	if len(lspNames) != 0 {
		t.Fatalf("p0-pre expects no lsp schemas when unavailable, got: %v", lspNames)
	}
}

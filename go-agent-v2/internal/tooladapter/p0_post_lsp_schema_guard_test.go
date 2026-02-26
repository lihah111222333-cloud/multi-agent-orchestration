package tooladapter

import (
	"os"
	"sort"
	"strings"
	"testing"
)

func p0PostModeTooladapter() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("LSP_P0_MODE")))
	if mode == "" {
		return "pre"
	}
	return mode
}

func TestP0PostLSPSchemaGuard(t *testing.T) {
	if p0PostModeTooladapter() != "post" {
		t.Skip("skip p0-post guard when LSP_P0_MODE is not post")
	}

	expectedMerged := []string{
		"lsp_file",
		"lsp_inspect",
		"lsp_xref",
		"lsp_grep",
		"lsp_structure",
		"lsp_edit",
		"lsp_completion",
	}
	legacyNames := []string{
		"lsp_hover",
		"lsp_open_file",
		"lsp_diagnostics",
		"lsp_definition",
		"lsp_references",
		"lsp_document_symbol",
		"lsp_rename",
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
		"lsp_text_search",
		"lsp_ast_search",
	}

	schemas := AllSchemas(testProviders(true))
	schemaSet := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		schemaSet[schema.Name] = struct{}{}
	}
	for _, name := range expectedMerged {
		if _, ok := schemaSet[name]; !ok {
			t.Fatalf("p0-post schema missing merged tool %q", name)
		}
	}
	for _, name := range legacyNames {
		if _, ok := schemaSet[name]; ok {
			t.Fatalf("p0-post must not expose legacy tool %q", name)
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

	foundGrep := false
	for _, name := range lspNames {
		if name == "lsp_grep" {
			foundGrep = true
			continue
		}
		t.Fatalf("p0-post unavailable mode must only expose lsp_grep, got %q", name)
	}
	if !foundGrep {
		t.Fatalf("p0-post unavailable mode must expose lsp_grep")
	}
}

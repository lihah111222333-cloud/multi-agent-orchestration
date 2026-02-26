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
	expectedSet := make(map[string]struct{}, len(expectedMerged))
	for _, name := range expectedMerged {
		expectedSet[name] = struct{}{}
	}

	schemas := AllSchemas(testProviders(true))
	schemaSet := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		schemaSet[schema.Name] = struct{}{}
		if strings.HasPrefix(schema.Name, "lsp_") {
			if _, ok := expectedSet[schema.Name]; !ok {
				t.Fatalf("p0-post must not expose unexpected lsp schema %q", schema.Name)
			}
		}
	}
	for _, name := range expectedMerged {
		if _, ok := schemaSet[name]; !ok {
			t.Fatalf("p0-post schema missing merged tool %q", name)
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

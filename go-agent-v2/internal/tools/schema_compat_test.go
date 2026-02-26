package tools

import "testing"

func TestDynamicToolSchemasStable(t *testing.T) {
	base := LSPTools()
	if len(base) == 0 {
		t.Fatalf("expected LSP base schemas")
	}

	seen := make(map[string]struct{}, len(base))
	for _, schema := range base {
		if schema.Name == "" {
			t.Fatalf("schema name must not be empty")
		}
		if _, exists := seen[schema.Name]; exists {
			t.Fatalf("duplicate schema name: %s", schema.Name)
		}
		seen[schema.Name] = struct{}{}
	}

	if addons := LSPAddonTools(); len(addons) != 0 {
		t.Fatalf("expected no LSP addon schemas, got %d", len(addons))
	}
}

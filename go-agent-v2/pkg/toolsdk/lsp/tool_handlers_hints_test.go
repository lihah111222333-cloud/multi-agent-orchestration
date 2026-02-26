package lsp

import (
	"errors"
	"strings"
	"testing"
)

func TestLSPToolCursorHint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		tool       string
		errText    string
		wantSubstr string
	}{
		{
			name:       "type hierarchy not a type",
			tool:       "lsp_type_hierarchy",
			errText:    "LSP.call: textDocument/prepareTypeHierarchy error 0: not a type name",
			wantSubstr: "type or interface identifier",
		},
		{
			name:       "type definition from function",
			tool:       "lsp_type_definition",
			errText:    "LSP.call: textDocument/typeDefinition error 0: cannot find type name from type func(values ...string) string",
			wantSubstr: "concrete type",
		},
		{
			name:       "implementation wrong symbol",
			tool:       "lsp_implementation",
			errText:    "LSP.call: textDocument/implementation error 0: FirstNonEmpty is a function, not a method",
			wantSubstr: "interface type name",
		},
		{
			name:       "no hint for unrelated tool",
			tool:       "lsp_definition",
			errText:    "LSP.call: textDocument/definition error 0: boom",
			wantSubstr: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := lspToolCursorHint(tc.tool, tc.errText)
			if tc.wantSubstr == "" {
				if got != "" {
					t.Fatalf("expected empty hint, got %q", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("expected hint containing %q, got empty", tc.wantSubstr)
			}
			if !strings.Contains(got, tc.wantSubstr) {
				t.Fatalf("expected hint %q to contain %q", got, tc.wantSubstr)
			}
		})
	}
}

func TestFirstHoverSummaryLine(t *testing.T) {
	t.Parallel()

	input := "```go\nfunc Foo() string\n```\n\n---\n\nDoc body"
	got := firstHoverSummaryLine(input)
	if got != "func Foo() string" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestContextualToolErrorNoHint(t *testing.T) {
	t.Parallel()
	h := &ToolHandlers{}
	got := h.contextualToolError("lsp_definition", "x.go", 1, 1, errors.New("boom"))
	if got != "error: boom" {
		t.Fatalf("unexpected error: %q", got)
	}
}

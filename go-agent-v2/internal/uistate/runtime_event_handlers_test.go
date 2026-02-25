package uistate

import "testing"

func TestNormalizeActivityToolName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain lsp",
			in:   "lsp_open_file",
			want: "lsp_open_file",
		},
		{
			name: "slash form",
			in:   "lsp/open_file",
			want: "lsp_open_file",
		},
		{
			name: "dot form",
			in:   "lsp.open_file",
			want: "lsp_open_file",
		},
		{
			name: "namespaced form",
			in:   "functions.lsp_open_file",
			want: "lsp_open_file",
		},
		{
			name: "mixed separators and case",
			in:   "  Functions:lsp-open-file  ",
			want: "lsp_open_file",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeActivityToolName(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeActivityToolName(%q)=%q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIncrActivityStatCountsLSPVariants(t *testing.T) {
	t.Parallel()

	mgr := NewRuntimeManager()
	threadID := "thread-1"
	names := []string{
		"lsp_open_file",
		"lsp/open_file",
		"lsp.open_file",
		"functions.lsp_open_file",
	}

	for _, name := range names {
		mgr.IncrActivityStat(threadID, "toolCall", name)
	}

	stats := mgr.Snapshot().ActivityStatsByThread[threadID]
	if got, want := stats.LSPCalls, int64(len(names)); got != want {
		t.Fatalf("LSPCalls=%d, want %d", got, want)
	}
	for _, name := range names {
		if got := stats.ToolCalls[name]; got != 1 {
			t.Fatalf("ToolCalls[%q]=%d, want 1", name, got)
		}
	}
}

func TestIncrActivityStatNonLSPDoesNotCount(t *testing.T) {
	t.Parallel()

	mgr := NewRuntimeManager()
	threadID := "thread-2"
	names := []string{
		"code_run",
		"functions.code_run",
		"json_render",
		"mcp__playwright__browser_click",
	}

	for _, name := range names {
		mgr.IncrActivityStat(threadID, "toolCall", name)
	}

	stats := mgr.Snapshot().ActivityStatsByThread[threadID]
	if stats.LSPCalls != 0 {
		t.Fatalf("LSPCalls=%d, want 0", stats.LSPCalls)
	}
}

func TestResolveActivityToolName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		text    string
		payload map[string]any
		want    string
	}{
		{
			name: "prefer text",
			text: "lsp_open_file",
			payload: map[string]any{
				"tool_name": "lsp_diagnostics",
			},
			want: "lsp_open_file",
		},
		{
			name: "top level tool",
			payload: map[string]any{
				"tool": "lsp_hover",
			},
			want: "lsp_hover",
		},
		{
			name: "top level tool_name",
			payload: map[string]any{
				"tool_name": "lsp_diagnostics",
			},
			want: "lsp_diagnostics",
		},
		{
			name: "nested item tool_name",
			payload: map[string]any{
				"item": map[string]any{
					"tool_name": "lsp_references",
				},
			},
			want: "lsp_references",
		},
		{
			name: "missing",
			want: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveActivityToolName(tc.text, tc.payload)
			if got != tc.want {
				t.Fatalf("resolveActivityToolName(%q, %v)=%q, want %q", tc.text, tc.payload, got, tc.want)
			}
		})
	}
}

func TestApplyUITypeDepthsToolCallUsesPayloadToolName(t *testing.T) {
	t.Parallel()

	mgr := NewRuntimeManager()
	threadID := "thread-3"

	mgr.mu.Lock()
	mgr.ensureThreadLocked(threadID)
	rt := mgr.runtime[threadID]
	mgr.applyUITypeDepthsLocked(
		threadID,
		rt,
		UITypeToolCall,
		"mcp_tool_call_begin",
		"",
		"",
		map[string]any{"tool_name": "lsp_diagnostics"},
	)
	mgr.mu.Unlock()

	stats := mgr.Snapshot().ActivityStatsByThread[threadID]
	if got := stats.ToolCalls["lsp_diagnostics"]; got != 1 {
		t.Fatalf("ToolCalls[%q]=%d, want 1", "lsp_diagnostics", got)
	}
	if got := stats.LSPCalls; got != 1 {
		t.Fatalf("LSPCalls=%d, want 1", got)
	}
}

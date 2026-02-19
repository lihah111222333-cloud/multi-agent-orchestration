package uistate

import (
	"math"
	"testing"
)

// ── extractContextWindow ─────────────────────────────────────

func TestExtractContextWindow_StructuredPath(t *testing.T) {
	payload := map[string]any{
		"tokenUsage": map[string]any{
			"modelContextWindow": 200000,
		},
	}
	got, ok := extractContextWindow(payload)
	if !ok || got != 200000 {
		t.Fatalf("extractContextWindow = (%d, %v), want (200000, true)", got, ok)
	}
}

func TestExtractContextWindow_InfoPath(t *testing.T) {
	payload := map[string]any{
		"info": map[string]any{
			"model_context_window": 128000,
		},
	}
	got, ok := extractContextWindow(payload)
	if !ok || got != 128000 {
		t.Fatalf("extractContextWindow = (%d, %v), want (128000, true)", got, ok)
	}
}

func TestExtractContextWindow_FlatKey(t *testing.T) {
	payload := map[string]any{
		"context_window_tokens": 64000,
	}
	got, ok := extractContextWindow(payload)
	if !ok || got != 64000 {
		t.Fatalf("extractContextWindow = (%d, %v), want (64000, true)", got, ok)
	}
}

func TestExtractContextWindow_Missing(t *testing.T) {
	payload := map[string]any{"unrelated": "data"}
	_, ok := extractContextWindow(payload)
	if ok {
		t.Fatal("extractContextWindow should return false for missing keys")
	}
}

func TestExtractContextWindow_ZeroIgnored(t *testing.T) {
	payload := map[string]any{
		"tokenUsage": map[string]any{
			"modelContextWindow": 0,
		},
	}
	_, ok := extractContextWindow(payload)
	if ok {
		t.Fatal("extractContextWindow should return false for zero value")
	}
}

// ── extractTotalUsedTokens ───────────────────────────────────

func TestExtractTotalUsedTokens_PrefersLast(t *testing.T) {
	payload := map[string]any{
		"tokenUsage": map[string]any{
			"last":  map[string]any{"totalTokens": 119000},
			"total": map[string]any{"totalTokens": 40900000},
		},
	}
	got, ok := extractTotalUsedTokens(payload, false)
	if !ok || got != 119000 {
		t.Fatalf("extractTotalUsedTokens = (%d, %v), want (119000, true)", got, ok)
	}
}

func TestExtractTotalUsedTokens_FallsBackToTotal(t *testing.T) {
	payload := map[string]any{
		"tokenUsage": map[string]any{
			"total": map[string]any{"totalTokens": 3200},
		},
	}
	got, ok := extractTotalUsedTokens(payload, false)
	if !ok || got != 3200 {
		t.Fatalf("extractTotalUsedTokens = (%d, %v), want (3200, true)", got, ok)
	}
}

func TestExtractTotalUsedTokens_InfoLastPreferred(t *testing.T) {
	payload := map[string]any{
		"info": map[string]any{
			"last_token_usage":  map[string]any{"total_tokens": 1800},
			"total_token_usage": map[string]any{"total_tokens": 40900000},
		},
	}
	got, ok := extractTotalUsedTokens(payload, false)
	if !ok || got != 1800 {
		t.Fatalf("extractTotalUsedTokens = (%d, %v), want (1800, true)", got, ok)
	}
}

func TestExtractTotalUsedTokens_InfoTotalBlockedWithoutFlag(t *testing.T) {
	payload := map[string]any{
		"info": map[string]any{
			"total_token_usage": map[string]any{"total_tokens": 180000},
		},
	}
	// Without allowInfoTotal, info.total should NOT be used
	_, ok := extractTotalUsedTokens(payload, false)
	if ok {
		t.Fatal("extractTotalUsedTokens should not use info.total_token_usage when allowInfoTotal=false")
	}
}

func TestExtractTotalUsedTokens_InfoTotalAllowedWithFlag(t *testing.T) {
	payload := map[string]any{
		"info": map[string]any{
			"total_token_usage": map[string]any{"total_tokens": 91000},
		},
	}
	got, ok := extractTotalUsedTokens(payload, true)
	if !ok || got != 91000 {
		t.Fatalf("extractTotalUsedTokens = (%d, %v), want (91000, true)", got, ok)
	}
}

func TestExtractTotalUsedTokens_InputOutputFallback(t *testing.T) {
	payload := map[string]any{
		"input":                 1200,
		"output":                300,
		"context_window_tokens": 10000,
	}
	got, ok := extractTotalUsedTokens(payload, false)
	if !ok || got != 1500 {
		t.Fatalf("extractTotalUsedTokens = (%d, %v), want (1500, true)", got, ok)
	}
}

func TestExtractTotalUsedTokens_NegativeClampedToZero(t *testing.T) {
	payload := map[string]any{
		"tokenUsage": map[string]any{
			"last": map[string]any{"totalTokens": -42},
		},
	}
	got, ok := extractTotalUsedTokens(payload, false)
	if !ok || got != 0 {
		t.Fatalf("extractTotalUsedTokens = (%d, %v), want (0, true)", got, ok)
	}
}

// ── computeTokenPercent ──────────────────────────────────────

func TestComputeTokenPercent_Normal(t *testing.T) {
	used, left := computeTokenPercent(1500, 10000)
	if math.Abs(used-15) > 0.001 {
		t.Fatalf("usedPct = %f, want 15", used)
	}
	if math.Abs(left-85) > 0.001 {
		t.Fatalf("leftPct = %f, want 85", left)
	}
}

func TestComputeTokenPercent_ZeroWindow(t *testing.T) {
	used, left := computeTokenPercent(100, 0)
	if used != 0 || left != 0 {
		t.Fatalf("computeTokenPercent(100, 0) = (%f, %f), want (0, 0)", used, left)
	}
}

func TestComputeTokenPercent_ClampedAt100(t *testing.T) {
	used, left := computeTokenPercent(20000, 10000)
	if used != 100 {
		t.Fatalf("usedPct = %f, want 100 (clamped)", used)
	}
	if left != 0 {
		t.Fatalf("leftPct = %f, want 0 (clamped)", left)
	}
}

// ── applyCollabDepth ─────────────────────────────────────────

func TestApplyCollabDepth_Increment(t *testing.T) {
	rt := &threadRuntime{}
	applyCollabDepth(rt, "collab_agent_spawn_begin")
	if rt.collabDepth != 1 {
		t.Fatalf("collabDepth = %d, want 1", rt.collabDepth)
	}
	applyCollabDepth(rt, "collab_agent_interaction_begin")
	if rt.collabDepth != 2 {
		t.Fatalf("collabDepth = %d, want 2", rt.collabDepth)
	}
}

func TestApplyCollabDepth_Decrement(t *testing.T) {
	rt := &threadRuntime{collabDepth: 2}
	applyCollabDepth(rt, "collab_agent_spawn_end")
	if rt.collabDepth != 1 {
		t.Fatalf("collabDepth = %d, want 1", rt.collabDepth)
	}
}

func TestApplyCollabDepth_FloorAtZero(t *testing.T) {
	rt := &threadRuntime{collabDepth: 0}
	applyCollabDepth(rt, "collab_agent_spawn_end")
	if rt.collabDepth != 0 {
		t.Fatalf("collabDepth = %d, want 0 (floor)", rt.collabDepth)
	}
}

func TestApplyCollabDepth_NoOp(t *testing.T) {
	rt := &threadRuntime{collabDepth: 5}
	applyCollabDepth(rt, "unrelated_event")
	if rt.collabDepth != 5 {
		t.Fatalf("collabDepth = %d, want 5 (unchanged)", rt.collabDepth)
	}
}

// ── applyOverlays ────────────────────────────────────────────

func TestApplyOverlays_TerminalWaitSet(t *testing.T) {
	rt := &threadRuntime{}
	// isTerminalWaitPayload returns true when stdin is nil/absent
	applyOverlays(rt, "exec_terminal_interaction", "", map[string]any{})
	if !rt.terminalWaitOverlay {
		t.Fatal("terminalWaitOverlay should be true")
	}
}

func TestApplyOverlays_MCPStartupSet(t *testing.T) {
	rt := &threadRuntime{}
	applyOverlays(rt, "mcp_startup_update", "codex/event/mcp_startup_update", map[string]any{"server": "filesystem"})
	if !rt.mcpStartupOverlay {
		t.Fatal("mcpStartupOverlay should be true")
	}
}

func TestApplyOverlays_MCPStartupClear(t *testing.T) {
	rt := &threadRuntime{mcpStartupOverlay: true, mcpStartupLabel: "test"}
	applyOverlays(rt, "mcp_startup_complete", "codex/event/mcp_startup_complete", map[string]any{})
	if rt.mcpStartupOverlay {
		t.Fatal("mcpStartupOverlay should be cleared")
	}
	if rt.mcpStartupLabel != "" {
		t.Fatalf("mcpStartupLabel = %q, want empty", rt.mcpStartupLabel)
	}
}

// ── cloneTimelineItems ───────────────────────────────────────

func TestCloneTimelineItems_DeepCopiesPointerFields(t *testing.T) {
	exitCode := 42
	elapsedMS := 1500
	src := map[string][]TimelineItem{
		"thread-1": {
			{
				Kind:      "command",
				Text:      "echo hi",
				ExitCode:  &exitCode,
				ElapsedMS: &elapsedMS,
				Attachments: []TimelineAttachment{
					{Kind: "file", Name: "a.go"},
				},
			},
		},
	}
	dst := make(map[string][]TimelineItem)
	cloneTimelineItems(src, dst)

	// Verify deep copy
	if dst["thread-1"][0].ExitCode == src["thread-1"][0].ExitCode {
		t.Fatal("ExitCode pointer should not be shared")
	}
	if *dst["thread-1"][0].ExitCode != 42 {
		t.Fatalf("ExitCode = %d, want 42", *dst["thread-1"][0].ExitCode)
	}

	// Mutate source, verify dst unchanged
	*src["thread-1"][0].ExitCode = 999
	if *dst["thread-1"][0].ExitCode != 42 {
		t.Fatal("dst ExitCode should not be affected by src mutation")
	}
}

// ── cloneActivityStatsMap ────────────────────────────────────

func TestCloneActivityStatsMap_DeepCopiesToolCalls(t *testing.T) {
	src := map[string]ActivityStats{
		"thread-1": {
			Commands:  5,
			FileEdits: 3,
			ToolCalls: map[string]int64{"read_file": 10, "write_file": 2},
		},
	}
	dst := cloneActivityStatsMap(src)

	if dst["thread-1"].Commands != 5 {
		t.Fatalf("Commands = %d, want 5", dst["thread-1"].Commands)
	}

	// Mutate source ToolCalls, verify isolation
	src["thread-1"].ToolCalls["read_file"] = 999
	if dst["thread-1"].ToolCalls["read_file"] != 10 {
		t.Fatal("dst ToolCalls should not be affected by src mutation")
	}
}

// ── cloneAlerts ──────────────────────────────────────────────

func TestCloneAlerts_DeepCopies(t *testing.T) {
	src := map[string][]AlertEntry{
		"thread-1": {
			{Level: "error", Message: "connection lost"},
			{Level: "warning", Message: "rate limited"},
		},
	}
	dst := cloneAlerts(src)

	if len(dst["thread-1"]) != 2 {
		t.Fatalf("dst alerts len = %d, want 2", len(dst["thread-1"]))
	}

	// Mutate source, verify isolation
	src["thread-1"][0].Message = "MUTATED"
	if dst["thread-1"][0].Message != "connection lost" {
		t.Fatal("dst alerts should not be affected by src mutation")
	}
}

func TestCloneAlerts_SkipsEmpty(t *testing.T) {
	src := map[string][]AlertEntry{
		"thread-1": {},
		"thread-2": {{Level: "info", Message: "ok"}},
	}
	dst := cloneAlerts(src)

	if _, ok := dst["thread-1"]; ok {
		t.Fatal("empty alert slices should not be cloned")
	}
	if len(dst["thread-2"]) != 1 {
		t.Fatalf("dst[thread-2] len = %d, want 1", len(dst["thread-2"]))
	}
}

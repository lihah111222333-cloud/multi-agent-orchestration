// patrol_test.go — Monitor 巡检纯逻辑测试 (无 DB 依赖)。
// Python 对应: test_agent_monitor.py / test_dashboard_agent_status.py。
package monitor

import "testing"

// ========================================
// ClassifyStatus
// ========================================

func TestClassifyStatus(t *testing.T) {
	tests := []struct {
		name        string
		lines       []string
		hasSession  bool
		stagnantSec int
		want        string
	}{
		{"no_session", []string{"hello"}, false, 0, "unknown"},
		{"empty_lines_prompt_only", []string{}, true, 0, "idle"},
		{"shell_prompt_only", []string{"$"}, true, 0, "idle"},
		{"python_prompt_only", []string{">>>"}, true, 0, "idle"},
		{"multi_prompt_only", []string{"$", ">>>", ">"}, true, 0, "idle"},
		{"error_keyword", []string{"Traceback (most recent call last):"}, true, 0, "error"},
		{"exception_keyword", []string{"RuntimeError: exception occurred"}, true, 0, "error"},
		{"disconnected_timeout", []string{"connection timeout"}, true, 0, "disconnected"},
		{"disconnected_refused", []string{"connection refused"}, true, 0, "disconnected"},
		{"disconnected_econnreset", []string{"econnreset"}, true, 0, "disconnected"},
		{"stagnant_above_threshold", []string{"output remains same"}, true, 61, "stuck"},
		{"stagnant_at_threshold", []string{"output remains same"}, true, 60, "stuck"},
		{"stagnant_below_threshold", []string{"output remains same"}, true, 59, "running"},
		{"normal_running", []string{"processing data...", "step 2 done"}, true, 0, "running"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyStatus(tt.lines, tt.hasSession, tt.stagnantSec)
			if got != tt.want {
				t.Errorf("ClassifyStatus(%v, %v, %d) = %q, want %q",
					tt.lines, tt.hasSession, tt.stagnantSec, got, tt.want)
			}
		})
	}
}

// ========================================
// normalizeLines
// ========================================

func TestNormalizeLines(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want int // expected length
	}{
		{"empty", []string{}, 0},
		{"whitespace_filtered", []string{"  ", "\t", ""}, 0},
		{"mixed", []string{"hello", "  ", "world"}, 2},
		{"trims", []string{"  hello  ", "world"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLines(tt.in)
			if len(got) != tt.want {
				t.Errorf("normalizeLines(%v) len = %d, want %d", tt.in, len(got), tt.want)
			}
		})
	}
}

// ========================================
// isPromptOnly
// ========================================

func TestIsPromptOnly(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want bool
	}{
		{"empty_true", []string{}, true},
		{"shell_prompt", []string{"$"}, true},
		{"python_prompt", []string{">>>"}, true},
		{"hash_prompt", []string{"#"}, true},
		{"mixed_prompts", []string{"$", ">>>", ">"}, true},
		{"with_text_false", []string{"$", "hello"}, false},
		{"only_text_false", []string{"processing..."}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPromptOnly(tt.in)
			if got != tt.want {
				t.Errorf("isPromptOnly(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// ========================================
// containsAny
// ========================================

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		keywords []string
		want     bool
	}{
		{"match", "traceback found", []string{"traceback", "error"}, true},
		{"match_second", "fatal error", []string{"traceback", "error"}, true},
		{"no_match", "all good", []string{"traceback", "error"}, false},
		{"empty_keywords", "hello", []string{}, false},
		{"empty_text", "", []string{"error"}, false},
		{"case_sensitive_no_match", "ERROR", []string{"error"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsAny(tt.text, tt.keywords)
			if got != tt.want {
				t.Errorf("containsAny(%q, %v) = %v, want %v", tt.text, tt.keywords, got, tt.want)
			}
		})
	}
}

// ========================================
// parseOutputTail
// ========================================

func TestParseOutputTail(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		got := parseOutputTail(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("string_input", func(t *testing.T) {
		got := parseOutputTail("line1\nline2")
		if len(got) != 2 {
			t.Errorf("expected 2 lines, got %d", len(got))
		}
	})

	t.Run("empty_string", func(t *testing.T) {
		got := parseOutputTail("")
		if got != nil {
			t.Errorf("expected nil for empty string, got %v", got)
		}
	})

	t.Run("string_slice", func(t *testing.T) {
		in := []string{"a", "b", "c"}
		got := parseOutputTail(in)
		if len(got) != 3 {
			t.Errorf("expected 3, got %d", len(got))
		}
	})

	t.Run("any_slice", func(t *testing.T) {
		in := []any{"hello", "world", "  "}
		got := parseOutputTail(in)
		if len(got) != 2 { // "  " is whitespace only, filtered
			t.Errorf("expected 2 (whitespace filtered), got %d: %v", len(got), got)
		}
	})

	t.Run("int_returns_nil", func(t *testing.T) {
		got := parseOutputTail(42)
		if got != nil {
			t.Errorf("expected nil for int, got %v", got)
		}
	})
}

// ========================================
// hashLines
// ========================================

func TestHashLines(t *testing.T) {
	t.Run("truncates_to_6_lines", func(t *testing.T) {
		lines := []string{"1", "2", "3", "4", "5", "6", "7", "8"}
		hash := hashLines(lines)
		// Should only use last 6 lines
		expected := hashLines([]string{"3", "4", "5", "6", "7", "8"})
		if hash != expected {
			t.Errorf("hashLines should truncate to last 6, got different hash")
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		lines := []string{"hello", "world"}
		h1 := hashLines(lines)
		h2 := hashLines(lines)
		if h1 != h2 {
			t.Errorf("hashLines should be deterministic")
		}
	})

	t.Run("different_input_different_hash", func(t *testing.T) {
		h1 := hashLines([]string{"a"})
		h2 := hashLines([]string{"b"})
		if h1 == h2 {
			t.Errorf("different inputs should produce different hashes")
		}
	})
}

// ========================================
// summarize
// ========================================

func TestSummarize(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := summarize(nil)
		if s["total"] != 0 || s["healthy"] != 0 || s["unhealthy"] != 0 {
			t.Errorf("empty summary should be all zeros, got %v", s)
		}
	})

	t.Run("mixed_statuses", func(t *testing.T) {
		agents := []AgentSnapshot{
			{Status: "running"},
			{Status: "idle"},
			{Status: "error"},
			{Status: "stuck"},
			{Status: "running"},
		}
		s := summarize(agents)
		if s["total"] != 5 {
			t.Errorf("total = %d, want 5", s["total"])
		}
		if s["running"] != 2 {
			t.Errorf("running = %d, want 2", s["running"])
		}
		if s["idle"] != 1 {
			t.Errorf("idle = %d, want 1", s["idle"])
		}
		if s["healthy"] != 3 { // running(2) + idle(1)
			t.Errorf("healthy = %d, want 3", s["healthy"])
		}
		if s["unhealthy"] != 2 { // error(1) + stuck(1)
			t.Errorf("unhealthy = %d, want 2", s["unhealthy"])
		}
	})
}

// ========================================
// emptySummary
// ========================================

func TestEmptySummary(t *testing.T) {
	s := emptySummary()
	for _, name := range StatusNames {
		if s[name] != 0 {
			t.Errorf("emptySummary[%q] = %d, want 0", name, s[name])
		}
	}
	if s["total"] != 0 || s["healthy"] != 0 || s["unhealthy"] != 0 {
		t.Errorf("meta keys should be 0, got %v", s)
	}
}

// master_logic_test.go — Master 编排器纯逻辑测试。
// Python 对应: test_master.py (9 个纯函数 1:1 对照)。
package orchestrator

import (
	"strings"
	"testing"
)

// ========================================
// trimTaskText
// ========================================

func TestTrimTaskText(t *testing.T) {
	tests := []struct {
		name     string
		task     string
		max      int
		wantTrim bool
	}{
		{"short_unchanged", "hello", 100, false},
		{"exact_boundary", "hello", 5, false},
		{"truncated", "hello world this is a long text", 10, true},
		{"empty", "", 100, false},
		{"whitespace_trimmed", "  hello  ", 100, false},
		{"chinese_chars", "你好世界，这是一个很长的任务描述", 5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimTaskText(tt.task, tt.max)
			if tt.wantTrim {
				if !strings.Contains(got, "...(任务文本已截断)") {
					t.Errorf("expected truncation marker, got %q", got)
				}
			} else {
				if strings.Contains(got, "...") {
					t.Errorf("unexpected truncation in %q", got)
				}
			}
		})
	}
}

// ========================================
// extractJSON
// ========================================

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantNil bool
		checkFn func(map[string]any) bool
	}{
		{
			"simple_object",
			`{"key": "value"}`,
			false,
			func(m map[string]any) bool { return m["key"] == "value" },
		},
		{
			"nested_object",
			`Here is JSON: {"a": {"b": 1}}`,
			false,
			func(m map[string]any) bool {
				inner, ok := m["a"].(map[string]any)
				return ok && inner["b"] == float64(1)
			},
		},
		{
			"with_array",
			`result: {"items": [1, 2, 3]}`,
			false,
			func(m map[string]any) bool {
				items, ok := m["items"].([]any)
				return ok && len(items) == 3
			},
		},
		{"no_json", "hello world", true, nil},
		{"empty", "", true, nil},
		{
			"surrounded_by_text",
			"The output is ```json\n{\"answer\": 42}\n``` done.",
			false,
			func(m map[string]any) bool { return m["answer"] == float64(42) },
		},
		{"incomplete_json", `{"key": "value`, true, nil},
		{
			"json_in_string",
			`prefix {"outer": "has {nested}"} suffix`,
			false,
			func(m map[string]any) bool { return m["outer"] == "has {nested}" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.text)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil, got %v", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil result")
			}
			if !tt.wantNil && got != nil && tt.checkFn != nil && !tt.checkFn(got) {
				t.Errorf("check failed for result %v", got)
			}
		})
	}
}

// ========================================
// sanitizeTopology
// ========================================

func TestSanitizeTopology(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		got := sanitizeTopology(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("no_gateways_key", func(t *testing.T) {
		got := sanitizeTopology(map[string]any{"other": "data"})
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("empty_gateways", func(t *testing.T) {
		got := sanitizeTopology(map[string]any{"gateways": []any{}})
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("valid_topology", func(t *testing.T) {
		input := map[string]any{
			"gateways": []any{
				map[string]any{
					"id":   "gw1",
					"name": "Gateway 1",
					"agents": []any{
						map[string]any{"id": "agent1", "name": "Agent 1"},
					},
				},
			},
		}
		got := sanitizeTopology(input)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		gws := got["gateways"].([]map[string]any)
		if len(gws) != 1 || gws[0]["id"] != "gw1" {
			t.Errorf("unexpected result %v", got)
		}
	})

	t.Run("dedup_gateway_ids", func(t *testing.T) {
		input := map[string]any{
			"gateways": []any{
				map[string]any{
					"id": "gw1", "name": "G1",
					"agents": []any{map[string]any{"id": "a1", "name": "A1"}},
				},
				map[string]any{
					"id": "gw1", "name": "G1 dup",
					"agents": []any{map[string]any{"id": "a2", "name": "A2"}},
				},
			},
		}
		got := sanitizeTopology(input)
		gws := got["gateways"].([]map[string]any)
		if len(gws) != 1 {
			t.Errorf("expected 1 gateway after dedup, got %d", len(gws))
		}
	})

	t.Run("auto_generate_id", func(t *testing.T) {
		input := map[string]any{
			"gateways": []any{
				map[string]any{
					"name": "No ID Gateway",
					"agents": []any{
						map[string]any{"name": "No ID Agent"},
					},
				},
			},
		}
		got := sanitizeTopology(input)
		gws := got["gateways"].([]map[string]any)
		if gws[0]["id"] != "gateway_1" {
			t.Errorf("expected auto-generated id 'gateway_1', got %q", gws[0]["id"])
		}
	})

	t.Run("skip_gateway_without_agents", func(t *testing.T) {
		input := map[string]any{
			"gateways": []any{
				map[string]any{"id": "gw_empty"},
			},
		}
		got := sanitizeTopology(input)
		if got != nil {
			t.Errorf("expected nil (gateway without agents), got %v", got)
		}
	})
}

// ========================================
// scoreOutputQuality
// ========================================

func TestScoreOutputQuality(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantMin int
		wantMax int
	}{
		{"empty_zero", "", 0, 0},
		{"short_low", "ok", 0, 20},
		{"medium_text", "This is a reasonable output.\nIt has multiple lines.\nShowing results.", 5, 80},
		{"error_penalty", "error occurred: traceback found", 0, 30},
		{"chinese_error_penalty", "任务超时: 无法连接", 0, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scoreOutputQuality(tt.text)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("scoreOutputQuality = %d, want [%d, %d]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// ========================================
// normalizeAssignmentLine
// ========================================

func TestNormalizeAssignmentLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"code_fence", "```json", ""},
		{"bullet_dash", "- gw1 | task1", "gw1 | task1"},
		{"bullet_star", "* gw1 | task1", "gw1 | task1"},
		{"numbered", "1. gw1 | task1", "gw1 | task1"},
		{"backtick_wrapper", "`gw1 | task1`", "gw1 | task1"},
		{"quoted_block", "> - gw1 | task1", "gw1 | task1"},
		{"plain", "gw1 | task1", "gw1 | task1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAssignmentLine(tt.in)
			if got != tt.want {
				t.Errorf("normalizeAssignmentLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ========================================
// parseAssignments
// ========================================

func TestParseAssignments(t *testing.T) {
	gateways := map[string]bool{"gw1": true, "gw2": true}

	t.Run("normal_parsing", func(t *testing.T) {
		text := "gw1 | do this\ngw2 | do that"
		got := parseAssignments(text, gateways)
		if got["gw1"] != "do this" || got["gw2"] != "do that" {
			t.Errorf("unexpected result %v", got)
		}
	})

	t.Run("unknown_gateway_filtered", func(t *testing.T) {
		text := "gw1 | task\ngw_unknown | other"
		got := parseAssignments(text, gateways)
		if len(got) != 1 || got["gw1"] != "task" {
			t.Errorf("expected only gw1, got %v", got)
		}
	})

	t.Run("empty_subtask_filtered", func(t *testing.T) {
		text := "gw1 | "
		got := parseAssignments(text, gateways)
		if len(got) != 0 {
			t.Errorf("expected empty, got %v", got)
		}
	})

	t.Run("with_list_prefix", func(t *testing.T) {
		text := "1. gw1 | analyze code\n2. gw2 | review docs"
		got := parseAssignments(text, gateways)
		if got["gw1"] != "analyze code" || got["gw2"] != "review docs" {
			t.Errorf("unexpected result %v", got)
		}
	})
}

// ========================================
// truncateSummaryText
// ========================================

func TestTruncateSummaryText(t *testing.T) {
	t.Run("short_unchanged", func(t *testing.T) {
		got := truncateSummaryText("hello world", 100)
		if got != "hello world" {
			t.Errorf("expected 'hello world', got %q", got)
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := truncateSummaryText("", 100)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		text := "word1 word2 word3 word4 word5"
		got := truncateSummaryText(text, 3)
		if !strings.Contains(got, "...(内容已截断") {
			t.Errorf("expected truncation marker, got %q", got)
		}
	})

	t.Run("chinese_truncation", func(t *testing.T) {
		text := "你好世界这是一个测试"
		got := truncateSummaryText(text, 3) // 3 Chinese char units
		if !strings.Contains(got, "...(内容已截断") {
			t.Errorf("expected truncation, got %q", got)
		}
	})
}

// ========================================
// degradedTask & fallbackAssignments
// ========================================

func TestDegradedTask(t *testing.T) {
	got := degradedTask("分析代码")
	if !strings.Contains(got, "分析代码") {
		t.Errorf("missing original task in degraded text")
	}
	if !strings.Contains(got, "[降级模式]") {
		t.Errorf("missing degradation marker")
	}
}

func TestFallbackAssignments(t *testing.T) {
	gateways := map[string]bool{"gw1": true, "gw2": true}
	got := fallbackAssignments("task", gateways)
	if len(got) != 2 {
		t.Errorf("expected 2 assignments, got %d", len(got))
	}
	for _, v := range got {
		if !strings.Contains(v, "[降级模式]") {
			t.Errorf("each assignment should contain degradation marker, got %q", v)
		}
	}
}

// ========================================
// gatewayPromptBrief
// ========================================

func TestGatewayPromptBrief(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		gw := map[string]any{
			"name":         "Test Gateway",
			"description":  "handles tests",
			"capabilities": []any{"testing", "debugging"},
		}
		got := gatewayPromptBrief("gw1", gw)
		if !strings.HasPrefix(got, "- gw1:") {
			t.Errorf("expected prefix '- gw1:', got %q", got)
		}
		if !strings.Contains(got, "testing") {
			t.Errorf("expected capability 'testing' in brief, got %q", got)
		}
	})

	t.Run("no_capabilities", func(t *testing.T) {
		gw := map[string]any{
			"name":        "Empty",
			"description": "",
		}
		got := gatewayPromptBrief("gw2", gw)
		if !strings.Contains(got, "未声明") {
			t.Errorf("expected '未声明' for empty capabilities, got %q", got)
		}
	})
}

// ========================================
// extractStringSlice
// ========================================

func TestExtractStringSlice(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want int
	}{
		{"nil", nil, 0},
		{"empty_slice", []any{}, 0},
		{"strings", []any{"a", "b"}, 2},
		{"with_blank", []any{"a", "", "b"}, 2},
		{"not_array", "hello", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStringSlice(tt.in)
			if len(got) != tt.want {
				t.Errorf("extractStringSlice = %v, want len %d", got, tt.want)
			}
		})
	}
}

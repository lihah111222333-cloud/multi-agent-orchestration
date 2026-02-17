// agent_status_test.go — Agent 状态验证的纯逻辑测试。
// Python 对应: test_agent_status_store.py → test_upsert_rejects_invalid_status。
package store

import "testing"

func TestValidateAgentID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid_simple", "agent_01", false},
		{"valid_dots_hyphens", "my-agent.v2", false},
		{"valid_uppercase", "Agent_01", false},
		{"empty", "", true},
		{"special_chars", "agent!@#", true},
		{"spaces", "agent 01", true},
		{"slash", "agent/01", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgentID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAgentID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeOutputTail(t *testing.T) {
	t.Run("nil_returns_empty_slice", func(t *testing.T) {
		got := normalizeOutputTail(nil)
		sl, ok := got.([]string)
		if !ok {
			t.Fatalf("expected []string, got %T", got)
		}
		if len(sl) != 0 {
			t.Errorf("expected empty slice, got %d elements", len(sl))
		}
	})

	t.Run("truncate_over_50", func(t *testing.T) {
		lines := make([]string, 60)
		for i := range lines {
			lines[i] = "line"
		}
		got := normalizeOutputTail(lines)
		sl, ok := got.([]string)
		if !ok {
			t.Fatalf("expected []string, got %T", got)
		}
		if len(sl) != 50 {
			t.Errorf("expected 50 lines, got %d", len(sl))
		}
	})

	t.Run("under_50_unchanged", func(t *testing.T) {
		lines := []string{"a", "b", "c"}
		got := normalizeOutputTail(lines)
		sl, ok := got.([]string)
		if !ok {
			t.Fatalf("expected []string, got %T", got)
		}
		if len(sl) != 3 {
			t.Errorf("expected 3 lines, got %d", len(sl))
		}
	})
}

func TestValidStatuses(t *testing.T) {
	valid := []string{"idle", "running", "stagnant", "error", "stopped", "unknown"}
	for _, s := range valid {
		t.Run("valid_"+s, func(t *testing.T) {
			if !validStatuses[s] {
				t.Errorf("status %q should be valid", s)
			}
		})
	}

	invalid := []string{"bad-status", "paused", ""}
	for _, s := range invalid {
		name := s
		if name == "" {
			name = "empty"
		}
		t.Run("invalid_"+name, func(t *testing.T) {
			if validStatuses[s] {
				t.Errorf("status %q should be invalid", s)
			}
		})
	}
}

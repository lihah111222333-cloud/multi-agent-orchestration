// bridge_test.go — Telegram 桥接纯逻辑测试。
// Python 对应: test_tg_bridge_watchdog.py + inline tg_bridge.py tests。
package telegram

import (
	"strings"
	"sync"
	"testing"
)

func TestTruncateText(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		maxLen  int
		wantCut bool
	}{
		{"short_unchanged", "hello", 100, false},
		{"exact_boundary", "hello", 5, false},
		{"truncated", strings.Repeat("x", 5000), 4000, true},
		{"empty", "", 100, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateText(tt.text, tt.maxLen)
			if tt.wantCut {
				if !strings.Contains(got, "... (已截断) ...") {
					t.Errorf("expected truncation marker")
				}
				if len([]rune(got)) >= len([]rune(tt.text)) {
					t.Errorf("expected shorter output")
				}
			} else {
				if strings.Contains(got, "... (已截断)") {
					t.Errorf("unexpected truncation")
				}
			}
		})
	}
}

func TestIsAuthorized(t *testing.T) {
	tests := []struct {
		name    string
		chatID  string
		allowed string
		want    bool
	}{
		{"empty_allowed_all", "12345", "", true},
		{"match", "12345", "12345", true},
		{"mismatch", "12345", "99999", false},
		{"empty_chatid", "", "12345", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAuthorized(tt.chatID, tt.allowed)
			if got != tt.want {
				t.Errorf("isAuthorized(%q, %q) = %v, want %v", tt.chatID, tt.allowed, got, tt.want)
			}
		})
	}
}

func TestNormalizeMasterName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"normal", "主Agent", "主agent"},
		{"typo_agenr", "主Agenr", "主agent"},
		{"typo_agnet", "主Agnet", "主agent"},
		{"empty", "", ""},
		{"whitespace", "  主Agent  ", "主agent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeMasterName(tt.in)
			if got != tt.want {
				t.Errorf("normalizeMasterName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsWorkerAgentID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"valid_01", "agent_01", true},
		{"valid_99", "agent_99", true},
		{"case_insensitive", "Agent_01", true},
		{"invalid_single", "agent_1", false},
		{"invalid_triple", "agent_001", false},
		{"invalid_prefix", "worker_01", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWorkerAgentID(tt.in)
			if got != tt.want {
				t.Errorf("isWorkerAgentID(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestHistory(t *testing.T) {
	t.Run("add_and_get", func(t *testing.T) {
		h := NewHistory()
		h.Add("user", "hello", "123", "alice", "ok")
		h.Add("bot", "world", "123", "", "ok")
		entries := h.Get(10)
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
		if entries[0].Role != "user" || entries[1].Role != "bot" {
			t.Errorf("unexpected roles: %q, %q", entries[0].Role, entries[1].Role)
		}
	})

	t.Run("limit_returns_recent", func(t *testing.T) {
		h := NewHistory()
		for i := 0; i < 10; i++ {
			h.Add("user", "msg", "", "", "ok")
		}
		entries := h.Get(3)
		if len(entries) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(entries))
		}
	})

	t.Run("max_len_ring_buffer", func(t *testing.T) {
		h := &History{maxLen: 5}
		for i := 0; i < 10; i++ {
			h.Add("user", "msg", "", "", "ok")
		}
		if h.Len() != 5 {
			t.Errorf("expected 5 entries after overflow, got %d", h.Len())
		}
	})

	t.Run("clear", func(t *testing.T) {
		h := NewHistory()
		h.Add("user", "hello", "", "", "ok")
		h.Clear()
		if h.Len() != 0 {
			t.Errorf("expected 0 after clear, got %d", h.Len())
		}
	})

	t.Run("text_truncated_to_4000", func(t *testing.T) {
		h := NewHistory()
		longText := strings.Repeat("x", 5000)
		entry := h.Add("user", longText, "", "", "ok")
		if len([]rune(entry.Text)) != 4000 {
			t.Errorf("expected text truncated to 4000, got %d", len([]rune(entry.Text)))
		}
	})

	t.Run("concurrent_safe", func(t *testing.T) {
		h := NewHistory()
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				h.Add("user", "msg", "", "", "ok")
			}()
		}
		wg.Wait()
		if h.Len() != 100 {
			t.Errorf("expected 100 concurrent adds, got %d", h.Len())
		}
	})
}

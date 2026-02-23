// methods_thread_test.go — 重构护栏: thread 操作相关纯函数的行为基线测试。
package apiserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ========================================
// calculateHydrationLoadLimit
// ========================================

func TestCalculateHydrationLoadLimit(t *testing.T) {
	tests := []struct {
		name         string
		initialCount int
		total        int64
		want         int
	}{
		{"zero_both", 0, 0, 0},
		{"initial_bigger", 100, 50, 100},
		{"total_bigger", 50, 200, 200},
		{"exceeds_max", 100, 99999, threadMessageHydrationMaxRecords},
		{"negative_initial", -5, 10, 10},
		{"negative_initial_exceeds", -5, 99999, threadMessageHydrationMaxRecords},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateHydrationLoadLimit(tt.initialCount, tt.total)
			if got != tt.want {
				t.Errorf("calculateHydrationLoadLimit(%d, %d) = %d, want %d",
					tt.initialCount, tt.total, got, tt.want)
			}
		})
	}
}

// ========================================
// sanitizeArchiveName / sanitizeArchiveNameStrict
// ========================================

func TestSanitizeArchiveName(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"alphanumeric", "hello123", "hello123"},
		{"special_chars", "hello world!@#$%", "hello_world"},
		{"dots_dashes", "my-archive.v2", "my-archive.v2"},
		{"empty", "", ""},
		{"only_special", "!@#", ""},
		{"leading_dot", ".hidden", "hidden"},
		{"trailing_underscore", "name_", "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeArchiveName(tt.raw)
			if got != tt.want {
				t.Errorf("sanitizeArchiveName(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSanitizeArchiveNameStrict(t *testing.T) {
	// valid
	if name, err := sanitizeArchiveNameStrict("valid-name"); err != nil || name != "valid-name" {
		t.Errorf("valid: got %q, %v", name, err)
	}
	// empty → error
	if _, err := sanitizeArchiveNameStrict(""); err == nil {
		t.Error("empty: expected error, got nil")
	}
	// only special → error
	if _, err := sanitizeArchiveNameStrict("!@#"); err == nil {
		t.Error("special: expected error, got nil")
	}
}

// ========================================
// inferThreadArtifactKind
// ========================================

func TestInferThreadArtifactKind(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"rollout", "rollout-123.jsonl", "rollout"},
		{"breakpoint", "bp-snapshot.dat", "breakpoint"},
		{"shell", "setup.sh", "shell_snapshot"},
		{"jsonl_other", "events.jsonl", "jsonl"},
		{"unknown", "readme.md", "artifact"},
		{"empty", "", "artifact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferThreadArtifactKind(tt.filename)
			if got != tt.want {
				t.Errorf("inferThreadArtifactKind(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

// ========================================
// pathWithinRoot
// ========================================

func TestPathWithinRoot(t *testing.T) {
	// Create temp dir for deterministic paths
	tmpDir := t.TempDir()
	child := filepath.Join(tmpDir, "sub", "file.txt")
	_ = os.MkdirAll(filepath.Dir(child), 0o755)

	within, err := pathWithinRoot(tmpDir, child)
	if err != nil || !within {
		t.Errorf("child: got %v, %v; want true, nil", within, err)
	}

	// root itself
	within, err = pathWithinRoot(tmpDir, tmpDir)
	if err != nil || !within {
		t.Errorf("self: got %v, %v; want true, nil", within, err)
	}

	// outside
	within, err = pathWithinRoot(tmpDir, "/tmp")
	if err != nil {
		t.Fatalf("outside: error=%v", err)
	}
	if within {
		t.Error("outside: got true, want false")
	}
}

// ========================================
// normalizeThreadArchiveMap
// ========================================

func TestNormalizeThreadArchiveMap(t *testing.T) {
	// nil → empty map
	got := normalizeThreadArchiveMap(nil)
	if len(got) != 0 {
		t.Errorf("nil: got %v", got)
	}

	// map[string]int64
	got = normalizeThreadArchiveMap(map[string]int64{"a": 123, "": 456})
	if len(got) != 1 || got["a"] != 123 {
		t.Errorf("int64 map: got %v", got)
	}

	// map[string]any with float64
	got = normalizeThreadArchiveMap(map[string]any{"b": float64(789)})
	if got["b"] != 789 {
		t.Errorf("any map: got %v", got)
	}

	// JSON string
	got = normalizeThreadArchiveMap(`{"c": 1000}`)
	if got["c"] != 1000 {
		t.Errorf("json string: got %v", got)
	}

	// json.RawMessage
	raw := json.RawMessage(`{"d": 2000}`)
	got = normalizeThreadArchiveMap(raw)
	if got["d"] != 2000 {
		t.Errorf("raw message: got %v", got)
	}

	// zero value filtered
	got = normalizeThreadArchiveMap(map[string]any{"e": float64(0)})
	if len(got) != 0 {
		t.Errorf("zero: got %v", got)
	}
}

// pure_functions_test.go — 重构前行为保持测试 (characterization tests)。
//
// 覆盖 app.go / debug_server.go / build_info.go 中纯函数。
package main

import (
	"errors"
	"testing"
	"time"
)

// ========================================
// isDialogCancelError
// ========================================

func TestIsDialogCancelError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"cancel", errors.New("user cancelled"), true},
		{"Cancel_uppercase", errors.New("Cancel"), true},
		{"cancelled", errors.New("dialog cancelled by user"), true},
		{"other_error", errors.New("permission denied"), false},
		{"empty_error", errors.New(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDialogCancelError(tt.err)
			if got != tt.want {
				t.Errorf("isDialogCancelError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ========================================
// splitPickerOutput
// ========================================

func TestSplitPickerOutput(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"single", "/path/to/file", []string{"/path/to/file"}},
		{"multi", "/a.go\n/b.go\n/c.go", []string{"/a.go", "/b.go", "/c.go"}},
		{"with_empty_lines", "/a.go\n\n/b.go\n \n/c.go", []string{"/a.go", "/b.go", "/c.go"}},
		{"trailing_newline", "/file\n", []string{"/file"}},
		{"empty", "", []string{}},
		{"whitespace_only", "  \n  \n  ", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitPickerOutput(tt.raw)
			if len(got) != len(tt.want) {
				t.Errorf("splitPickerOutput(%q) len = %d, want %d\ngot: %v", tt.raw, len(got), len(tt.want), got)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitPickerOutput(%q)[%d] = %q, want %q", tt.raw, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ========================================
// isCallAPIHotMethod
// ========================================

func TestIsCallAPIHotMethod(t *testing.T) {
	tests := []struct {
		method string
		want   bool
	}{
		{"thread/list", true},
		{"workspace/run/list", true},
		{"thread/messages", true},
		{"thread/start", false},
		{"model/list", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := isCallAPIHotMethod(tt.method)
			if got != tt.want {
				t.Errorf("isCallAPIHotMethod(%q) = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

// ========================================
// shouldLogCallAPIBegin
// ========================================

func TestShouldLogCallAPIBegin(t *testing.T) {
	tests := []struct {
		name   string
		method string
		reqID  int64
		want   bool
	}{
		{"early_request", "thread/list", 1, true},
		{"early_6", "thread/list", 6, true},
		{"non_hot_always", "model/list", 100, true},
		{"hot_not_sample", "thread/list", 7, false},
		{"hot_sample_30", "thread/list", 30, true},
		{"hot_sample_60", "thread/list", 60, true},
		{"hot_not_sample_31", "thread/list", 31, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldLogCallAPIBegin(tt.method, tt.reqID)
			if got != tt.want {
				t.Errorf("shouldLogCallAPIBegin(%q, %d) = %v, want %v", tt.method, tt.reqID, got, tt.want)
			}
		})
	}
}

// ========================================
// shouldLogCallAPIDone
// ========================================

func TestShouldLogCallAPIDone(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		reqID    int64
		duration time.Duration
		want     bool
	}{
		{"early_request", "thread/list", 1, 0, true},
		{"early_6", "thread/list", 6, 0, true},
		{"slow_always", "thread/list", 100, 1200 * time.Millisecond, true},
		{"slow_1201ms", "thread/list", 100, 1201 * time.Millisecond, true},
		{"non_hot_always", "model/list", 100, 0, true},
		{"hot_fast_not_sample", "thread/list", 7, 100 * time.Millisecond, false},
		{"hot_fast_sample_30", "thread/list", 30, 100 * time.Millisecond, true},
		{"hot_fast_not_sample_31", "thread/list", 31, 100 * time.Millisecond, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldLogCallAPIDone(tt.method, tt.reqID, tt.duration)
			if got != tt.want {
				t.Errorf("shouldLogCallAPIDone(%q, %d, %v) = %v, want %v",
					tt.method, tt.reqID, tt.duration, got, tt.want)
			}
		})
	}
}

// ========================================
// shouldLogBridgeNotify
// ========================================

func TestShouldLogBridgeNotify(t *testing.T) {
	tests := []struct {
		name   string
		method string
		seq    int64
		want   bool
	}{
		{"non_delta_always", "turn/started", 1, true},
		{"non_delta_always_2", "thread/started", 999, true},
		{"delta_sample_120", "item/agentMessage/delta", 120, true},
		{"delta_not_sample", "item/agentMessage/delta", 1, false},
		{"delta_not_sample_2", "item/agentMessage/delta", 7, false},
		{"output_sample_120", "item/commandExecution/outputDelta", 120, true},
		{"output_not_sample", "item/commandExecution/outputDelta", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldLogBridgeNotify(tt.method, tt.seq)
			if got != tt.want {
				t.Errorf("shouldLogBridgeNotify(%q, %d) = %v, want %v",
					tt.method, tt.seq, got, tt.want)
			}
		})
	}
}

// ========================================
// shouldLogBridgePublish
// ========================================

func TestShouldLogBridgePublish(t *testing.T) {
	tests := []struct {
		name   string
		method string
		seq    int64
		want   bool
	}{
		{"non_delta_always", "turn/started", 1, true},
		{"delta_sample_120", "item/agentMessage/delta", 120, true},
		{"delta_not_sample", "item/agentMessage/delta", 1, false},
		{"output_sample", "item/commandExecution/outputDelta", 120, true},
		{"stream_sample", "data/stream", 120, true},
		{"stream_not_sample", "data/stream", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldLogBridgePublish(tt.method, tt.seq)
			if got != tt.want {
				t.Errorf("shouldLogBridgePublish(%q, %d) = %v, want %v",
					tt.method, tt.seq, got, tt.want)
			}
		})
	}
}

// ========================================
// buildDebugShimScript
// ========================================

func TestBuildDebugShimScript(t *testing.T) {
	result := buildDebugShimScript("http://localhost:4501")
	if result == "" {
		t.Fatal("buildDebugShimScript returned empty string")
	}
	// 确认 URL 被替换
	if !contains(result, "http://localhost:4501") {
		t.Error("URL not injected into shim script")
	}
	// 确认模板占位符已替换
	if contains(result, "__APP_SERVER_BASE_URL__") {
		t.Error("template placeholder not replaced")
	}
}

// ========================================
// shortCommit
// ========================================

func TestShortCommit(t *testing.T) {
	tests := []struct {
		name     string
		revision string
		want     string
	}{
		{"long", "abcdef1234567890", "abcdef123456"},
		{"exact_12", "abcdef123456", "abcdef123456"},
		{"short", "abcdef", "abcdef"},
		{"empty", "", ""},
		{"with_spaces", "  abcdef1234567890  ", "abcdef123456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortCommit(tt.revision)
			if got != tt.want {
				t.Errorf("shortCommit(%q) = %q, want %q", tt.revision, got, tt.want)
			}
		})
	}
}

// ========================================
// helpers
// ========================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

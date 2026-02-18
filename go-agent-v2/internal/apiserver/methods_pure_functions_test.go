// methods_pure_functions_test.go — 重构前行为保持测试 (characterization tests)。
//
// 覆盖 methods.go 中待重构纯函数:
//   - fuzzyMatch
//   - extractInputs
//   - isAllowedEnvKey
//   - boolToStatus
//   - limitedWriter
package apiserver

import (
	"strings"
	"testing"
)

// ========================================
// fuzzyMatch
// ========================================

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		pattern string
		want    bool
	}{
		{"exact", "hello", "hello", true},
		{"subsequence", "hello world", "hwd", true},
		{"prefix", "foobar", "foo", true},
		{"suffix", "foobar", "bar", true},
		{"no_match", "hello", "xyz", false},
		{"empty_pattern", "anything", "", true},
		{"empty_text", "", "a", false},
		{"both_empty", "", "", true},
		{"interleaved", "abcdefgh", "aceg", true},
		{"case_sensitive", "Hello", "hello", false},
		{"single_char_match", "abc", "b", true},
		{"single_char_no_match", "abc", "z", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fuzzyMatch(tt.text, tt.pattern)
			if got != tt.want {
				t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.text, tt.pattern, got, tt.want)
			}
		})
	}
}

// ========================================
// extractInputs
// ========================================

func TestExtractInputs(t *testing.T) {
	tests := []struct {
		name       string
		inputs     []UserInput
		wantPrompt string
		wantImages []string
		wantFiles  []string
	}{
		{
			name:       "empty",
			inputs:     nil,
			wantPrompt: "",
			wantImages: nil,
			wantFiles:  nil,
		},
		{
			name: "text_only",
			inputs: []UserInput{
				{Type: "text", Text: "hello"},
				{Type: "text", Text: "world"},
			},
			wantPrompt: "hello\nworld",
			wantImages: nil,
			wantFiles:  nil,
		},
		{
			name: "images",
			inputs: []UserInput{
				{Type: "image", URL: "https://example.com/a.png"},
				{Type: "localImage", Path: "/tmp/b.png"},
			},
			wantPrompt: "",
			wantImages: []string{"https://example.com/a.png", "/tmp/b.png"},
			wantFiles:  nil,
		},
		{
			name: "files",
			inputs: []UserInput{
				{Type: "fileContent", Path: "/src/main.go"},
				{Type: "mention", Path: "/src/util.go"},
			},
			wantPrompt: "",
			wantImages: nil,
			wantFiles:  []string{"/src/main.go", "/src/util.go"},
		},
		{
			name: "skill",
			inputs: []UserInput{
				{Type: "skill", Name: "debug", Content: "fix this bug"},
			},
			wantPrompt: "[skill:debug] fix this bug",
			wantImages: nil,
			wantFiles:  nil,
		},
		{
			name: "mixed_all",
			inputs: []UserInput{
				{Type: "text", Text: "analyze"},
				{Type: "image", URL: "https://img.com/x.png"},
				{Type: "fileContent", Path: "/code.go"},
				{Type: "skill", Name: "review", Content: "review code"},
			},
			wantPrompt: "analyze\n[skill:review] review code",
			wantImages: []string{"https://img.com/x.png"},
			wantFiles:  []string{"/code.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt, images, files := extractInputs(tt.inputs)
			if prompt != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", prompt, tt.wantPrompt)
			}
			if !sliceEqual(images, tt.wantImages) {
				t.Errorf("images = %v, want %v", images, tt.wantImages)
			}
			if !sliceEqual(files, tt.wantFiles) {
				t.Errorf("files = %v, want %v", files, tt.wantFiles)
			}
		})
	}
}

// ========================================
// isAllowedEnvKey
// ========================================

func TestIsAllowedEnvKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"OPENAI_API_KEY", true},
		{"ANTHROPIC_MODEL", true},
		{"CODEX_TIMEOUT", true},
		{"MODEL_NAME", true},
		{"LOG_LEVEL", true},
		{"AGENT_MODE", true},
		{"MCP_SERVER", true},
		{"APP_PORT", true},
		{"STRESS_TEST_DURATION", true},
		{"TEST_E2E_KEY", true},
		// 拒绝
		{"PATH", false},
		{"HOME", false},
		{"SHELL", false},
		{"USER", false},
		{"LD_PRELOAD", false},
		{"RANDOM_VAR", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := isAllowedEnvKey(tt.key)
			if got != tt.want {
				t.Errorf("isAllowedEnvKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// ========================================
// boolToStatus
// ========================================

func TestBoolToStatus(t *testing.T) {
	if got := boolToStatus(true); got != "met" {
		t.Errorf("boolToStatus(true) = %q, want %q", got, "met")
	}
	if got := boolToStatus(false); got != "unmet" {
		t.Errorf("boolToStatus(false) = %q, want %q", got, "unmet")
	}
}

// ========================================
// limitedWriter
// ========================================

func TestLimitedWriter_Normal(t *testing.T) {
	var sb strings.Builder
	lw := &limitedWriter{w: &sb, limit: 100}
	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
	if sb.String() != "hello" {
		t.Errorf("output = %q, want %q", sb.String(), "hello")
	}
}

func TestLimitedWriter_ExceedsLimit(t *testing.T) {
	var sb strings.Builder
	lw := &limitedWriter{w: &sb, limit: 5}
	// 写入超限数据
	n, err := lw.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 只写入 5 字节
	if n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
	if sb.String() != "hello" {
		t.Errorf("output = %q, want %q", sb.String(), "hello")
	}
	// 后续写入应被丢弃
	n2, err2 := lw.Write([]byte("more"))
	if err2 != nil {
		t.Fatalf("unexpected error on discard: %v", err2)
	}
	// 返回原始长度 (不报 short write)
	if n2 != 4 {
		t.Errorf("discard n = %d, want 4", n2)
	}
	if sb.String() != "hello" {
		t.Errorf("output after discard = %q, want %q", sb.String(), "hello")
	}
}

func TestLimitedWriter_ExactLimit(t *testing.T) {
	var sb strings.Builder
	lw := &limitedWriter{w: &sb, limit: 5}
	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
	if sb.String() != "hello" {
		t.Errorf("output = %q, want %q", sb.String(), "hello")
	}
}

func TestLimitedWriter_MultipleWrites(t *testing.T) {
	var sb strings.Builder
	lw := &limitedWriter{w: &sb, limit: 10}
	lw.Write([]byte("hello"))
	lw.Write([]byte(" world!!"))

	// 只写入前 10 字节
	if sb.String() != "hello worl" {
		t.Errorf("output = %q, want %q", sb.String(), "hello worl")
	}
}

// ========================================
// helpers
// ========================================

func sliceEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

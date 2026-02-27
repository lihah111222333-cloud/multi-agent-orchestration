// methods_helpers_test.go — 重构护栏: helpers 相关纯函数的行为基线测试。
package apiserver

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	lifecycleconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/lifecycle"
)

// ========================================
// isLikelyCodexThreadID
// ========================================

func TestIsLikelyCodexThreadID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"valid_uuid", "550e8400-e29b-41d4-a716-446655440000", true},
		{"urn_uuid", "urn:uuid:550e8400-e29b-41d4-a716-446655440000", true},
		{"URN_UUID", "URN:UUID:550e8400-e29b-41d4-a716-446655440000", true},
		{"not_uuid", "agent-123", false},
		{"empty", "", false},
		{"whitespace", "   ", false},
		{"partial_uuid", "550e8400-e29b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLikelyCodexThreadID(tt.raw)
			if got != tt.want {
				t.Errorf("isLikelyCodexThreadID(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// ========================================
// normalizeCodexThreadID
// ========================================

func TestNormalizeCodexThreadID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"valid", "550e8400-E29B-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
		{"urn", "urn:uuid:550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
		{"not_uuid", "agent-123", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCodexThreadID(tt.raw)
			if got != tt.want {
				t.Errorf("normalizeCodexThreadID(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// ========================================
// buildResumeCandidates
// ========================================

func TestBuildResumeCandidates(t *testing.T) {
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	// when threadID is a UUID, return it directly
	got := lifecycleconsumer.BuildResumeCandidates(uuid, nil, normalizeCodexThreadID)
	if len(got) != 1 || got[0] != uuid {
		t.Errorf("UUID input: got %v, want [%s]", got, uuid)
	}
	// when threadID is NOT a UUID, use resolved
	resolved := []string{"id-1", "id-2", "id-2"} // 2nd is dup
	got = lifecycleconsumer.BuildResumeCandidates("my-agent", resolved, normalizeCodexThreadID)
	if len(got) != 2 {
		t.Errorf("dedup: got %d, want 2", len(got))
	}
	// when no resolved, fallback to threadID
	got = lifecycleconsumer.BuildResumeCandidates("my-agent", nil, normalizeCodexThreadID)
	if len(got) != 1 || got[0] != "my-agent" {
		t.Errorf("fallback: got %v, want [my-agent]", got)
	}
	// empty
	got = lifecycleconsumer.BuildResumeCandidates("", nil, normalizeCodexThreadID)
	if got != nil {
		t.Errorf("empty: got %v, want nil", got)
	}
}

// ========================================
// tryResumeCandidates
// ========================================

func TestTryResumeCandidates(t *testing.T) {
	// no candidates
	_, err := lifecycleconsumer.TryResumeCandidates(nil, "fallback", nil, lifecycleconsumer.IsHistoricalResumeCandidateError)
	if err == nil {
		t.Error("no candidates: expected error, got nil")
	}

	// first succeeds
	id, err := lifecycleconsumer.TryResumeCandidates([]string{"a", "b"}, "fallback", func(s string) error {
		return nil
	}, lifecycleconsumer.IsHistoricalResumeCandidateError)
	if err != nil || id != "a" {
		t.Errorf("first succeeds: got %q, %v; want 'a', nil", id, err)
	}

	// first fails with candidate error, second succeeds
	calls := 0
	id, err = lifecycleconsumer.TryResumeCandidates([]string{"a", "b"}, "fallback", func(s string) error {
		calls++
		if calls == 1 {
			return errors.New("no rollout found for thread id")
		}
		return nil
	}, lifecycleconsumer.IsHistoricalResumeCandidateError)
	if err != nil || id != "b" {
		t.Errorf("skip+succeed: got %q, %v; want 'b', nil", id, err)
	}

	// non-candidate error → immediate return
	id, err = lifecycleconsumer.TryResumeCandidates([]string{"a", "b"}, "fallback", func(s string) error {
		return errors.New("network timeout")
	}, lifecycleconsumer.IsHistoricalResumeCandidateError)
	if err == nil {
		t.Error("non-candidate error: expected error, got nil")
	}
	if id != "" {
		t.Errorf("non-candidate error: got id=%q, want empty", id)
	}

	// all candidate errors → error
	_, err = lifecycleconsumer.TryResumeCandidates([]string{"a", "b"}, "fallback", func(s string) error {
		return fmt.Errorf("no rollout found for thread id %s", s)
	}, lifecycleconsumer.IsHistoricalResumeCandidateError)
	if err == nil {
		t.Error("all exhausted: expected error, got nil")
	}
}

// ========================================
// previewResumeCandidates
// ========================================

func TestPreviewResumeCandidates(t *testing.T) {
	if got := lifecycleconsumer.PreviewResumeCandidates(nil, 3); got != nil {
		t.Errorf("nil: got %v, want nil", got)
	}
	if got := lifecycleconsumer.PreviewResumeCandidates([]string{"a", "b"}, 5); len(got) != 2 {
		t.Errorf("under: got %d, want 2", len(got))
	}
	got := lifecycleconsumer.PreviewResumeCandidates([]string{"a", "b", "c", "d"}, 2)
	if len(got) != 3 || got[2] != "...+2 more" {
		t.Errorf("over: got %v, want [a b ...+2 more]", got)
	}
}

// ========================================
// isHistoricalResumeCandidateError
// ========================================

func TestIsHistoricalResumeCandidateError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"no_rollout", errors.New("no rollout found for thread id abc"), true},
		{"failed_load", errors.New("failed to load rollout"), true},
		{"invalid_id", errors.New("invalid thread id xyz"), true},
		{"empty_resume", errors.New("thread/resume returned empty thread id"), true},
		{"ws_close", errors.New("websocket: close 1006"), true},
		{"abnormal", errors.New("abnormal closure"), true},
		{"random", errors.New("network error"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lifecycleconsumer.IsHistoricalResumeCandidateError(tt.err)
			if got != tt.want {
				t.Errorf("isHistoricalResumeCandidateError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ========================================
// isCodexProcessCrashError
// ========================================

func TestIsCodexProcessCrashError(t *testing.T) {
	if lifecycleconsumer.IsCodexProcessCrashError(nil) {
		t.Error("nil should be false")
	}
	if !lifecycleconsumer.IsCodexProcessCrashError(errors.New("websocket: close 1006")) {
		t.Error("ws close 1006 should be true")
	}
	if !lifecycleconsumer.IsCodexProcessCrashError(errors.New("abnormal closure")) {
		t.Error("abnormal closure should be true")
	}
	if lifecycleconsumer.IsCodexProcessCrashError(errors.New("random error")) {
		t.Error("random should be false")
	}
}

// ========================================
// extractInputs
// ========================================

func TestExtractInputs(t *testing.T) {
	inputs := []UserInput{
		{Type: "text", Text: "hello"},
		{Type: "text", Text: "world"},
		{Type: "image", URL: "https://example.com/img.png"},
		{Type: "localImage", Path: "/tmp/photo.jpg"},
		{Type: "mention", Path: "/src/main.go"},
		{Type: "skill", Name: "mySkill", Content: "ignored"},
		{Type: "fileContent", Path: "/path/to/file.go"},
	}
	prompt, images, files := extractInputs(inputs)
	if !strings.Contains(prompt, "hello") || !strings.Contains(prompt, "world") {
		t.Errorf("prompt missing text: %q", prompt)
	}
	if len(images) != 2 {
		t.Errorf("images = %d, want 2", len(images))
	}
	if len(files) != 2 { // mention + fileContent
		t.Errorf("files = %d, want 2", len(files))
	}
}

func TestExtractInputsEmpty(t *testing.T) {
	prompt, images, files := extractInputs(nil)
	if prompt != "" || len(images) != 0 || len(files) != 0 {
		t.Errorf("empty: prompt=%q, images=%d, files=%d", prompt, len(images), len(files))
	}
}

// ========================================
// buildAttachmentName
// ========================================

func TestBuildAttachmentName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"local_file", "/Users/foo/bar/baz.go", "baz.go"},
		{"url", "https://example.com/path/image.png", "image.png"},
		{"data_uri", "data:image/png;base64,abc", "image.png"},
		{"data_uri_no_ext", "data:image/;base64,abc", "image"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildAttachmentName(tt.path)
			if got != tt.want {
				t.Errorf("buildAttachmentName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// ========================================
// buildAttachmentPreviewURL
// ========================================

func TestBuildAttachmentPreviewURL(t *testing.T) {
	// http
	if got := buildAttachmentPreviewURL("https://foo.com/img.png"); got != "https://foo.com/img.png" {
		t.Errorf("http: got %q", got)
	}
	// data:
	if got := buildAttachmentPreviewURL("data:image/png;base64,abc"); got != "data:image/png;base64,abc" {
		t.Errorf("data: got %q", got)
	}
	// local file → file://
	got := buildAttachmentPreviewURL("/tmp/photo.jpg")
	if !strings.HasPrefix(got, "file://") {
		t.Errorf("local: got %q, want file:// prefix", got)
	}
	// empty
	if got := buildAttachmentPreviewURL(""); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

// ========================================
// appendUniqueThreadID
// ========================================

func TestAppendUniqueThreadID(t *testing.T) {
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	seen := make(map[string]struct{})
	// valid UUID
	dst := appendUniqueThreadID(nil, seen, uuid)
	if len(dst) != 1 {
		t.Errorf("first: got %d, want 1", len(dst))
	}
	// duplicate → no append
	dst = appendUniqueThreadID(dst, seen, uuid)
	if len(dst) != 1 {
		t.Errorf("dup: got %d, want 1", len(dst))
	}
	// non-UUID → no append
	dst = appendUniqueThreadID(dst, seen, "not-a-uuid")
	if len(dst) != 1 {
		t.Errorf("non-uuid: got %d, want 1", len(dst))
	}
}

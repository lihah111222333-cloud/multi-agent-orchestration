package apiserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterMethods_IncludesUICodeOpen(t *testing.T) {
	t.Parallel()
	srv := New(Deps{})
	if _, ok := srv.methods["ui/code/open"]; !ok {
		t.Fatal("ui/code/open should be registered in apiserver methods map")
	}
}

func TestResolveCodeReferenceFilePath_RelativeWithProject(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "internal", "codex", "client.go")
	if err := ensureTestFile(target, "package codex\n"); err != nil {
		t.Fatalf("ensureTestFile failed: %v", err)
	}

	got, err := resolveCodeReferenceFilePath("internal/codex/client.go", root, nil)
	if err != nil {
		t.Fatalf("resolveCodeReferenceFilePath failed: %v", err)
	}
	if got != target {
		t.Fatalf("resolved path = %q, want %q", got, target)
	}
}

func TestUICodeOpenTyped_ReturnsSnippetAroundLine(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "pkg", "demo.go")
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := ensureTestFile(target, content); err != nil {
		t.Fatalf("ensureTestFile failed: %v", err)
	}

	srv := New(Deps{})
	out, err := srv.uiCodeOpenTyped(context.Background(), uiCodeOpenParams{
		FilePath: "pkg/demo.go",
		Line:     3,
		Context:  1,
		Project:  root,
	})
	if err != nil {
		t.Fatalf("uiCodeOpenTyped failed: %v", err)
	}
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", out)
	}
	if got := intFromAny(result["line"]); got != 3 {
		t.Fatalf("line = %d, want 3", got)
	}
	if got := intFromAny(result["startLine"]); got != 2 {
		t.Fatalf("startLine = %d, want 2", got)
	}
	if got := intFromAny(result["endLine"]); got != 4 {
		t.Fatalf("endLine = %d, want 4", got)
	}
	snippet, ok := result["snippet"].([]map[string]any)
	if !ok || len(snippet) != 3 {
		t.Fatalf("snippet length = %d, want 3", len(snippet))
	}
	if text := asString(snippet[1]["text"]); text != "line3" {
		t.Fatalf("focused line text = %q, want line3", text)
	}
}

func TestUICodeOpenTyped_LargeLogFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "logs", "agent-terminal-2026-02-21.log")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir logs failed: %v", err)
	}

	const totalLines = 50000
	const targetLine = 32123
	linePayload := strings.Repeat("x", 64)
	var builder strings.Builder
	for i := 1; i <= totalLines; i++ {
		_, _ = fmt.Fprintf(&builder, "entry-%06d %s\n", i, linePayload)
	}
	if err := os.WriteFile(target, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("write large log failed: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat large log failed: %v", err)
	}
	if info.Size() <= 2<<20 {
		t.Fatalf("large log size = %d, want > 2MB", info.Size())
	}

	srv := New(Deps{})
	out, err := srv.uiCodeOpenTyped(context.Background(), uiCodeOpenParams{
		FilePath: "logs/agent-terminal-2026-02-21.log",
		Line:     targetLine,
		Context:  1,
		Project:  root,
	})
	if err != nil {
		t.Fatalf("uiCodeOpenTyped failed for large log: %v", err)
	}
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", out)
	}
	if got := intFromAny(result["line"]); got != targetLine {
		t.Fatalf("line = %d, want %d", got, targetLine)
	}
	snippet, ok := result["snippet"].([]map[string]any)
	if !ok || len(snippet) == 0 {
		t.Fatalf("snippet length = %d, want > 0", len(snippet))
	}
	expectedPrefix := fmt.Sprintf("entry-%06d ", targetLine)
	hit := false
	for _, row := range snippet {
		if strings.HasPrefix(asString(row["text"]), expectedPrefix) {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("focused snippet line with prefix %q not found", expectedPrefix)
	}
	if opened, _ := result["lspOpened"].(bool); opened {
		t.Fatalf("lspOpened = true for .log file, want false")
	}
}

func TestUICodeOpenTyped_FileNotFound(t *testing.T) {
	t.Parallel()
	srv := New(Deps{})
	_, err := srv.uiCodeOpenTyped(context.Background(), uiCodeOpenParams{
		FilePath: "missing/not_found.go",
		Project:  t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUICodeOpenTyped_ImageExtensionsUseImageParser(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cases := []struct {
		name      string
		relPath   string
		content   []byte
		mediaType string
	}{
		{
			name:    "png",
			relPath: "assets/preview.png",
			content: []byte{
				0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
				0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
				0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
				0x08, 0x02, 0x00, 0x00, 0x00,
			},
			mediaType: "image/png",
		},
		{
			name:    "jpg",
			relPath: "assets/preview.jpg",
			content: []byte{
				0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00,
				0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
				0xff, 0xd9,
			},
			mediaType: "image/jpeg",
		},
		{
			name:    "jpeg",
			relPath: "assets/preview.jpeg",
			content: []byte{
				0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00,
				0x01, 0x02, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
				0xff, 0xd9,
			},
			mediaType: "image/jpeg",
		},
		{
			name:      "svg",
			relPath:   "assets/preview.svg",
			content:   []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><rect width="40" height="40" fill="#222"/></svg>`),
			mediaType: "image/svg+xml",
		},
	}

	srv := New(Deps{})
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			target := filepath.Join(root, filepath.FromSlash(tc.relPath))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatalf("mkdir failed: %v", err)
			}
			if err := os.WriteFile(target, tc.content, 0o644); err != nil {
				t.Fatalf("write image file failed: %v", err)
			}

			out, err := srv.uiCodeOpenTyped(context.Background(), uiCodeOpenParams{
				FilePath: tc.relPath,
				Project:  root,
			})
			if err != nil {
				t.Fatalf("uiCodeOpenTyped failed: %v", err)
			}
			result, ok := out.(map[string]any)
			if !ok {
				t.Fatalf("result type = %T, want map[string]any", out)
			}
			if got, _ := result["image"].(bool); !got {
				t.Fatal("image flag = false, want true")
			}
			if got := asString(result["plugin"]); got != "image-parser" {
				t.Fatalf("plugin = %q, want image-parser", got)
			}
			if got := asString(result["mediaType"]); got != tc.mediaType {
				t.Fatalf("mediaType = %q, want %q", got, tc.mediaType)
			}
			expectedDataPrefix := "data:" + tc.mediaType + ";base64,"
			if previewURL := asString(result["previewURL"]); !strings.HasPrefix(previewURL, expectedDataPrefix) {
				t.Fatalf("previewURL = %q, want prefix %q", previewURL, expectedDataPrefix)
			}
			if thumbURL := asString(result["thumbnailURL"]); !strings.HasPrefix(thumbURL, expectedDataPrefix) {
				t.Fatalf("thumbnailURL = %q, want prefix %q", thumbURL, expectedDataPrefix)
			}
			snippet, ok := result["snippet"].([]map[string]any)
			if !ok || len(snippet) != 1 {
				t.Fatalf("snippet length = %d, want 1", len(snippet))
			}
			lineText := asString(snippet[0]["text"])
			if !strings.Contains(lineText, "image preview") {
				t.Fatalf("snippet text = %q, want image preview placeholder", lineText)
			}
		})
	}
}

func ensureTestFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

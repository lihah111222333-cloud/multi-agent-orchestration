package apiserver

import (
	"context"
	"os"
	"path/filepath"
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

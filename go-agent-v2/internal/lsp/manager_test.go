package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathToURI_PreservesFileURI(t *testing.T) {
	raw := "file:///tmp/a%20b.go"
	if got := pathToURI(raw); got != raw {
		t.Fatalf("pathToURI(%q) = %q, want %q", raw, got, raw)
	}
}

func TestPathToURI_EncodesSpecialChars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dir with space#hash")
	path := filepath.Join(dir, "a b#c.go")
	uri := pathToURI(path)
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("uri %q does not start with file://", uri)
	}
	if !strings.Contains(uri, "%20") {
		t.Fatalf("uri %q should encode spaces", uri)
	}
	if !strings.Contains(uri, "%23") {
		t.Fatalf("uri %q should encode #", uri)
	}
}

func TestStopAll_ContextRenewed(t *testing.T) {
	m := NewManager(nil)
	m.StopAll()
	// StopAll 后 context 应该被重建，不应该是已取消状态
	if m.ctx.Err() != nil {
		t.Fatal("context should be renewed after StopAll, got:", m.ctx.Err())
	}
}

func TestReload_ContextRenewed(t *testing.T) {
	m := NewManager(nil)
	m.Reload()
	if m.ctx.Err() != nil {
		t.Fatal("context should be valid after Reload, got:", m.ctx.Err())
	}
}

func TestNewManager_DefaultConfigs(t *testing.T) {
	m := NewManager(nil)
	if len(m.configs) == 0 {
		t.Fatal("expected default configs to be loaded")
	}
	cfg, ok := m.configs["go"]
	if !ok {
		t.Fatal("expected 'go' extension in configs")
	}
	if cfg.Language != "go" {
		t.Fatalf("got language %q, want 'go'", cfg.Language)
	}
}

func TestManager_OpenFileUnsupportedExt(t *testing.T) {
	m := NewManager(nil)
	if err := m.OpenFile("/tmp/test.xyz", "content"); err == nil {
		t.Fatal("expected error for unsupported ext, got nil")
	}
}

func TestStopAll_ThenReload_Works(t *testing.T) {
	m := NewManager(nil)
	m.StopAll()
	m.Reload()
	if m.ctx.Err() != nil {
		t.Fatal("context should be valid after StopAll+Reload")
	}
}

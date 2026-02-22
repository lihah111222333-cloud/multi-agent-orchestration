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

func TestEffectiveRootURI_UsesWorkspaceFallback(t *testing.T) {
	workspace := t.TempDir()

	got := effectiveRootURI("", workspace)
	want := pathToURI(workspace)
	if got != want {
		t.Fatalf("effectiveRootURI fallback = %q, want %q", got, want)
	}

	explicit := "file:///tmp/explicit-root"
	if got := effectiveRootURI(explicit, workspace); got != explicit {
		t.Fatalf("effectiveRootURI should keep explicit root: got %q, want %q", got, explicit)
	}
}

func TestSetRootURI_ChangeClearsClientsAndResetsDocumentState(t *testing.T) {
	m := NewManager(nil)

	m.mu.Lock()
	m.rootURI = "file:///tmp/old-root"
	m.clients["go"] = NewClient("go")
	m.mu.Unlock()

	uri := pathToURI("/tmp/test.go")
	state := m.documentState(uri)
	state.Open = true
	state.Version = 9

	m.SetRootURI("file:///tmp/new-root")

	m.mu.RLock()
	gotRoot := m.rootURI
	clientCount := len(m.clients)
	m.mu.RUnlock()
	if gotRoot != "file:///tmp/new-root" {
		t.Fatalf("rootURI = %q, want %q", gotRoot, "file:///tmp/new-root")
	}
	if clientCount != 0 {
		t.Fatalf("expected clients to be cleared on rootURI change, got %d", clientCount)
	}
	if state.Open {
		t.Fatal("document state should be marked closed after rootURI change")
	}
	if state.Version != 0 {
		t.Fatalf("document state version = %d, want 0 after rootURI change", state.Version)
	}
}

func TestSetRootURI_SameValueKeepsClients(t *testing.T) {
	m := NewManager(nil)

	m.mu.Lock()
	m.rootURI = "file:///tmp/same-root"
	m.clients["go"] = NewClient("go")
	m.mu.Unlock()

	m.SetRootURI("file:///tmp/same-root")

	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.clients) != 1 {
		t.Fatalf("expected clients to stay unchanged when rootURI is same, got %d", len(m.clients))
	}
}

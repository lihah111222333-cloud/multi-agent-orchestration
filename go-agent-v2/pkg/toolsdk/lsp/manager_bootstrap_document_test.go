package lsp

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type countingWriteCloser struct {
	mu     sync.Mutex
	writes int
}

func (c *countingWriteCloser) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes++
	c.mu.Unlock()
	return len(p), nil
}

func (c *countingWriteCloser) Close() error { return nil }

func (c *countingWriteCloser) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes
}

type failingWriteCloser struct {
	err error
}

func (f *failingWriteCloser) Write(_ []byte) (int, error) {
	if f.err == nil {
		f.err = errors.New("write failed")
	}
	return 0, f.err
}

func (f *failingWriteCloser) Close() error { return nil }

func newRunningStubClient(language string, wc *countingWriteCloser) *Client {
	cli := NewClient(language)
	cli.stdin = wc
	cli.cmd = &exec.Cmd{Process: &os.Process{Pid: 1}}
	return cli
}

func setTestEnvForBootstrapDoc(t *testing.T, key, value string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set env %s: %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestBootstrapDocument_DoesNotOverwriteUnsavedDidChangeContent(t *testing.T) {
	m := NewManager(nil)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "main.go")
	oldContent := "package main\nfunc oldName() {}\n"
	newContent := "package main\nfunc newName() {}\n"
	if err := os.WriteFile(filePath, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	wc := &countingWriteCloser{}
	stub := newRunningStubClient("go", wc)

	m.mu.Lock()
	m.clients["go"] = stub
	m.mu.Unlock()

	if err := m.OpenFile(filePath, oldContent); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if err := m.ChangeFile(filePath, 2, newContent); err != nil {
		t.Fatalf("ChangeFile: %v", err)
	}

	uri := pathToURI(filePath)
	state := m.documentState(uri)
	if got, want := state.ContentHash, hashBytes([]byte(newContent)); got != want {
		t.Fatalf("state hash before bootstrap = %q, want %q", got, want)
	}
	if state.DiskBacked {
		t.Fatal("state should be in-memory-backed after ChangeFile on unsaved content")
	}

	beforeWrites := wc.Count()
	if err := m.BootstrapDocument(filePath); err != nil {
		t.Fatalf("BootstrapDocument: %v", err)
	}
	afterWrites := wc.Count()
	if afterWrites != beforeWrites {
		t.Fatalf("bootstrap should not push disk snapshot over unsaved content: writes before=%d after=%d", beforeWrites, afterWrites)
	}

	state = m.documentState(uri)
	if got, want := state.ContentHash, hashBytes([]byte(newContent)); got != want {
		t.Fatalf("state hash after bootstrap = %q, want %q", got, want)
	}
	if state.DiskBacked {
		t.Fatal("state should remain in-memory-backed while disk content is old")
	}
}

func TestWorkspaceSymbol_LanguageOnlyBootstrap(t *testing.T) {
	m := NewManager([]ServerConfig{
		{
			Language:   "go",
			Command:    "__definitely_missing_gopls__",
			Extensions: []string{"go"},
		},
	})

	_, err := m.WorkspaceSymbol("", "go", "Foo")
	if err == nil {
		t.Fatal("expected error because command is missing")
	}
	if !strings.Contains(err.Error(), "__definitely_missing_gopls__") {
		t.Fatalf("expected error to come from ensureClientForLanguage, got: %v", err)
	}
}

func TestCacheDisabled_DefaultBehavior_NoPersistedCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache-disabled")
	setTestEnvForBootstrapDoc(t, envLSPCacheEnabled, "false")
	setTestEnvForBootstrapDoc(t, envLSPCacheDir, cacheDir)

	m := NewManager(nil)
	if m.cache == nil {
		t.Fatal("cache store should be initialized")
	}
	if m.cache.Enabled() {
		t.Fatal("cache must be disabled by default/flag")
	}

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "main.go")
	content := "package main\nfunc main(){}\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	wc := &countingWriteCloser{}
	stub := newRunningStubClient("go", wc)
	m.mu.Lock()
	m.clients["go"] = stub
	m.mu.Unlock()

	if err := m.OpenFile(filePath, content); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if writes := wc.Count(); writes == 0 {
		t.Fatal("open file should still notify lsp when cache disabled")
	}
	if _, ok := m.cache.Load(m.cacheWorkspaceHint(filePath), "go", pathToURI(filePath)); ok {
		t.Fatal("disabled cache must not return entries")
	}
	if _, err := os.Stat(cacheDir); err == nil {
		t.Fatalf("disabled cache should not create cache dir: %s", cacheDir)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat cache dir: %v", err)
	}
}

func TestCacheFailClosed_OpenSyncFailureDoesNotUpsert(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache-fail-closed")
	setTestEnvForBootstrapDoc(t, envLSPCacheEnabled, "true")
	setTestEnvForBootstrapDoc(t, envLSPCacheDir, cacheDir)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "main.go")
	content := "package main\nfunc main(){}\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	m := NewManager(nil)
	cli := NewClient("go")
	cli.stdin = &failingWriteCloser{}
	cli.cmd = &exec.Cmd{Process: &os.Process{Pid: 1}}
	m.mu.Lock()
	m.clients["go"] = cli
	m.mu.Unlock()

	err := m.OpenFile(filePath, content)
	if err == nil {
		t.Fatal("expected OpenFile to fail when didOpen write fails")
	}

	uri := pathToURI(filePath)
	if _, ok := m.cache.Load(m.cacheWorkspaceHint(filePath), "go", uri); ok {
		t.Fatal("failed sync must not upsert cache record")
	}
}

func TestCacheRecover_BootstrapStillChecksDiskFreshness(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "lsp-cache")
	setTestEnvForBootstrapDoc(t, envLSPCacheEnabled, "true")
	setTestEnvForBootstrapDoc(t, envLSPCacheDir, cacheDir)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "main.go")
	oldContent := "package main\nfunc oldName() {}\n"
	newContent := "package main\nfunc newName() {}\n"
	if err := os.WriteFile(filePath, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	m1 := NewManager(nil)
	wc1 := &countingWriteCloser{}
	stub1 := newRunningStubClient("go", wc1)
	m1.mu.Lock()
	m1.clients["go"] = stub1
	m1.mu.Unlock()
	if err := m1.OpenFile(filePath, oldContent); err != nil {
		t.Fatalf("first manager OpenFile: %v", err)
	}

	uri := pathToURI(filePath)
	if record, ok := m1.cache.Load(m1.cacheWorkspaceHint(filePath), "go", uri); !ok {
		t.Fatal("expected record persisted by first manager")
	} else if record.ContentHash != hashBytes([]byte(oldContent)) {
		t.Fatalf("persisted content hash = %q, want %q", record.ContentHash, hashBytes([]byte(oldContent)))
	}

	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte(newContent), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	m2 := NewManager(nil)
	wc2 := &countingWriteCloser{}
	stub2 := newRunningStubClient("go", wc2)
	m2.mu.Lock()
	m2.clients["go"] = stub2
	m2.mu.Unlock()

	if err := m2.BootstrapDocument(filePath); err != nil {
		t.Fatalf("BootstrapDocument with cache baseline: %v", err)
	}

	state := m2.documentState(uri)
	if state.Version <= 1 {
		t.Fatalf("expected recovered baseline to advance version after stale disk sync, got %d", state.Version)
	}
	if got, want := state.ContentHash, hashBytes([]byte(newContent)); got != want {
		t.Fatalf("state hash after recover bootstrap = %q, want %q", got, want)
	}
	if got, want := state.Content, newContent; got != want {
		t.Fatalf("state content after recover bootstrap = %q, want %q", got, want)
	}
	if !state.DiskBacked {
		t.Fatal("state should be disk-backed after recover bootstrap")
	}
	if writes := wc2.Count(); writes != 2 {
		t.Fatalf("expected exactly one didOpen frame pair (header+body) after restart recover, writes=%d", writes)
	}
	if record, ok := m2.cache.Load(m2.cacheWorkspaceHint(filePath), "go", uri); !ok {
		t.Fatal("expected refreshed cache record after bootstrap")
	} else if record.ContentHash != hashBytes([]byte(newContent)) {
		t.Fatalf("refreshed cache hash = %q, want %q", record.ContentHash, hashBytes([]byte(newContent)))
	}
}

func TestOpenFileAllowsMarkdownWithoutLanguageServer(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil)

	mdPath := filepath.Join(t.TempDir(), "review.md")
	content := "# Title\n\n- item\n"

	if err := mgr.OpenFile(mdPath, content); err != nil {
		t.Fatalf("OpenFile(.md) error = %v", err)
	}

	uri := pathToURI(mdPath)
	state := mgr.documentState(uri)
	if !state.Open {
		t.Fatalf("expected markdown file to be marked open")
	}
	if state.Language != "markdown" {
		t.Fatalf("state.Language = %q, want %q", state.Language, "markdown")
	}
	if state.Content != content {
		t.Fatalf("state.Content mismatch")
	}
	if state.Version != 1 {
		t.Fatalf("state.Version = %d, want 1", state.Version)
	}

	mgr.mu.RLock()
	clientCount := len(mgr.clients)
	mgr.mu.RUnlock()
	if clientCount != 0 {
		t.Fatalf("expected no LSP clients started for markdown, got %d", clientCount)
	}
}

func TestOpenFileAllowsDotMarkdownWithoutLanguageServer(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil)
	markdownPath := filepath.Join(t.TempDir(), "notes.markdown")

	if err := mgr.OpenFile(markdownPath, "content"); err != nil {
		t.Fatalf("OpenFile(.markdown) error = %v", err)
	}
}

func TestBootstrapDocumentAllowsMarkdownWithoutLanguageServer(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil)
	mdPath := filepath.Join(t.TempDir(), "workflow.md")
	content := "# Title\n\n## Section\n"
	if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write markdown file: %v", err)
	}

	if err := mgr.BootstrapDocument(mdPath); err != nil {
		t.Fatalf("BootstrapDocument(.md) error = %v", err)
	}

	uri := pathToURI(mdPath)
	state := mgr.documentState(uri)
	if !state.Open {
		t.Fatal("expected markdown document state to be open after bootstrap")
	}
	if state.Language != "markdown" {
		t.Fatalf("state.Language = %q, want markdown", state.Language)
	}
	if state.Content != content {
		t.Fatalf("state.Content mismatch")
	}

	mgr.mu.RLock()
	clientCount := len(mgr.clients)
	mgr.mu.RUnlock()
	if clientCount != 0 {
		t.Fatalf("expected no LSP clients started for markdown bootstrap, got %d", clientCount)
	}
}

func TestDocumentSymbolMarkdownFallbackWithoutLanguageServer(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil)
	mdPath := filepath.Join(t.TempDir(), "outline.md")
	content := "# Root\n\n## Child A\n\n### Grandchild\n\nSubtopic\n---\n"
	if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write markdown file: %v", err)
	}

	symbols, err := mgr.DocumentSymbol(mdPath)
	if err != nil {
		t.Fatalf("DocumentSymbol(.md) error = %v", err)
	}
	if len(symbols) != 1 {
		t.Fatalf("expected one root heading symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "Root" {
		t.Fatalf("root symbol name = %q, want Root", symbols[0].Name)
	}
	if len(symbols[0].Children) < 2 {
		t.Fatalf("expected Root to have at least two children, got %d", len(symbols[0].Children))
	}
	if symbols[0].Children[0].Name != "Child A" {
		t.Fatalf("first child symbol name = %q, want Child A", symbols[0].Children[0].Name)
	}
	if len(symbols[0].Children[0].Children) != 1 || symbols[0].Children[0].Children[0].Name != "Grandchild" {
		t.Fatalf("expected Child A -> Grandchild hierarchy, got %#v", symbols[0].Children[0].Children)
	}

	foundSubtopic := false
	for _, child := range symbols[0].Children {
		if child.Name == "Subtopic" {
			foundSubtopic = true
			break
		}
	}
	if !foundSubtopic {
		t.Fatalf("expected setext heading Subtopic in root children, got %#v", symbols[0].Children)
	}

	mgr.mu.RLock()
	clientCount := len(mgr.clients)
	mgr.mu.RUnlock()
	if clientCount != 0 {
		t.Fatalf("expected no LSP clients started for markdown symbols, got %d", clientCount)
	}
}

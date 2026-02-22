package lsp

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeLanguage_Aliases(t *testing.T) {
	cases := map[string]string{
		"ts":         "typescript",
		"javascript": "typescript",
		"golang":     "go",
		"rs":         "rust",
		"py":         "python",
		"cpp":        "c",
	}
	for in, want := range cases {
		if got := normalizeLanguage(in); got != want {
			t.Fatalf("normalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkspaceSymbolResolution_Rules(t *testing.T) {
	m := NewManager(nil)

	t.Run("file-path-only", func(t *testing.T) {
		got, err := m.resolveWorkspaceSymbolLanguage("/tmp/demo.go", "")
		if err != nil {
			t.Fatalf("resolveWorkspaceSymbolLanguage(file only) error = %v", err)
		}
		if got != "go" {
			t.Fatalf("got %q, want go", got)
		}
	})

	t.Run("language-only", func(t *testing.T) {
		got, err := m.resolveWorkspaceSymbolLanguage("", "javascript")
		if err != nil {
			t.Fatalf("resolveWorkspaceSymbolLanguage(language only) error = %v", err)
		}
		if got != "typescript" {
			t.Fatalf("got %q, want typescript", got)
		}
	})

	t.Run("file-and-language-mutually-exclusive", func(t *testing.T) {
		_, err := m.resolveWorkspaceSymbolLanguage("/tmp/demo.go", "go")
		if err == nil {
			t.Fatal("expected mutually exclusive error, got nil")
		}
		if got := err.Error(); got == "" || !containsAll(got, "mutually", "exclusive") {
			t.Fatalf("expected mutually exclusive error, got %v", err)
		}
	})

	t.Run("file-not-inferable", func(t *testing.T) {
		_, err := m.resolveWorkspaceSymbolLanguage("/tmp/README", "")
		if err == nil {
			t.Fatal("expected cannot infer error, got nil")
		}
		if got := err.Error(); got == "" || !containsAll(got, "cannot infer language") {
			t.Fatalf("expected cannot infer error, got %v", err)
		}
	})
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}

func TestBootstrapAtomic_DocumentLockSerializesSameURI(t *testing.T) {
	m := NewManager(nil)
	uri := "file:///tmp/demo.go"

	var inFlight int32
	var maxInFlight int32

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			lock := m.documentLock(uri)
			lock.Lock()
			current := atomic.AddInt32(&inFlight, 1)
			for {
				max := atomic.LoadInt32(&maxInFlight)
				if current <= max || atomic.CompareAndSwapInt32(&maxInFlight, max, current) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			lock.Unlock()
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got != 1 {
		t.Fatalf("expected max concurrent holders = 1, got %d", got)
	}
}

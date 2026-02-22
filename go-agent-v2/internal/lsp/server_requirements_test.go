package lsp

import (
	"os"
	"os/exec"
	"sync"
	"testing"
)

var (
	serverProbeMu      sync.Mutex
	serverProbeResults = map[string]error{}
)

func requireServerOrSkip(t *testing.T, language string) {
	t.Helper()

	normalized := normalizeLanguage(language)
	cmd := lspServerCommandForLanguage(normalized)
	if cmd == "" {
		t.Fatalf("unsupported language for requireServerOrSkip: %q", language)
	}
	if _, err := exec.LookPath(cmd); err != nil {
		t.Skipf("%s not found for language %q, skipping", cmd, normalized)
	}

	if err := probeLanguageServerCompatibility(normalized); err != nil {
		t.Skipf("language server %q unavailable or incompatible: %v", normalized, err)
	}
}

func lspServerCommandForLanguage(language string) string {
	switch normalizeLanguage(language) {
	case "go":
		return "gopls"
	case "typescript":
		return "typescript-language-server"
	case "rust":
		return "rust-analyzer"
	default:
		return ""
	}
}

func probeLanguageServerCompatibility(language string) error {
	normalized := normalizeLanguage(language)
	serverProbeMu.Lock()
	err, ok := serverProbeResults[normalized]
	serverProbeMu.Unlock()
	if ok {
		return err
	}

	mgr := NewManager(nil)
	defer mgr.StopAll()
	if probeRoot, mkErr := os.MkdirTemp("", "lsp-probe-*"); mkErr == nil {
		defer os.RemoveAll(probeRoot)
		mgr.SetRootURI(pathToURI(probeRoot))
	}
	err = mgr.BootstrapLanguage(normalized)

	serverProbeMu.Lock()
	serverProbeResults[normalized] = err
	serverProbeMu.Unlock()

	return err
}

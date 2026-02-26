package lsp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextSearchMissingRG(t *testing.T) {
	oldLookPath := lspSearchLookPath
	oldGetwd := lspSearchGetwd
	defer func() {
		lspSearchLookPath = oldLookPath
		lspSearchGetwd = oldGetwd
	}()

	workspace := t.TempDir()
	lspSearchGetwd = func() (string, error) { return workspace, nil }
	lspSearchLookPath = func(name string) (string, error) {
		if name == "rg" {
			return "", errors.New("not found")
		}
		return oldLookPath(name)
	}

	raw, err := json.Marshal(map[string]any{"query": "needle"})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	got := (&ToolHandlers{}).TextSearch(raw)
	if !strings.Contains(got, "rg not found in PATH") {
		t.Fatalf("unexpected TextSearch output: %s", got)
	}
}

func TestAstSearchMissingSG(t *testing.T) {
	oldLookPath := lspSearchLookPath
	oldGetwd := lspSearchGetwd
	defer func() {
		lspSearchLookPath = oldLookPath
		lspSearchGetwd = oldGetwd
	}()

	workspace := t.TempDir()
	lspSearchGetwd = func() (string, error) { return workspace, nil }
	lspSearchLookPath = func(name string) (string, error) {
		if name == "sg" {
			return "", errors.New("not found")
		}
		return oldLookPath(name)
	}

	raw, err := json.Marshal(map[string]any{"pattern": "$A", "language": "go"})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	got := (&ToolHandlers{}).AstSearch(raw)
	if !strings.Contains(got, "sg not found in PATH") {
		t.Fatalf("unexpected AstSearch output: %s", got)
	}
}

func TestResolveSearchTargetRejectsTraversal(t *testing.T) {
	oldGetwd := lspSearchGetwd
	defer func() { lspSearchGetwd = oldGetwd }()

	workspace := t.TempDir()
	parent := filepath.Dir(workspace)
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}

	lspSearchGetwd = func() (string, error) { return workspace, nil }
	_, _, err := resolveSearchTarget("../outside")
	if err == nil {
		t.Fatalf("expected traversal rejection")
	}
	if !strings.Contains(err.Error(), "path out of workspace root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRipgrepVimgrepOutputLimit(t *testing.T) {
	workspace := "/repo"
	output := []byte("a.go:1:2:first\na.go:2:2:second\na.go:3:2:third\n")
	matches := parseRipgrepVimgrepOutput(output, workspace, 2)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].Path != "a.go" || matches[0].Line != 1 || matches[0].Column != 2 {
		t.Fatalf("unexpected first match: %+v", matches[0])
	}
}

func TestFilterAndCapSearchMatchesPayload(t *testing.T) {
	big := strings.Repeat("x", 1024)
	matches := make([]lspSearchMatch, 0, 30)
	for i := 0; i < 30; i++ {
		matches = append(matches, lspSearchMatch{Path: "a.go", Line: i + 1, Column: 1, Text: big})
	}
	capped := filterAndCapSearchMatches(matches)
	if len(capped) == 0 {
		t.Fatalf("expected non-empty capped matches")
	}
	data, err := json.Marshal(capped)
	if err != nil {
		t.Fatalf("marshal capped: %v", err)
	}
	if len(data) > lspSearchPayloadMax {
		t.Fatalf("payload exceeds cap: %d > %d", len(data), lspSearchPayloadMax)
	}
}

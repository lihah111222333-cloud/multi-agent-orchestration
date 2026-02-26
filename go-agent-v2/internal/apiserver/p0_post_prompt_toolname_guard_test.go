package apiserver

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func p0PostModeAPIServerPrompt() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("LSP_P0_MODE")))
	if mode == "" {
		return "pre"
	}
	return mode
}

func TestP0PostPromptToolnameGuard(t *testing.T) {
	if p0PostModeAPIServerPrompt() != "post" {
		t.Skip("skip p0-post guard when LSP_P0_MODE is not post")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))

	targets := []string{
		".agent",
		"internal/apiserver/commonadapter/skills.go",
		"internal/store/prompt_template.go",
	}
	legacyNames := []string{
		"lsp_hover",
		"lsp_open_file",
		"lsp_diagnostics",
		"lsp_definition",
		"lsp_references",
		"lsp_document_symbol",
		"lsp_rename",
		"lsp_did_change",
		"lsp_code_action",
		"lsp_signature_help",
		"lsp_format",
		"lsp_call_hierarchy",
		"lsp_type_hierarchy",
		"lsp_semantic_tokens",
		"lsp_folding_range",
		"lsp_workspace_symbol",
		"lsp_implementation",
		"lsp_type_definition",
		"lsp_text_search",
		"lsp_ast_search",
	}

	totalHits := 0
	scanFile := func(path string) error {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lower := strings.ToLower(string(content))
		for _, tool := range legacyNames {
			if strings.Contains(lower, tool) {
				totalHits++
			}
		}
		return nil
	}

	for _, rel := range targets {
		abs := filepath.Join(repoRoot, rel)
		info, statErr := os.Stat(abs)
		if statErr != nil {
			t.Fatalf("target missing: %s (%v)", rel, statErr)
		}
		if !info.IsDir() {
			if err := scanFile(abs); err != nil {
				t.Fatalf("scan file %s: %v", rel, err)
			}
			continue
		}
		walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				name := strings.ToLower(d.Name())
				if name == ".git" || name == "node_modules" || name == "vendor" || name == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			switch ext {
			case ".md", ".go", ".txt", ".json", ".yaml", ".yml":
				return scanFile(path)
			default:
				return nil
			}
		})
		if walkErr != nil {
			t.Fatalf("walk target %s: %v", rel, walkErr)
		}
	}

	if totalHits != 0 {
		t.Fatalf("p0-post expects zero legacy tool-name hits, got %d", totalHits)
	}
}

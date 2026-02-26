package apiserver

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	allowed := map[string]struct{}{
		"lsp_file":       {},
		"lsp_inspect":    {},
		"lsp_xref":       {},
		"lsp_grep":       {},
		"lsp_structure":  {},
		"lsp_edit":       {},
		"lsp_completion": {},
	}
	ignoredNonToolTokens := map[string]struct{}{
		"lsp_tools":          {},
		"lsp_ext_":           {},
		"lsp_schema_builder": {},
	}
	toolPattern := regexp.MustCompile(`\blsp_[a-z_]+\b`)
	disallowed := make(map[string]int)

	scanFile := func(path string) error {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lower := strings.ToLower(string(content))
		matches := toolPattern.FindAllString(lower, -1)
		for _, tool := range matches {
			if _, ok := allowed[tool]; ok {
				continue
			}
			if _, ok := ignoredNonToolTokens[tool]; ok {
				continue
			}
			disallowed[tool]++
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

	if len(disallowed) != 0 {
		tools := make([]string, 0, len(disallowed))
		for tool, count := range disallowed {
			tools = append(tools, tool+"("+strconv.Itoa(count)+")")
		}
		sort.Strings(tools)
		t.Fatalf("p0-post expects only merged tool names in targets, disallowed=%v", tools)
	}
}

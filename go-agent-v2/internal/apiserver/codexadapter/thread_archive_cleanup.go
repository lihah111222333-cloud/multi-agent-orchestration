package codexadapter

import (
	"os"
	"path/filepath"
	"strings"
)

// removeEmptyCodexParentDirs removes empty parent dirs until codex root boundary.
func removeEmptyCodexParentDirs(startDir string, codexRoot string) {
	current := strings.TrimSpace(startDir)
	root := strings.TrimSpace(codexRoot)
	if current == "" || root == "" {
		return
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for current != "" {
		currentAbs, err := filepath.Abs(current)
		if err != nil {
			return
		}
		if currentAbs == rootAbs {
			return
		}
		withinRoot, err := PathWithinRoot(rootAbs, currentAbs)
		if err != nil || !withinRoot {
			return
		}
		entries, err := os.ReadDir(currentAbs)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(currentAbs); err != nil {
			return
		}
		parent := filepath.Dir(currentAbs)
		if parent == currentAbs {
			return
		}
		current = parent
	}
}

package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
)

func FuzzyFileSearch(query string, roots []string, fuzzyMatch func(text, pattern string) bool) []map[string]any {
	query = strings.ToLower(query)
	results := make([]map[string]any, 0)
	if fuzzyMatch == nil {
		return results
	}

	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				base := filepath.Base(path)
				if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if fuzzyMatch(strings.ToLower(rel), query) {
				results = append(results, map[string]any{
					"root":     root,
					"path":     rel,
					"fileName": info.Name(),
				})
				if len(results) >= 100 {
					return filepath.SkipAll
				}
			}
			return nil
		})
	}
	return results
}

package codexadapter

// FuzzyFileSearch walks directories and returns fuzzy-matched file paths.
func (a *Adapter) FuzzyFileSearch(query string, roots []string, fuzzyMatch func(text, pattern string) bool) []map[string]any {
	return fuzzyFileSearch(query, roots, fuzzyMatch)
}

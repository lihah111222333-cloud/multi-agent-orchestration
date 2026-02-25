package codexadapter

import (
	"regexp"
	"strings"
)

// CodexThreadIDPattern matches a lowercase UUID (codex thread ID format).
var CodexThreadIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// NormalizeCodexThreadID trims, lowercases, strips "urn:uuid:" prefix,
// and validates against UUID pattern. Returns "" if invalid.
func NormalizeCodexThreadID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	id = strings.TrimPrefix(strings.ToLower(id), "urn:uuid:")
	if !CodexThreadIDPattern.MatchString(id) {
		return ""
	}
	return id
}

// IsLikelyCodexThreadID reports whether raw looks like a valid codex thread ID.
func IsLikelyCodexThreadID(raw string) bool {
	return NormalizeCodexThreadID(raw) != ""
}

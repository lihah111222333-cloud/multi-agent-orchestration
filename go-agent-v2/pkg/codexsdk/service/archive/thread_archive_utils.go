package archive

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/pathutil"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type threadArtifactCandidate struct {
	Kind string
	Path string
}

func NormalizeThreadArchiveMap(value any) map[string]int64 {
	result := map[string]int64{}
	addEntries := func(entries map[string]any) {
		for id, at := range entries {
			addThreadArchiveMapEntry(result, id, at)
		}
	}
	loadDecoded := func(raw []byte) {
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err == nil {
			addEntries(decoded)
		}
	}
	switch typed := value.(type) {
	case map[string]int64:
		for id, at := range typed {
			addThreadArchiveMapEntry(result, id, at)
		}
	case map[string]any:
		addEntries(typed)
	case string:
		loadDecoded([]byte(strings.TrimSpace(typed)))
	case json.RawMessage:
		loadDecoded(typed)
	}
	return result
}

func SanitizeArchiveName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._")
}

// SanitizeArchiveNameStrict validates sanitized archive names.
func SanitizeArchiveNameStrict(raw string) (string, error) {
	sanitized := SanitizeArchiveName(raw)
	if sanitized == "" {
		return "", apperrors.Newf("sanitizeArchiveNameStrict", "invalid archive name from %q", raw)
	}
	return sanitized, nil
}

// PathWithinRoot returns whether path is inside root (or equal root).
func PathWithinRoot(root string, path string) (bool, error) {
	rootAbs, err := pathutil.Abs(strings.TrimSpace(root))
	if err != nil {
		return false, err
	}
	pathAbs, err := pathutil.Abs(strings.TrimSpace(path))
	if err != nil {
		return false, err
	}
	rel, err := pathutil.Rel(rootAbs, pathAbs)
	if err != nil {
		return false, err
	}
	if rel = pathutil.Clean(rel); rel == "." {
		return true, nil
	}
	return !strings.HasPrefix(rel, ".."+pathutil.Separator) && rel != "..", nil
}

func addThreadArchiveMapEntry(result map[string]int64, rawID string, rawAt any) {
	id := strings.TrimSpace(rawID)
	if id == "" {
		return
	}
	if at := normalizeThreadArchiveTimestamp(rawAt); at > 0 {
		result[id] = at
	}
}

func normalizeThreadArchiveTimestamp(rawAt any) int64 {
	switch v := rawAt.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

// inferThreadArtifactKind infers the artifact kind from filename.
func inferThreadArtifactKind(filename string) string {
	lower := strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasPrefix(lower, "rollout-") && strings.HasSuffix(lower, ".jsonl"):
		return "rollout"
	case strings.Contains(lower, "bp"):
		return "breakpoint"
	case strings.HasSuffix(lower, ".sh"):
		return "shell_snapshot"
	case strings.HasSuffix(lower, ".jsonl"):
		return "jsonl"
	default:
		return "artifact"
	}
}

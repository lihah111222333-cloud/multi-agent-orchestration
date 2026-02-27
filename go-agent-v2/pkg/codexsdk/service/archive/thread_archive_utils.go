package archive

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type threadArtifactCandidate struct {
	Kind string
	Path string
}

// NormalizeThreadArchiveMap normalizes archive state payload into map[string]int64.
func NormalizeThreadArchiveMap(value any) map[string]int64 {
	return normalizeThreadArchiveMap(value)
}

// SanitizeArchiveName sanitizes archive file and directory names.
func SanitizeArchiveName(raw string) string {
	return sanitizeArchiveName(raw)
}

// SanitizeArchiveNameStrict validates sanitized archive names.
func SanitizeArchiveNameStrict(raw string) (string, error) {
	return sanitizeArchiveNameStrict(raw)
}

// PathWithinRoot returns whether path is inside root (or equal root).
func PathWithinRoot(root string, path string) (bool, error) {
	return pathWithinRoot(root, path)
}

func normalizeThreadArchiveMap(value any) map[string]int64 {
	result := map[string]int64{}

	switch typed := value.(type) {
	case map[string]int64:
		for id, at := range typed {
			addThreadArchiveMapEntry(result, id, at)
		}
	case map[string]any:
		for id, at := range typed {
			addThreadArchiveMapEntry(result, id, at)
		}
	case string:
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &decoded); err == nil {
			for id, at := range decoded {
				addThreadArchiveMapEntry(result, id, at)
			}
		}
	case json.RawMessage:
		decoded := map[string]any{}
		if err := json.Unmarshal(typed, &decoded); err == nil {
			for id, at := range decoded {
				addThreadArchiveMapEntry(result, id, at)
			}
		}
	}

	return result
}

func addThreadArchiveMapEntry(result map[string]int64, rawID string, rawAt any) {
	if result == nil {
		return
	}
	id := strings.TrimSpace(rawID)
	if id == "" {
		return
	}
	at := normalizeThreadArchiveTimestamp(rawAt)
	if at <= 0 {
		return
	}
	result[id] = at
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
		parsed, err := v.Int64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func sanitizeArchiveName(raw string) string {
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

func sanitizeArchiveNameStrict(raw string) (string, error) {
	sanitized := sanitizeArchiveName(raw)
	if sanitized == "" {
		return "", apperrors.Newf("sanitizeArchiveNameStrict", "invalid archive name from %q", raw)
	}
	return sanitized, nil
}

func pathWithinRoot(root string, path string) (bool, error) {
	rootAbs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return false, err
	}
	pathAbs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false, err
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true, nil
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..", nil
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

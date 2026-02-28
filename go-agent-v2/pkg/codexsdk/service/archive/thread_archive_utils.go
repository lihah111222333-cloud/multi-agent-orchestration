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
	addDecoded := func(raw []byte) {
		if len(raw) == 0 {
			return
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return
		}
		for id, at := range decoded {
			addThreadArchiveMapEntry(result, id, at)
		}
	}
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
		addDecoded([]byte(strings.TrimSpace(typed)))
	case json.RawMessage:
		addDecoded(typed)
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
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._")
}

func SanitizeArchiveNameStrict(raw string) (string, error) {
	sanitized := SanitizeArchiveName(raw)
	if sanitized == "" {
		return "", apperrors.Newf("sanitizeArchiveNameStrict", "invalid archive name from %q", raw)
	}
	return sanitized, nil
}

func PathWithinRoot(root, path string) (bool, error) {
	root, path = strings.TrimSpace(root), strings.TrimSpace(path)
	if root == "" || path == "" {
		return false, apperrors.New("PathWithinRoot", "root and path are required")
	}
	rootAbs, err := pathutil.Abs(root)
	if err != nil {
		return false, err
	}
	pathAbs, err := pathutil.Abs(path)
	if err != nil {
		return false, err
	}
	rel, err := pathutil.Rel(rootAbs, pathAbs)
	if err != nil {
		return false, err
	}
	rel = pathutil.Clean(rel)
	return rel == "." || (!strings.HasPrefix(rel, ".."+pathutil.Separator) && rel != ".."), nil
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

package codexadapter

import (
	"encoding/json"
	"strconv"
	"strings"
)

// NormalizeThreadArchiveMap normalizes archive state payload into map[string]int64.
func NormalizeThreadArchiveMap(value any) map[string]int64 {
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

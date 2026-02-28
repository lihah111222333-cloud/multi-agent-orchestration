package util

import (
	"fmt"
	"strings"
)

func AsString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func AsStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(item); text != "" {
				items = append(items, text)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				if text = strings.TrimSpace(text); text != "" {
					items = append(items, text)
				}
			}
		}
		return items
	default:
		return nil
	}
}

func ExtractFirstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := payload[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

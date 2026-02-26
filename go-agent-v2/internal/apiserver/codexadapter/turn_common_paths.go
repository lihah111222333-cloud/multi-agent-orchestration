package codexadapter

import "strings"

func collectTrimmedUniqueValues(values []string, keyFn func(string) string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		key := value
		if keyFn != nil {
			key = strings.TrimSpace(keyFn(value))
			if key == "" {
				key = value
			}
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

package apiserver

import "strings"

func normalizeFiles(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		value := strings.TrimSpace(v)
		if value == "" {
			return nil
		}
		return []string{value}
	case []string:
		return uniqueStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return uniqueStrings(out)
	default:
		return nil
	}
}

func uniqueStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		s := strings.TrimSpace(item)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func parseFilesFromPatchDelta(delta string) []string {
	if delta == "" {
		return nil
	}
	lines := strings.Split(delta, "\n")
	files := make([]string, 0, 4)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "diff --git ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 4 {
				path := strings.TrimPrefix(parts[3], "b/")
				if path != "" {
					files = append(files, path)
				}
			}
			continue
		}
		if len(trimmed) > 2 && (strings.HasPrefix(trimmed, "M ") || strings.HasPrefix(trimmed, "A ") || strings.HasPrefix(trimmed, "D ")) {
			path := strings.TrimSpace(trimmed[2:])
			if path != "" {
				files = append(files, path)
			}
		}
	}
	return uniqueStrings(files)
}

func toolResultSuccess(result string) bool {
	value := strings.TrimSpace(strings.ToLower(result))
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "error") ||
		strings.HasPrefix(value, "failed") ||
		strings.HasPrefix(value, "unknown tool") {
		return false
	}
	if strings.HasPrefix(value, `{"error"`) ||
		strings.Contains(value, `"error":`) {
		return false
	}
	return true
}

func rememberFileChanges(s *Server, threadID string, files []string) {
	if s == nil {
		return
	}
	if threadID == "" {
		return
	}
	files = uniqueStrings(files)
	if len(files) == 0 {
		return
	}
	rememberFileChangesState(s, threadID, files)
}

func consumeRememberedFileChanges(s *Server, threadID string) []string {
	if s == nil {
		return nil
	}
	if threadID == "" {
		return nil
	}
	return consumeFileChangesState(s, threadID)
}

func enrichFileChangePayload(s *Server, threadID, eventType, method string, payload map[string]any) {
	if payload == nil {
		return
	}
	isFileChangeEvent := strings.Contains(strings.ToLower(eventType), "filechange") ||
		strings.Contains(strings.ToLower(eventType), "patch_apply")
	isFileChangeMethod := strings.Contains(method, "fileChange")
	if !isFileChangeEvent && !isFileChangeMethod {
		return
	}

	files := normalizeFiles(payload["files"])
	if len(files) == 0 {
		files = normalizeFiles(payload["file"])
	}
	if len(files) == 0 {
		delta := ""
		if value, ok := payload["delta"].(string); ok {
			delta = value
		} else if value, ok := payload["output"].(string); ok {
			delta = value
		}
		files = parseFilesFromPatchDelta(delta)
	}

	switch method {
	case "item/fileChange/outputDelta", "item/started":
		if len(files) > 0 {
			payload["files"] = files
			payload["file"] = files[0]
			payload["type"] = "fileChange"
			rememberFileChanges(s, threadID, files)
		}
	case "item/completed":
		if len(files) == 0 {
			files = consumeRememberedFileChanges(s, threadID)
		}
		if len(files) > 0 {
			payload["files"] = files
			payload["file"] = files[0]
			payload["type"] = "fileChange"
		}
	}
}

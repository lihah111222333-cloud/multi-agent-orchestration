package codexadapter

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func paginateLoadedThreadIDs(ids []string, cursor *string, limit *uint32) ([]string, *string) {
	if len(ids) == 0 {
		return []string{}, nil
	}

	start := 0
	if cursor != nil {
		cursorID := strings.TrimSpace(*cursor)
		if cursorID != "" {
			start = sort.SearchStrings(ids, cursorID)
			if start < len(ids) && ids[start] == cursorID {
				start++
			}
		}
	}
	if start >= len(ids) {
		return []string{}, nil
	}

	pageSize := len(ids)
	if limit != nil {
		pageSize = int(*limit)
		if pageSize < 1 {
			pageSize = 1
		}
	}

	end := start + pageSize
	if end > len(ids) {
		end = len(ids)
	}

	page := append([]string(nil), ids[start:end]...)
	if end >= len(ids) {
		return page, nil
	}
	nextCursor := page[len(page)-1]
	return page, &nextCursor
}

func appendArchivedThreads(threads []threadListItem, seen map[string]struct{}, archived map[string]int64) []threadListItem {
	type archivedEntry struct {
		ID string
		At int64
	}
	entries := make([]archivedEntry, 0, len(archived))
	for rawID, rawAt := range archived {
		id := strings.TrimSpace(rawID)
		if id == "" || rawAt <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		entries = append(entries, archivedEntry{ID: id, At: rawAt})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].At != entries[j].At {
			return entries[i].At > entries[j].At
		}
		return entries[i].ID < entries[j].ID
	})
	for _, item := range entries {
		threads = append(threads, threadListItem{
			ID:    item.ID,
			Name:  item.ID,
			State: "idle",
		})
		seen[item.ID] = struct{}{}
	}
	return threads
}

func normalizethreadListItem(id, name string) (string, string, bool) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return "", "", false
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		trimmedName = trimmedID
	}
	return trimmedID, trimmedName, true
}

func appendthreadListItem(threads []threadListItem, seen map[string]struct{}, id, name, state string) []threadListItem {
	trimmedID, trimmedName, ok := normalizethreadListItem(id, name)
	if !ok {
		return threads
	}
	if _, exists := seen[trimmedID]; exists {
		return threads
	}
	seen[trimmedID] = struct{}{}
	return append(threads, threadListItem{
		ID:    trimmedID,
		Name:  trimmedName,
		State: state,
	})
}

// normalizeThreadAliases parses various formats (map, JSON string, json.RawMessage)
// into a normalized map[string]string of thread aliases.
func normalizeThreadAliases(value any) map[string]string {
	aliases := map[string]string{}

	switch typed := value.(type) {
	case map[string]string:
		for threadID, alias := range typed {
			addNormalizedThreadAlias(aliases, threadID, alias)
		}
	case map[string]any:
		for threadID, alias := range typed {
			addNormalizedThreadAlias(aliases, threadID, alias)
		}
	case string:
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(typed)), &decoded); err == nil {
			for threadID, alias := range decoded {
				addNormalizedThreadAlias(aliases, threadID, alias)
			}
		}
	case json.RawMessage:
		decoded := map[string]any{}
		if err := json.Unmarshal(typed, &decoded); err == nil {
			for threadID, alias := range decoded {
				addNormalizedThreadAlias(aliases, threadID, alias)
			}
		}
	}
	return aliases
}

func addNormalizedThreadAlias(aliases map[string]string, threadID string, alias any) {
	if aliases == nil {
		return
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return
	}
	name := strings.TrimSpace(stringValue(alias))
	if name == "" || name == id {
		return
	}
	aliases[id] = name
}

func applyThreadAliases(threads []threadListItem, aliases map[string]string) {
	if len(threads) == 0 || len(aliases) == 0 {
		return
	}
	for i := range threads {
		id := strings.TrimSpace(threads[i].ID)
		if id == "" {
			continue
		}
		alias := strings.TrimSpace(aliases[id])
		if alias == "" {
			continue
		}
		threads[i].Name = alias
	}
}

// stringValue extracts a string from any value (string or fmt.Stringer).
func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

package util

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

func TrimInjectedLSPHint(text string) string {
	const marker = "\n已注入"
	if idx := strings.Index(text, marker); idx >= 0 {
		return text[:idx]
	}
	return text
}

func TrimInjectedSkillBlock(text string) string {
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "[skill:") || !strings.Contains(line, "]") {
			continue
		}
		if !looksLikeInjectedSkillBlock(lines, i) {
			continue
		}
		return strings.TrimRight(strings.Join(lines[:i], "\n"), "\n")
	}
	return text
}

func looksLikeInjectedSkillBlock(lines []string, start int) bool {
	if start < 0 || start >= len(lines) {
		return false
	}
	current := strings.TrimSpace(lines[start])
	hasSummary := strings.Contains(current, "摘要:")
	hasUsage := strings.Contains(current, "使用方式: ")

	const lookahead = 8
	for i := start + 1; i < len(lines) && i <= start+lookahead; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[skill:") {
			break
		}
		if strings.HasPrefix(line, "摘要:") {
			hasSummary = true
			continue
		}
		if strings.HasPrefix(line, "使用方式: ") {
			hasUsage = true
			continue
		}
	}
	return hasSummary && hasUsage
}

func BuildAttachmentPreviewURL(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:image/") ||
		strings.HasPrefix(lower, "file://") {
		return value
	}
	return (&url.URL{Scheme: "file", Path: value}).String()
}

func BuildAttachmentName(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if ext, ok := strings.CutPrefix(lower, "data:image/"); ok {
		ext = strings.TrimSpace(ext)
		if idx := strings.Index(ext, ";"); idx >= 0 {
			ext = ext[:idx]
		}
		ext = strings.TrimSpace(ext)
		if ext == "" {
			return "image"
		}
		return "image." + ext
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if parsed, err := url.Parse(value); err == nil {
			base := strings.TrimSpace(filepath.Base(parsed.Path))
			if base != "" && base != "." && base != string(filepath.Separator) {
				return base
			}
		}
		return value
	}
	base := strings.TrimSpace(filepath.Base(value))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return value
	}
	return base
}

func ResolveCodeRunCallID(callID string, requestID *int64) string {
	trimmed := strings.TrimSpace(callID)
	if trimmed != "" {
		return trimmed
	}
	if requestID != nil {
		return fmt.Sprintf("req-%d", *requestID)
	}
	return ""
}

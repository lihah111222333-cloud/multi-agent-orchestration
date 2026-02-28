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
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[skill:") && strings.Contains(line, "]") && looksLikeInjectedSkillBlock(lines, i) {
			return strings.TrimRight(strings.Join(lines[:i], "\n"), "\n")
		}
	}
	return text
}

func looksLikeInjectedSkillBlock(lines []string, start int) bool {
	if start < 0 || start >= len(lines) {
		return false
	}
	const (
		lookahead     = 8
		summaryPrefix = "摘要:"
		usagePrefix   = "使用方式: "
	)
	current := strings.TrimSpace(lines[start])
	hasSummary := strings.Contains(current, summaryPrefix)
	hasUsage := strings.Contains(current, usagePrefix)

	for i := start + 1; i < len(lines) && i <= start+lookahead; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[skill:") {
			break
		}
		hasSummary = hasSummary || strings.HasPrefix(line, summaryPrefix)
		hasUsage = hasUsage || strings.HasPrefix(line, usagePrefix)
		if hasSummary && hasUsage {
			return true
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
		if idx := strings.Index(ext, ";"); idx >= 0 {
			ext = ext[:idx]
		}
		ext = strings.TrimSpace(ext)
		if ext != "" {
			return "image." + ext
		}
		return "image"
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if parsed, err := url.Parse(value); err == nil {
			if base := strings.TrimSpace(filepath.Base(parsed.Path)); base != "" && base != "." && base != string(filepath.Separator) {
				return base
			}
		}
		return value
	}
	if base := strings.TrimSpace(filepath.Base(value)); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return value
}

func ResolveCodeRunCallID(callID string, requestID *int64) string {
	if callID = strings.TrimSpace(callID); callID != "" {
		return callID
	}
	if requestID == nil {
		return ""
	}
	return fmt.Sprintf("req-%d", *requestID)
}

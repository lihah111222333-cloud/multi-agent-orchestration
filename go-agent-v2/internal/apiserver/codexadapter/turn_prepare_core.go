package codexadapter

import (
	"github.com/multi-agent/go-agent-v2/pkg/util"
	"net/url"
	"path/filepath"
	"strings"
)

func ensureTurnSteerResultTurnID(result map[string]any, activeTurnID string) map[string]any {
	if result == nil {
		result = map[string]any{}
	}
	if currentID, _ := result["turnId"].(string); strings.TrimSpace(currentID) == "" {
		result["turnId"] = strings.TrimSpace(activeTurnID)
	}
	return result
}

func buildAttachmentName(path string) string {
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

// buildAttachmentPreviewURL preserves compatibility for apiserver helper call sites.
func buildAttachmentPreviewURL(path string) string {
	return util.BuildAttachmentPreviewURL(path)
}

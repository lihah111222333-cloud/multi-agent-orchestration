package runtime

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// BuildAttachmentName normalizes display names for image/file attachments.
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

// BuildAttachmentPreviewURL normalizes preview URL for timeline attachments.
func BuildAttachmentPreviewURL(path string) string {
	return util.BuildAttachmentPreviewURL(path)
}

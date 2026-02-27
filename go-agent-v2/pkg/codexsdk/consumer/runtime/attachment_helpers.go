package runtime

import "github.com/multi-agent/go-agent-v2/pkg/util"

// BuildAttachmentName normalizes display names for image/file attachments.
func BuildAttachmentName(path string) string {
	return util.BuildAttachmentName(path)
}

// BuildAttachmentPreviewURL normalizes preview URL for timeline attachments.
func BuildAttachmentPreviewURL(path string) string {
	return util.BuildAttachmentPreviewURL(path)
}

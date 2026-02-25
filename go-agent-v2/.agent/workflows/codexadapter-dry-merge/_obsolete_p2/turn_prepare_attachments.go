package codexadapter

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

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

func BuildAttachmentPreviewURL(path string) string {
	return util.BuildAttachmentPreviewURL(path)
}

func BuildUserTimelineAttachments(images, files []string) []uistate.TimelineAttachment {
	attachments := make([]uistate.TimelineAttachment, 0, len(images)+len(files))
	for _, raw := range images {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = append(attachments, uistate.TimelineAttachment{
			Kind:       "image",
			Name:       BuildAttachmentName(path),
			Path:       path,
			PreviewURL: BuildAttachmentPreviewURL(path),
		})
	}
	for _, raw := range files {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = append(attachments, uistate.TimelineAttachment{
			Kind: "file",
			Name: BuildAttachmentName(path),
			Path: path,
		})
	}
	return attachments
}

func BuildUserTimelineAttachmentsFromInputs(inputs []contracts.TurnInput) []uistate.TimelineAttachment {
	if len(inputs) == 0 {
		return nil
	}
	attachments := make([]uistate.TimelineAttachment, 0, len(inputs))
	for _, input := range inputs {
		kind := strings.ToLower(strings.TrimSpace(input.Type))
		switch kind {
		case "image":
			imageURL := strings.TrimSpace(input.URL)
			if imageURL == "" {
				imageURL = strings.TrimSpace(input.Path)
			}
			if imageURL == "" {
				continue
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind:       "image",
				Name:       BuildAttachmentName(imageURL),
				Path:       imageURL,
				PreviewURL: BuildAttachmentPreviewURL(imageURL),
			})
		case "localimage":
			imagePath := strings.TrimSpace(input.Path)
			preview := strings.TrimSpace(input.URL)
			if preview == "" {
				preview = imagePath
			}
			if preview == "" {
				continue
			}
			nameSource := imagePath
			if nameSource == "" {
				nameSource = preview
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind:       "image",
				Name:       BuildAttachmentName(nameSource),
				Path:       imagePath,
				PreviewURL: BuildAttachmentPreviewURL(preview),
			})
		case "mention", "file":
			path := strings.TrimSpace(input.Path)
			if path == "" {
				continue
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind: "file",
				Name: BuildAttachmentName(path),
				Path: path,
			})
		case "filecontent":
			path := strings.TrimSpace(input.Path)
			if path != "" {
				attachments = append(attachments, uistate.TimelineAttachment{
					Kind: "file",
					Name: BuildAttachmentName(path),
					Path: path,
				})
				continue
			}
			if strings.TrimSpace(input.Content) == "" {
				continue
			}
			name := strings.TrimSpace(input.Name)
			if name == "" {
				name = "inline-file"
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind: "file",
				Name: name,
			})
		}
	}
	return attachments
}

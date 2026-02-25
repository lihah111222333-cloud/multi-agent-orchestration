package apiserver

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type parsedUserInputsForTest struct {
	prompt              string
	images              []string
	files               []string
	timelineAttachments []uistate.TimelineAttachment
}

func extractInputs(inputs []UserInput) (prompt string, images, files []string) {
	parsed := parseUserInputsForTest(inputs)
	return parsed.prompt, parsed.images, parsed.files
}

func parseUserInputsForTest(inputs []UserInput) parsedUserInputsForTest {
	if len(inputs) == 0 {
		return parsedUserInputsForTest{}
	}
	texts := make([]string, 0, len(inputs))
	images := make([]string, 0, len(inputs))
	files := make([]string, 0, len(inputs))
	attachments := make([]uistate.TimelineAttachment, 0, len(inputs))
	for _, inp := range inputs {
		switch strings.ToLower(strings.TrimSpace(inp.Type)) {
		case "text":
			text := util.StripLeadingSystemNoise(inp.Text)
			if strings.TrimSpace(text) != "" {
				texts = append(texts, text)
			}
		case "image":
			image := strings.TrimSpace(inp.URL)
			if image == "" {
				image = strings.TrimSpace(inp.Path)
			}
			if image == "" {
				continue
			}
			images = append(images, image)
			attachments = appendImageTimelineAttachmentForTest(
				attachments, buildAttachmentName(image), image, image,
			)
		case "localimage":
			imagePath := strings.TrimSpace(inp.Path)
			preview := strings.TrimSpace(inp.URL)
			if util.IsRemoteImageURL(preview) {
				images = append(images, preview)
			} else if imagePath != "" {
				images = append(images, imagePath)
			}
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
			attachments = appendImageTimelineAttachmentForTest(
				attachments, buildAttachmentName(nameSource), imagePath, preview,
			)
		case "filecontent":
			path := strings.TrimSpace(inp.Path)
			if path != "" {
				files = append(files, path)
				attachments = appendFileTimelineAttachmentForTest(
					attachments, buildAttachmentName(path), path,
				)
				continue
			}
			if inline := commonadapter.FileContentInputText(inp.Name, inp.Content); inline != "" {
				texts = append(texts, inline)
			}
			if strings.TrimSpace(inp.Content) == "" {
				continue
			}
			name := strings.TrimSpace(inp.Name)
			if name == "" {
				name = "inline-file"
			}
			attachments = appendFileTimelineAttachmentForTest(attachments, name, "")
		case "mention", "file":
			path := strings.TrimSpace(inp.Path)
			if path == "" {
				continue
			}
			files = append(files, path)
			attachments = appendFileTimelineAttachmentForTest(
				attachments, buildAttachmentName(path), path,
			)
		case "skill":
			// Keep parity with runtime: selectedSkills owns skill prompt injection.
		}
	}
	return parsedUserInputsForTest{
		prompt:              strings.Join(texts, "\n"),
		images:              images,
		files:               files,
		timelineAttachments: attachments,
	}
}

func appendImageTimelineAttachmentForTest(
	attachments []uistate.TimelineAttachment,
	name string,
	path string,
	preview string,
) []uistate.TimelineAttachment {
	return append(attachments, uistate.TimelineAttachment{
		Kind:       "image",
		Name:       name,
		Path:       path,
		PreviewURL: buildAttachmentPreviewURL(preview),
	})
}

func appendFileTimelineAttachmentForTest(
	attachments []uistate.TimelineAttachment,
	name string,
	path string,
) []uistate.TimelineAttachment {
	return append(attachments, uistate.TimelineAttachment{
		Kind: "file",
		Name: name,
		Path: path,
	})
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

func buildAttachmentPreviewURL(path string) string {
	return util.BuildAttachmentPreviewURL(path)
}

func buildUserTimelineAttachments(images, files []string) []uistate.TimelineAttachment {
	attachments := make([]uistate.TimelineAttachment, 0, len(images)+len(files))
	for _, raw := range images {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = appendImageTimelineAttachmentForTest(
			attachments, buildAttachmentName(path), path, path,
		)
	}
	for _, raw := range files {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = appendFileTimelineAttachmentForTest(
			attachments, buildAttachmentName(path), path,
		)
	}
	return attachments
}

func buildUserTimelineAttachmentsFromInputs(inputs []UserInput) []uistate.TimelineAttachment {
	parsed := parseUserInputsForTest(inputs)
	if len(parsed.timelineAttachments) == 0 {
		return nil
	}
	return append([]uistate.TimelineAttachment(nil), parsed.timelineAttachments...)
}

var codexThreadIDPatternForTest = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
)

func isLikelyCodexThreadID(raw string) bool {
	return normalizeCodexThreadID(raw) != ""
}

func normalizeCodexThreadID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	id = strings.TrimPrefix(strings.ToLower(id), "urn:uuid:")
	if !codexThreadIDPatternForTest.MatchString(id) {
		return ""
	}
	return id
}

func appendUniqueThreadID(dst []string, seen map[string]struct{}, candidate string) []string {
	id := normalizeCodexThreadID(candidate)
	if id == "" {
		return dst
	}
	if _, ok := seen[id]; ok {
		return dst
	}
	seen[id] = struct{}{}
	return append(dst, id)
}

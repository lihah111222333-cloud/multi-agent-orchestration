package apiserver

import (
	"strings"
	"testing"
)

func TestBuildUserTimelineAttachments(t *testing.T) {
	attachments := buildUserTimelineAttachments(
		[]string{
			"/tmp/screen.png",
			"https://example.com/a.png",
			"data:image/png;base64,AAAA",
		},
		[]string{
			"/tmp/spec.md",
		},
	)

	if len(attachments) != 4 {
		t.Fatalf("len(attachments) = %d, want 4", len(attachments))
	}
	if attachments[0].Kind != "image" || attachments[0].PreviewURL != "file:///tmp/screen.png" {
		t.Fatalf("attachments[0] = %+v", attachments[0])
	}
	if attachments[1].Kind != "image" || attachments[1].PreviewURL != "https://example.com/a.png" {
		t.Fatalf("attachments[1] = %+v", attachments[1])
	}
	if attachments[2].Kind != "image" || attachments[2].PreviewURL != "data:image/png;base64,AAAA" {
		t.Fatalf("attachments[2] = %+v", attachments[2])
	}
	if attachments[3].Kind != "file" || attachments[3].Path != "/tmp/spec.md" {
		t.Fatalf("attachments[3] = %+v", attachments[3])
	}
}

func TestExtractInputs(t *testing.T) {
	prompt, images, files := extractInputs([]UserInput{
		{Type: "text", Text: "你好"},
		{Type: "localImage", Path: "/tmp/screen.png"},
		{Type: "mention", Path: "/tmp/spec.md"},
	})
	if prompt != "你好" {
		t.Fatalf("prompt = %q, want 你好", prompt)
	}
	if len(images) != 1 || images[0] != "/tmp/screen.png" {
		t.Fatalf("images = %#v", images)
	}
	if len(files) != 1 || files[0] != "/tmp/spec.md" {
		t.Fatalf("files = %#v", files)
	}
}

func TestBuildUserTimelineAttachmentsFromInputs_PreferLocalImageURL(t *testing.T) {
	attachments := buildUserTimelineAttachmentsFromInputs([]UserInput{
		{
			Type: "localImage",
			Path: "/tmp/clipboard-1.png",
			URL:  "data:image/png;base64,AAAA",
		},
		{
			Type: "mention",
			Path: "/tmp/spec.md",
		},
	})
	if len(attachments) != 2 {
		t.Fatalf("len(attachments) = %d, want 2", len(attachments))
	}
	if attachments[0].Kind != "image" {
		t.Fatalf("attachments[0].Kind = %q, want image", attachments[0].Kind)
	}
	if attachments[0].PreviewURL != "data:image/png;base64,AAAA" {
		t.Fatalf("attachments[0].PreviewURL = %q", attachments[0].PreviewURL)
	}
	if attachments[0].Path != "/tmp/clipboard-1.png" {
		t.Fatalf("attachments[0].Path = %q", attachments[0].Path)
	}
	if attachments[1].Kind != "file" || attachments[1].Path != "/tmp/spec.md" {
		t.Fatalf("attachments[1] = %+v", attachments[1])
	}
}

func TestExtractInputs_FileContentWithoutPathUsesPrompt(t *testing.T) {
	prompt, images, files := extractInputs([]UserInput{
		{Type: "text", Text: "请看附件"},
		{Type: "fileContent", Name: "L1记忆系统.md", Content: "# 设计\nA"},
	})
	if !strings.Contains(prompt, "[file:L1记忆系统.md]") {
		t.Fatalf("prompt = %q, want inline file marker", prompt)
	}
	if !strings.Contains(prompt, "# 设计") {
		t.Fatalf("prompt = %q, want inline file content", prompt)
	}
	if len(images) != 0 {
		t.Fatalf("images = %#v, want empty", images)
	}
	if len(files) != 0 {
		t.Fatalf("files = %#v, want empty", files)
	}
}

func TestBuildUserTimelineAttachmentsFromInputs_FileContentWithoutPath(t *testing.T) {
	attachments := buildUserTimelineAttachmentsFromInputs([]UserInput{
		{Type: "fileContent", Name: "L1记忆系统.md", Content: "hello"},
	})
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if attachments[0].Kind != "file" || attachments[0].Name != "L1记忆系统.md" {
		t.Fatalf("attachments[0] = %+v", attachments[0])
	}
	if attachments[0].Path != "" {
		t.Fatalf("attachments[0].Path = %q, want empty", attachments[0].Path)
	}
}

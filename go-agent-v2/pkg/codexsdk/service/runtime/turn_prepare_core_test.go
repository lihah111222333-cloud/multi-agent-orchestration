package runtime

import (
	"context"
	"strings"
	"testing"
)

type prepareAdapterStub struct {
	attachmentNameFn       func(string) string
	attachmentPreviewURLFn func(string) string
	activeTurnID           string
	hasActiveTurn          bool
}

func (s prepareAdapterStub) adapter() PrepareAdapter {
	return PrepareAdapter{
		MergePromptText: func(left, right string) string {
			left = strings.TrimSpace(left)
			right = strings.TrimSpace(right)
			if left == "" {
				return right
			}
			if right == "" {
				return left
			}
			return left + "\n" + right
		},
		FileContentInputText: func(name, content string) string { return "" },
		BuildAttachmentName: func(path string) string {
			if s.attachmentNameFn != nil {
				return s.attachmentNameFn(path)
			}
			return path
		},
		BuildAttachmentPreviewURL: func(path string) string {
			if s.attachmentPreviewURLFn != nil {
				return s.attachmentPreviewURLFn(path)
			}
			return path
		},
		ActiveTrackedTurnID: func(threadID string) (string, bool) {
			return s.activeTurnID, s.hasActiveTurn
		},
		ShowInjectedPromptInChat: func(context.Context) bool { return false },
		UIRuntime:                func() TimelineRuntime { return nil },
	}
}

func TestParseTurnInputs_UsesAttachmentCallbacks(t *testing.T) {
	nameFn := func(path string) string { return "N<" + strings.TrimSpace(path) + ">" }
	previewFn := func(path string) string { return "P<" + strings.TrimSpace(path) + ">" }

	parsed := ParseTurnInputs([]TurnInput{
		{Type: "image", URL: " https://example.com/a.png "},
		{Type: "localImage", Path: "/tmp/photo.jpg"},
		{Type: "mention", Path: "/repo/main.go"},
	}, nil, nameFn, previewFn)

	if len(parsed.TimelineAttachments) != 3 {
		t.Fatalf("attachments len = %d, want 3", len(parsed.TimelineAttachments))
	}

	if got, want := parsed.TimelineAttachments[0].Name, "N<https://example.com/a.png>"; got != want {
		t.Fatalf("attachment[0].Name = %q, want %q", got, want)
	}
	if got, want := parsed.TimelineAttachments[0].PreviewURL, "P<https://example.com/a.png>"; got != want {
		t.Fatalf("attachment[0].PreviewURL = %q, want %q", got, want)
	}

	if got, want := parsed.TimelineAttachments[1].Name, "N</tmp/photo.jpg>"; got != want {
		t.Fatalf("attachment[1].Name = %q, want %q", got, want)
	}
	if got, want := parsed.TimelineAttachments[1].PreviewURL, "P</tmp/photo.jpg>"; got != want {
		t.Fatalf("attachment[1].PreviewURL = %q, want %q", got, want)
	}

	if got, want := parsed.TimelineAttachments[2].Name, "N</repo/main.go>"; got != want {
		t.Fatalf("attachment[2].Name = %q, want %q", got, want)
	}
}

func TestPrepareTurnStartSubmission_UsesAdapterAttachmentCallbacks(t *testing.T) {
	adapter := prepareAdapterStub{
		attachmentNameFn:       func(path string) string { return "name:" + strings.TrimSpace(path) },
		attachmentPreviewURLFn: func(path string) string { return "preview:" + strings.TrimSpace(path) },
	}

	prepared, err := PrepareTurnStartSubmission(adapter.adapter(), "thread-1", []TurnInput{
		{Type: "image", URL: "https://example.com/img.png"},
		{Type: "mention", Path: "/tmp/doc.md"},
	}, nil, false)
	if err != nil {
		t.Fatalf("PrepareTurnStartSubmission error: %v", err)
	}
	if len(prepared.TimelineAttachments) != 2 {
		t.Fatalf("attachments len = %d, want 2", len(prepared.TimelineAttachments))
	}
	if got, want := prepared.TimelineAttachments[0].Name, "name:https://example.com/img.png"; got != want {
		t.Fatalf("attachment[0].Name = %q, want %q", got, want)
	}
	if got, want := prepared.TimelineAttachments[0].PreviewURL, "preview:https://example.com/img.png"; got != want {
		t.Fatalf("attachment[0].PreviewURL = %q, want %q", got, want)
	}
	if got, want := prepared.TimelineAttachments[1].Name, "name:/tmp/doc.md"; got != want {
		t.Fatalf("attachment[1].Name = %q, want %q", got, want)
	}
}

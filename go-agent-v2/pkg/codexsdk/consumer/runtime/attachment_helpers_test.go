package runtime

import (
	"strings"
	"testing"
)

func TestBuildAttachmentName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "local file", path: "/Users/foo/bar/baz.go", want: "baz.go"},
		{name: "url", path: "https://example.com/path/image.png", want: "image.png"},
		{name: "data uri", path: "data:image/png;base64,abc", want: "image.png"},
		{name: "data uri no ext", path: "data:image/;base64,abc", want: "image"},
		{name: "empty", path: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildAttachmentName(tt.path)
			if got != tt.want {
				t.Fatalf("BuildAttachmentName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestBuildAttachmentPreviewURL(t *testing.T) {
	if got := BuildAttachmentPreviewURL("https://foo.com/img.png"); got != "https://foo.com/img.png" {
		t.Fatalf("http preview = %q", got)
	}
	if got := BuildAttachmentPreviewURL("data:image/png;base64,abc"); got != "data:image/png;base64,abc" {
		t.Fatalf("data preview = %q", got)
	}
	got := BuildAttachmentPreviewURL("/tmp/photo.jpg")
	if !strings.HasPrefix(got, "file://") {
		t.Fatalf("local preview = %q, want file:// prefix", got)
	}
	if got := BuildAttachmentPreviewURL(""); got != "" {
		t.Fatalf("empty preview = %q, want empty", got)
	}
}

package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathToURI_PreservesFileURI(t *testing.T) {
	raw := "file:///tmp/a%20b.go"
	if got := pathToURI(raw); got != raw {
		t.Fatalf("pathToURI(%q) = %q, want %q", raw, got, raw)
	}
}

func TestPathToURI_EncodesSpecialChars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dir with space#hash")
	path := filepath.Join(dir, "a b#c.go")
	uri := pathToURI(path)
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("uri %q does not start with file://", uri)
	}
	if !strings.Contains(uri, "%20") {
		t.Fatalf("uri %q should encode spaces", uri)
	}
	if !strings.Contains(uri, "%23") {
		t.Fatalf("uri %q should encode #", uri)
	}
}

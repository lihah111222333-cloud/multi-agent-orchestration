package archive

import (
	"os"
	"path/filepath"
	"testing"

	archivesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/archive"
)

func TestCollectThreadArchiveMapFromRoot_MergesManifestAndDirTimestamp(t *testing.T) {
	root := t.TempDir()

	snapshotA := filepath.Join(root, "thread-a", "1700000000001")
	if err := os.MkdirAll(snapshotA, 0o755); err != nil {
		t.Fatalf("mkdir snapshotA: %v", err)
	}
	if err := WriteThreadArchiveManifest(archivesvc.ThreadArchiveManifest{
		ThreadID:   "thread-a-real",
		ArchivedAt: "1700000001234",
		ArchiveDir: snapshotA,
	}); err != nil {
		t.Fatalf("write manifest A: %v", err)
	}

	snapshotB := filepath.Join(root, "thread-b", "1700000002222")
	if err := os.MkdirAll(snapshotB, 0o755); err != nil {
		t.Fatalf("mkdir snapshotB: %v", err)
	}

	snapshotC := filepath.Join(root, "thread-c", "1700000003333")
	if err := os.MkdirAll(snapshotC, 0o755); err != nil {
		t.Fatalf("mkdir snapshotC: %v", err)
	}
	if err := os.WriteFile(filepath.Join(snapshotC, "manifest.json"), []byte("{bad json"), 0o644); err != nil {
		t.Fatalf("write bad manifest C: %v", err)
	}

	got, err := collectThreadArchiveMapFromRoot(root)
	if err != nil {
		t.Fatalf("collect map: %v", err)
	}
	if got["thread-a-real"] != 1700000001234 {
		t.Fatalf("thread-a-real archivedAt=%d, want %d (map=%v)", got["thread-a-real"], int64(1700000001234), got)
	}
	if _, ok := got["thread-a"]; ok {
		t.Fatalf("unexpected raw thread dir key thread-a in map=%v", got)
	}
	if got["thread-b"] != 1700000002222 {
		t.Fatalf("thread-b archivedAt=%d, want %d (map=%v)", got["thread-b"], int64(1700000002222), got)
	}
	if got["thread-c"] != 1700000003333 {
		t.Fatalf("thread-c archivedAt=%d, want %d (map=%v)", got["thread-c"], int64(1700000003333), got)
	}
}

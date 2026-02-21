package apiserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestResolveRolloutHistorySourceHandlesNilManager(t *testing.T) {
	srv := &Server{}
	codexThreadID, rolloutPath := srv.resolveRolloutHistorySource(context.Background(), "thread-1")
	if codexThreadID != "" || rolloutPath != "" {
		t.Fatalf("resolveRolloutHistorySource() = (%q, %q), want empty", codexThreadID, rolloutPath)
	}
}

func TestResolveThreadArchiveRootDirUsesHomeMultiAgent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		t.Fatalf("resolveThreadArchiveRootDir error: %v", err)
	}
	want := filepath.Join(tmpHome, ".multi-agent", "thread-archives")
	if rootDir != want {
		t.Fatalf("archive root = %q, want %q", rootDir, want)
	}
	if info, err := os.Stat(rootDir); err != nil || !info.IsDir() {
		t.Fatalf("archive root not created: info=%v err=%v", info, err)
	}
}

func TestResolveThreadArchiveSnapshotDirAllocatesUniquePath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		t.Fatalf("resolveThreadArchiveRootDir error: %v", err)
	}
	archivedAt := "2026-02-21T08:09:10.123456789Z"
	firstDir, err := resolveThreadArchiveSnapshotDir(rootDir, "thread-1", archivedAt)
	if err != nil {
		t.Fatalf("resolveThreadArchiveSnapshotDir(first) error: %v", err)
	}
	if err := os.MkdirAll(firstDir, 0o755); err != nil {
		t.Fatalf("mkdir first snapshot dir: %v", err)
	}
	secondDir, err := resolveThreadArchiveSnapshotDir(rootDir, "thread-1", archivedAt)
	if err != nil {
		t.Fatalf("resolveThreadArchiveSnapshotDir(second) error: %v", err)
	}
	if secondDir == firstDir {
		t.Fatalf("second snapshot dir should be unique, got %q", secondDir)
	}
}

func TestNextArchiveFilePathRejectsInvalidFilename(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := nextArchiveFilePath(tmpDir, "...")
	if err == nil {
		t.Fatal("nextArchiveFilePath should fail for invalid filename")
	}
}

func TestCopyFileRejectsExistingTarget(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.txt")
	targetPath := filepath.Join(tmpDir, "target.txt")
	if err := os.WriteFile(srcPath, []byte("source"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	err := copyFile(srcPath, targetPath)
	if err == nil {
		t.Fatal("copyFile should fail when target already exists")
	}
	if got, readErr := os.ReadFile(targetPath); readErr != nil || string(got) != "existing" {
		t.Fatalf("target file should stay unchanged, got=%q err=%v", string(got), readErr)
	}
}

func TestThreadUnarchiveTypedWarnsWhenArchiveModified(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		t.Fatalf("resolveThreadArchiveRootDir error: %v", err)
	}
	archivedAt := time.Now().UTC().Format(time.RFC3339Nano)
	archiveDir, err := resolveThreadArchiveSnapshotDir(rootDir, "thread-1", archivedAt)
	if err != nil {
		t.Fatalf("resolveThreadArchiveSnapshotDir error: %v", err)
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}

	archivedFile := filepath.Join(archiveDir, "bp.json")
	initial := []byte(`{"k":"v"}`)
	if err := os.WriteFile(archivedFile, initial, 0o644); err != nil {
		t.Fatalf("write archived file: %v", err)
	}
	sum, err := fileSHA256(archivedFile)
	if err != nil {
		t.Fatalf("fileSHA256: %v", err)
	}
	manifest := threadArchiveManifest{
		ThreadID:      "thread-1",
		CodexThreadID: "11111111-1111-1111-1111-111111111111",
		ArchivedAt:    archivedAt,
		ArchiveDir:    archiveDir,
		Files: []threadArchiveFile{
			{
				Kind:         "breakpoint",
				SourcePath:   "/tmp/source/bp.json",
				ArchivedPath: archivedFile,
				SizeBytes:    int64(len(initial)),
				SHA256:       sum,
			},
		},
	}
	if err := writeThreadArchiveManifest(manifest); err != nil {
		t.Fatalf("writeThreadArchiveManifest: %v", err)
	}
	// 模拟归档后被外部修改。
	if err := os.WriteFile(archivedFile, []byte(`{"k":"changed"}`), 0o644); err != nil {
		t.Fatalf("mutate archived file: %v", err)
	}

	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
	}
	ctx := context.Background()
	if err := srv.prefManager.Set(ctx, prefThreadArchivesChat, map[string]any{
		"thread-1": time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("set archive pref: %v", err)
	}

	raw, err := srv.threadUnarchiveTyped(ctx, threadIDParams{ThreadID: "thread-1"})
	if err != nil {
		t.Fatalf("threadUnarchiveTyped error: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("response type = %T, want map[string]any", raw)
	}
	if modified, _ := resp["archiveModified"].(bool); !modified {
		t.Fatalf("archiveModified = %#v, want true", resp["archiveModified"])
	}
	warning := strings.TrimSpace(toString(resp["warning"]))
	if warning == "" {
		t.Fatal("warning should be present when archived file was modified")
	}
	filesRaw, ok := resp["modifiedFiles"]
	if !ok {
		t.Fatal("modifiedFiles missing in response")
	}
	modifiedFiles, ok := filesRaw.([]string)
	if !ok || len(modifiedFiles) == 0 {
		t.Fatalf("modifiedFiles = %#v, want non-empty []string", filesRaw)
	}

	remaining, err := srv.loadThreadArchiveMap(ctx)
	if err != nil {
		t.Fatalf("loadThreadArchiveMap: %v", err)
	}
	if _, exists := remaining["thread-1"]; exists {
		t.Fatalf("thread-1 should be removed from archive pref map: %#v", remaining)
	}
}

func toString(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

package apiserver

import (
	"context"
	"os"
	"path/filepath"
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
	if _, exists := resp["warning"]; exists {
		t.Fatalf("warning should not be returned, got: %#v", resp["warning"])
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

func TestArchiveThreadArtifactsPrunesCodexSourcesOnHashMatch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	codexThreadID := "11111111-1111-1111-1111-111111111111"
	shellSource := filepath.Join(tmpHome, ".codex", "shell_snapshots", codexThreadID+".sh")
	if err := os.MkdirAll(filepath.Dir(shellSource), 0o755); err != nil {
		t.Fatalf("mkdir shell source dir: %v", err)
	}
	if err := os.WriteFile(shellSource, []byte("echo archived\n"), 0o644); err != nil {
		t.Fatalf("write shell source: %v", err)
	}

	now := time.Now().UTC()
	rolloutSource := filepath.Join(
		tmpHome,
		".codex",
		"sessions",
		now.Format("2006"),
		now.Format("01"),
		now.Format("02"),
		"rollout-"+now.Format("2006-01-02T15-04-05")+"-"+codexThreadID+".jsonl",
	)
	if err := os.MkdirAll(filepath.Dir(rolloutSource), 0o755); err != nil {
		t.Fatalf("mkdir rollout source dir: %v", err)
	}
	if err := os.WriteFile(rolloutSource, []byte("{\"type\":\"response_item\"}\n"), 0o644); err != nil {
		t.Fatalf("write rollout source: %v", err)
	}

	srv := &Server{}
	manifest, err := srv.archiveThreadArtifacts(context.Background(), codexThreadID)
	if err != nil {
		t.Fatalf("archiveThreadArtifacts error: %v", err)
	}
	if len(manifest.Files) < 2 {
		t.Fatalf("manifest files = %d, want >= 2", len(manifest.Files))
	}
	if _, err := os.Stat(shellSource); !os.IsNotExist(err) {
		t.Fatalf("shell source should be removed after archive, err=%v", err)
	}
	if _, err := os.Stat(rolloutSource); !os.IsNotExist(err) {
		t.Fatalf("rollout source should be removed after archive, err=%v", err)
	}
	for _, meta := range manifest.Files {
		if _, err := os.Stat(meta.ArchivedPath); err != nil {
			t.Fatalf("archived file should exist: %s err=%v", meta.ArchivedPath, err)
		}
	}
}

func TestPruneArchivedCodexSourceFilesSkipsDeleteWhenHashMismatch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	sourcePath := filepath.Join(tmpHome, ".codex", "shell_snapshots", "mismatch.sh")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("source-a"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	archiveDir := filepath.Join(tmpHome, ".multi-agent", "thread-archives", "thread-1", "snapshot")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}
	archivedPath := filepath.Join(archiveDir, "mismatch.sh")
	if err := os.WriteFile(archivedPath, []byte("source-b"), 0o644); err != nil {
		t.Fatalf("write archived file: %v", err)
	}
	sum, err := fileSHA256(archivedPath)
	if err != nil {
		t.Fatalf("fileSHA256: %v", err)
	}

	pruneArchivedCodexSourceFiles("thread-1", []threadArchiveFile{
		{
			Kind:         "shell_snapshot",
			SourcePath:   sourcePath,
			ArchivedPath: archivedPath,
			SHA256:       sum,
		},
	}, archiveDir)

	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("source file should be kept when hash mismatch, err=%v", err)
	}
}

func TestThreadUnarchiveTypedRestoresCodexArtifacts(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		t.Fatalf("resolveThreadArchiveRootDir error: %v", err)
	}
	threadID := "thread-restore-1"
	archivedAt := time.Now().UTC().Format(time.RFC3339Nano)
	archiveDir, err := resolveThreadArchiveSnapshotDir(rootDir, threadID, archivedAt)
	if err != nil {
		t.Fatalf("resolveThreadArchiveSnapshotDir error: %v", err)
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}

	archivedPath := filepath.Join(archiveDir, "restored.sh")
	archivedContent := []byte("echo restored\n")
	if err := os.WriteFile(archivedPath, archivedContent, 0o644); err != nil {
		t.Fatalf("write archived file: %v", err)
	}
	sum, err := fileSHA256(archivedPath)
	if err != nil {
		t.Fatalf("fileSHA256: %v", err)
	}
	sourcePath := filepath.Join(tmpHome, ".codex", "shell_snapshots", "restored.sh")
	manifest := threadArchiveManifest{
		ThreadID:      threadID,
		CodexThreadID: "11111111-1111-1111-1111-111111111111",
		ArchivedAt:    archivedAt,
		ArchiveDir:    archiveDir,
		Files: []threadArchiveFile{
			{
				Kind:         "shell_snapshot",
				SourcePath:   sourcePath,
				ArchivedPath: archivedPath,
				SizeBytes:    int64(len(archivedContent)),
				SHA256:       sum,
			},
		},
	}
	if err := writeThreadArchiveManifest(manifest); err != nil {
		t.Fatalf("writeThreadArchiveManifest: %v", err)
	}

	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
	}
	ctx := context.Background()
	if err := srv.prefManager.Set(ctx, prefThreadArchivesChat, map[string]any{
		threadID: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("set archive pref: %v", err)
	}

	raw, err := srv.threadUnarchiveTyped(ctx, threadIDParams{ThreadID: threadID})
	if err != nil {
		t.Fatalf("threadUnarchiveTyped error: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("response type = %T, want map[string]any", raw)
	}
	restoredRaw, ok := resp["restoredFiles"]
	if !ok {
		t.Fatalf("restoredFiles missing in response: %#v", resp)
	}
	restoredFiles, ok := restoredRaw.([]string)
	if !ok || len(restoredFiles) == 0 {
		t.Fatalf("restoredFiles = %#v, want non-empty []string", restoredRaw)
	}
	got, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read restored source: %v", err)
	}
	if string(got) != string(archivedContent) {
		t.Fatalf("restored content mismatch: got=%q want=%q", string(got), string(archivedContent))
	}

	remaining, err := srv.loadThreadArchiveMap(ctx)
	if err != nil {
		t.Fatalf("loadThreadArchiveMap: %v", err)
	}
	if _, exists := remaining[threadID]; exists {
		t.Fatalf("thread should be removed from archive pref map: %#v", remaining)
	}
}

func TestThreadUnarchiveTypedSkipsRestoreWhenThreadNotArchived(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		t.Fatalf("resolveThreadArchiveRootDir error: %v", err)
	}
	threadID := "thread-not-archived"
	archivedAt := time.Now().UTC().Format(time.RFC3339Nano)
	archiveDir, err := resolveThreadArchiveSnapshotDir(rootDir, threadID, archivedAt)
	if err != nil {
		t.Fatalf("resolveThreadArchiveSnapshotDir error: %v", err)
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}

	archivedPath := filepath.Join(archiveDir, "skip-restore.sh")
	archivedContent := []byte("echo archived\n")
	if err := os.WriteFile(archivedPath, archivedContent, 0o644); err != nil {
		t.Fatalf("write archived file: %v", err)
	}
	sum, err := fileSHA256(archivedPath)
	if err != nil {
		t.Fatalf("fileSHA256: %v", err)
	}
	sourcePath := filepath.Join(tmpHome, ".codex", "shell_snapshots", "skip-restore.sh")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	originalSourceContent := []byte("echo original\n")
	if err := os.WriteFile(sourcePath, originalSourceContent, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	manifest := threadArchiveManifest{
		ThreadID:      threadID,
		CodexThreadID: "11111111-1111-1111-1111-111111111111",
		ArchivedAt:    archivedAt,
		ArchiveDir:    archiveDir,
		Files: []threadArchiveFile{
			{
				Kind:         "shell_snapshot",
				SourcePath:   sourcePath,
				ArchivedPath: archivedPath,
				SizeBytes:    int64(len(archivedContent)),
				SHA256:       sum,
			},
		},
	}
	if err := writeThreadArchiveManifest(manifest); err != nil {
		t.Fatalf("writeThreadArchiveManifest: %v", err)
	}

	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
	}
	ctx := context.Background()
	if err := srv.prefManager.Set(ctx, prefThreadArchivesChat, map[string]any{
		"another-thread": time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("set archive pref: %v", err)
	}

	raw, err := srv.threadUnarchiveTyped(ctx, threadIDParams{ThreadID: threadID})
	if err != nil {
		t.Fatalf("threadUnarchiveTyped error: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("response type = %T, want map[string]any", raw)
	}
	if _, exists := resp["restoredFiles"]; exists {
		t.Fatalf("restoredFiles should be absent when thread is not archived, got: %#v", resp["restoredFiles"])
	}
	got, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source file: %v", err)
	}
	if string(got) != string(originalSourceContent) {
		t.Fatalf("source content should stay unchanged, got=%q want=%q", string(got), string(originalSourceContent))
	}
}

func TestCopyFileOverwriteKeepsTargetWhenSourceMissing(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "target.txt")
	if err := os.WriteFile(targetPath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	err := copyFileOverwrite(filepath.Join(tmpDir, "missing.txt"), targetPath)
	if err == nil {
		t.Fatal("copyFileOverwrite should fail when source is missing")
	}
	got, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("read target after failed overwrite: %v", readErr)
	}
	if string(got) != "original" {
		t.Fatalf("target should stay unchanged after failed overwrite, got=%q", string(got))
	}
}

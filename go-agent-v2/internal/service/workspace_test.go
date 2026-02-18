package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

func TestRecordMergeError(t *testing.T) {
	result := &WorkspaceMergeResult{}
	recordMergeError(result, "a.go", "boom")

	if result.Errors != 1 {
		t.Fatalf("errors = %d, want 1", result.Errors)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files len = %d, want 1", len(result.Files))
	}
	if result.Files[0].Action != "error" {
		t.Fatalf("action = %q, want error", result.Files[0].Action)
	}
}

func TestValidateBootstrapFile(t *testing.T) {
	tempDir := t.TempDir()
	run := &store.WorkspaceRun{
		SourceRoot:    tempDir,
		WorkspacePath: tempDir,
	}

	filePath := filepath.Join(tempDir, "ok.txt")
	if err := os.WriteFile(filePath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	info, err := os.Lstat(filePath)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if err := validateBootstrapFile(run, "ok.txt", info, 2, 10, 20); err != nil {
		t.Fatalf("validateBootstrapFile() unexpected error: %v", err)
	}
}

func TestValidateBootstrapFile_RejectSymlink(t *testing.T) {
	tempDir := t.TempDir()
	run := &store.WorkspaceRun{
		SourceRoot:    tempDir,
		WorkspacePath: tempDir,
	}

	target := filepath.Join(tempDir, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(tempDir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if err := validateBootstrapFile(run, "link.txt", info, 0, 10, 20); err == nil {
		t.Fatal("expected symlink error")
	}
}

func TestValidateBootstrapFile_RejectTooLarge(t *testing.T) {
	tempDir := t.TempDir()
	run := &store.WorkspaceRun{
		SourceRoot:    tempDir,
		WorkspacePath: tempDir,
	}

	filePath := filepath.Join(tempDir, "big.txt")
	if err := os.WriteFile(filePath, []byte("123456"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	info, err := os.Lstat(filePath)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if err := validateBootstrapFile(run, "big.txt", info, 6, 5, 20); err == nil {
		t.Fatal("expected too large error")
	}
}


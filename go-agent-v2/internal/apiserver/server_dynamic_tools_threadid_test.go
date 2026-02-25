package apiserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDynamicToolThreadIDs(t *testing.T) {
	tests := []struct {
		name         string
		agentID      string
		rawThreadID  string
		wantThreadID string
		wantCodexID  string
	}{
		{
			name:         "prefer agent thread ID when both present",
			agentID:      "thread-123",
			rawThreadID:  "019c96bd-1450-7510-800f-6270ab10f06c",
			wantThreadID: "thread-123",
			wantCodexID:  "019c96bd-1450-7510-800f-6270ab10f06c",
		},
		{
			name:         "agent only",
			agentID:      "thread-123",
			rawThreadID:  "",
			wantThreadID: "thread-123",
			wantCodexID:  "",
		},
		{
			name:         "fallback to raw thread ID when agent is empty",
			agentID:      "",
			rawThreadID:  "019c96bd-1450-7510-800f-6270ab10f06c",
			wantThreadID: "019c96bd-1450-7510-800f-6270ab10f06c",
			wantCodexID:  "",
		},
		{
			name:         "drop duplicate codex thread ID",
			agentID:      "thread-123",
			rawThreadID:  "thread-123",
			wantThreadID: "thread-123",
			wantCodexID:  "",
		},
		{
			name:         "trim whitespace",
			agentID:      " thread-123 ",
			rawThreadID:  " 019c96bd-1450-7510-800f-6270ab10f06c ",
			wantThreadID: "thread-123",
			wantCodexID:  "019c96bd-1450-7510-800f-6270ab10f06c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotThreadID, gotCodexID := resolveDynamicToolThreadIDs(tt.agentID, tt.rawThreadID)
			if gotThreadID != tt.wantThreadID {
				t.Fatalf("threadID = %q, want %q", gotThreadID, tt.wantThreadID)
			}
			if gotCodexID != tt.wantCodexID {
				t.Fatalf("codexThreadID = %q, want %q", gotCodexID, tt.wantCodexID)
			}
		})
	}
}

func TestShouldCaptureDynamicToolDiff(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args map[string]any
		want bool
	}{
		{
			name: "lsp did_change persist true",
			tool: "lsp_did_change",
			args: map[string]any{"persist_to_disk": true},
			want: true,
		},
		{
			name: "lsp did_change persist false",
			tool: "lsp_did_change",
			args: map[string]any{"persist_to_disk": false},
			want: false,
		},
		{
			name: "lsp did_change string true",
			tool: "lsp_did_change",
			args: map[string]any{"persist_to_disk": "true"},
			want: true,
		},
		{
			name: "functions prefix",
			tool: "functions.code_run",
			args: map[string]any{"mode": "project_cmd"},
			want: true,
		},
		{
			name: "run alias",
			tool: "run",
			args: map[string]any{"mode": "run"},
			want: true,
		},
		{
			name: "read-only lsp",
			tool: "lsp_hover",
			args: map[string]any{"file_path": "main.go"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCaptureDynamicToolDiff(tt.tool, tt.args); got != tt.want {
				t.Fatalf("shouldCaptureDynamicToolDiff(%q, %v) = %v, want %v", tt.tool, tt.args, got, tt.want)
			}
		})
	}
}

func TestResolveDynamicToolDiffRepoRoot(t *testing.T) {
	repoRoot := initTestGitRepo(t)
	trackedPath := filepath.Join(repoRoot, "tracked.txt")

	got := resolveDynamicToolDiffRepoRoot(nil, "", map[string]any{"file_path": trackedPath})
	if !sameFilePath(got, repoRoot) {
		t.Fatalf("resolveDynamicToolDiffRepoRoot(file_path) = %q, want %q", got, repoRoot)
	}

	got = resolveDynamicToolDiffRepoRoot(nil, "", map[string]any{"work_dir": repoRoot})
	if !sameFilePath(got, repoRoot) {
		t.Fatalf("resolveDynamicToolDiffRepoRoot(work_dir) = %q, want %q", got, repoRoot)
	}
}

func TestCaptureRepoDiffSnapshotIncludesTrackedAndUntracked(t *testing.T) {
	repoRoot := initTestGitRepo(t)
	trackedPath := filepath.Join(repoRoot, "tracked.txt")
	newPath := filepath.Join(repoRoot, "new.txt")

	before, err := captureRepoDiffSnapshot(repoRoot)
	if err != nil {
		t.Fatalf("captureRepoDiffSnapshot(before): %v", err)
	}
	if strings.TrimSpace(before) != "" {
		t.Fatalf("expected empty baseline diff, got: %s", before)
	}

	if err := os.WriteFile(trackedPath, []byte("line-1\nline-2\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("brand-new\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	after, err := captureRepoDiffSnapshot(repoRoot)
	if err != nil {
		t.Fatalf("captureRepoDiffSnapshot(after): %v", err)
	}
	if !strings.Contains(after, "diff --git a/tracked.txt b/tracked.txt") {
		t.Fatalf("tracked diff missing, got: %s", after)
	}
	if !strings.Contains(after, "new.txt") {
		t.Fatalf("untracked diff missing, got: %s", after)
	}
}

func TestBuildIncrementalDiffTextFiltersPreexistingChanges(t *testing.T) {
	repoRoot := initTestGitRepo(t)
	trackedPath := filepath.Join(repoRoot, "tracked.txt")
	freshPath := filepath.Join(repoRoot, "fresh.txt")

	if err := os.WriteFile(trackedPath, []byte("line-1\npreexisting\n"), 0o644); err != nil {
		t.Fatalf("write preexisting tracked diff: %v", err)
	}
	beforeByFile, err := captureRepoDiffSnapshotByFile(repoRoot)
	if err != nil {
		t.Fatalf("capture before snapshot: %v", err)
	}

	if err := os.WriteFile(freshPath, []byte("new-file\n"), 0o644); err != nil {
		t.Fatalf("write fresh untracked file: %v", err)
	}
	afterByFile, err := captureRepoDiffSnapshotByFile(repoRoot)
	if err != nil {
		t.Fatalf("capture after snapshot: %v", err)
	}

	delta := buildIncrementalDiffText(beforeByFile, afterByFile)
	if !strings.Contains(delta, "fresh.txt") {
		t.Fatalf("incremental diff should include fresh.txt, got: %s", delta)
	}
	if strings.Contains(delta, "tracked.txt") {
		t.Fatalf("incremental diff should exclude preexisting tracked.txt, got: %s", delta)
	}
}

func TestBuildIncrementalDiffTextIncludesFurtherChangesOnDirtyFile(t *testing.T) {
	repoRoot := initTestGitRepo(t)
	trackedPath := filepath.Join(repoRoot, "tracked.txt")

	if err := os.WriteFile(trackedPath, []byte("line-1\nline-2\n"), 0o644); err != nil {
		t.Fatalf("write first dirty state: %v", err)
	}
	beforeByFile, err := captureRepoDiffSnapshotByFile(repoRoot)
	if err != nil {
		t.Fatalf("capture before snapshot: %v", err)
	}

	if err := os.WriteFile(trackedPath, []byte("line-1\nline-2\nline-3\n"), 0o644); err != nil {
		t.Fatalf("write second dirty state: %v", err)
	}
	afterByFile, err := captureRepoDiffSnapshotByFile(repoRoot)
	if err != nil {
		t.Fatalf("capture after snapshot: %v", err)
	}

	delta := buildIncrementalDiffText(beforeByFile, afterByFile)
	if !strings.Contains(delta, "tracked.txt") {
		t.Fatalf("incremental diff should include updated dirty tracked.txt, got: %s", delta)
	}
}

func sameFilePath(left, right string) bool {
	left = filepath.Clean(strings.TrimSpace(left))
	right = filepath.Clean(strings.TrimSpace(right))
	if left == right {
		return true
	}
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return filepath.Clean(leftResolved) == filepath.Clean(rightResolved)
}

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	repoRoot := t.TempDir()
	mustRunGit(t, repoRoot, "init")
	mustRunGit(t, repoRoot, "config", "user.email", "test@example.com")
	mustRunGit(t, repoRoot, "config", "user.name", "Codex Test")

	trackedPath := filepath.Join(repoRoot, "tracked.txt")
	if err := os.WriteFile(trackedPath, []byte("line-1\n"), 0o644); err != nil {
		t.Fatalf("seed tracked file: %v", err)
	}
	mustRunGit(t, repoRoot, "add", "tracked.txt")
	mustRunGit(t, repoRoot, "commit", "-m", "init")
	return repoRoot
}

func mustRunGit(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	cmdArgs := make([]string, 0, 2+len(args))
	cmdArgs = append(cmdArgs, "-C", repoRoot)
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

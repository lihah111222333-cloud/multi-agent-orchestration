package apiserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/tools"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
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
	beforeSnapshots := captureWorkingTreeFileSnapshots(repoRoot, sortedDiffMapKeys(beforeByFile))

	if err := os.WriteFile(freshPath, []byte("new-file\n"), 0o644); err != nil {
		t.Fatalf("write fresh untracked file: %v", err)
	}
	afterByFile, err := captureRepoDiffSnapshotByFile(repoRoot)
	if err != nil {
		t.Fatalf("capture after snapshot: %v", err)
	}

	delta, err := buildIncrementalDiffText(repoRoot, beforeByFile, afterByFile, beforeSnapshots)
	if err != nil {
		t.Fatalf("buildIncrementalDiffText: %v", err)
	}
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
	beforeSnapshots := captureWorkingTreeFileSnapshots(repoRoot, sortedDiffMapKeys(beforeByFile))

	if err := os.WriteFile(trackedPath, []byte("line-1\nline-2\nline-3\n"), 0o644); err != nil {
		t.Fatalf("write second dirty state: %v", err)
	}
	afterByFile, err := captureRepoDiffSnapshotByFile(repoRoot)
	if err != nil {
		t.Fatalf("capture after snapshot: %v", err)
	}

	delta, err := buildIncrementalDiffText(repoRoot, beforeByFile, afterByFile, beforeSnapshots)
	if err != nil {
		t.Fatalf("buildIncrementalDiffText: %v", err)
	}
	if !strings.Contains(delta, "tracked.txt") {
		t.Fatalf("incremental diff should include updated dirty tracked.txt, got: %s", delta)
	}
}

func TestSplitUnifiedDiffByFileSupportsQuotedPaths(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git \"a/dir with space/a.txt\" \"b/dir with space/a.txt\"",
		"index 1111111..2222222 100644",
		"--- \"a/dir with space/a.txt\"",
		"+++ \"b/dir with space/a.txt\"",
		"@@ -1 +1,2 @@",
		" line-1",
		"+line-2",
	}, "\n")

	byFile := splitUnifiedDiffByFile(diff)
	block, ok := byFile["dir with space/a.txt"]
	if !ok {
		t.Fatalf("expected quoted path key to be parsed, got keys: %v", sortedDiffMapKeys(byFile))
	}
	if !strings.Contains(block, "+line-2") {
		t.Fatalf("unexpected diff block: %s", block)
	}
}

func TestHandleDynamicToolCallUpdatesRuntimeDiffTextForCurrentCall(t *testing.T) {
	repoRoot := initTestGitRepo(t)
	preexistingPath := filepath.Join(repoRoot, "preexisting.txt")
	if err := os.WriteFile(preexistingPath, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("seed preexisting file: %v", err)
	}

	const agentID = "thread-123"
	mgr := newDynamicToolTestManager(t, "019c96bd-1450-7510-800f-6270ab10f06c")
	if err := mgr.Launch(context.Background(), agentID, agentID, "", repoRoot, "", nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(agentID) })

	s := New(Deps{Manager: mgr})
	trackedPath := filepath.Join(repoRoot, "tracked.txt")
	setRuntimeTool(s, "run", func(_ tools.ToolCallContext, args json.RawMessage) string {
		var req struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return "error: " + err.Error()
		}
		if err := os.WriteFile(req.FilePath, []byte(req.Content), 0o644); err != nil {
			return "error: " + err.Error()
		}
		return "ok"
	})

	callArgs, err := json.Marshal(map[string]any{
		"file_path": trackedPath,
		"content":   "line-1\nline-2\n",
	})
	if err != nil {
		t.Fatalf("marshal call args: %v", err)
	}
	rawCall, err := json.Marshal(agentcore.DynamicToolCallData{
		ThreadID:  "019c96bd-1450-7510-800f-6270ab10f06c",
		CallID:    "call-1",
		Tool:      "run",
		Arguments: callArgs,
	})
	if err != nil {
		t.Fatalf("marshal call payload: %v", err)
	}

	responded := false
	handleDynamicToolCall(s, agentID, agentcore.Event{
		Type: agentcore.EventDynamicToolCall,
		Data: rawCall,
		RespondResultFunc: func(any) error {
			responded = true
			return nil
		},
	})

	if !responded {
		t.Fatalf("dynamic tool result callback was not invoked")
	}
	diffText := s.uiRuntime.ThreadDiff(agentID)
	if !strings.Contains(diffText, "tracked.txt") {
		t.Fatalf("expected tracked file in diff, got: %s", diffText)
	}
	if !strings.Contains(diffText, "line-2") {
		t.Fatalf("expected updated content in diff, got: %s", diffText)
	}
	if strings.Contains(diffText, "preexisting.txt") {
		t.Fatalf("diff should not include preexisting dirty file, got: %s", diffText)
	}
}

func TestAgentEventHandlerTurnStartedClearsStaleDiff(t *testing.T) {
	repoRoot := initTestGitRepo(t)
	const agentID = "thread-456"
	mgr := newDynamicToolTestManager(t, "019c96bd-1450-7510-800f-6270ab10f06c")
	if err := mgr.Launch(context.Background(), agentID, agentID, "", repoRoot, "", nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop(agentID) })

	s := New(Deps{Manager: mgr})
	stalePayload := map[string]any{
		"threadId": agentID,
		"diff":     "stale-diff",
		"uiText":   "stale-diff",
	}
	staleNormalized := uistate.NormalizeEventFromPayload(agentcore.EventTurnDiff, "turn/diff/updated", stalePayload)
	s.uiRuntime.ApplyAgentEvent(agentID, staleNormalized, stalePayload)
	if got := s.uiRuntime.ThreadDiff(agentID); got == "" {
		t.Fatalf("expected stale diff to be set before turn start")
	}

	handler := AgentEventHandler(s, agentID)
	handler(agentcore.Event{
		Type: agentcore.EventTurnStarted,
		Data: json.RawMessage(`{}`),
	})

	if got := s.uiRuntime.ThreadDiff(agentID); got != "" {
		t.Fatalf("turn start should clear stale diff, got: %s", got)
	}
}

type dynamicToolFakeClient struct {
	port     int
	threadID string
	running  bool
	handler  agentcore.EventHandler
}

func (c *dynamicToolFakeClient) GetPort() int { return c.port }

func (c *dynamicToolFakeClient) GetThreadID() string { return c.threadID }

func (c *dynamicToolFakeClient) SetEventHandler(handler agentcore.EventHandler) { c.handler = handler }

func (c *dynamicToolFakeClient) SpawnAndConnect(context.Context, string, string, string, string, []agentcore.DynamicTool) error {
	c.running = true
	return nil
}

func (c *dynamicToolFakeClient) Submit(string, []string, []string, json.RawMessage) error { return nil }

func (c *dynamicToolFakeClient) SendCommand(string, string) error { return nil }

func (c *dynamicToolFakeClient) SendDynamicToolResult(string, string, *int64) error { return nil }

func (c *dynamicToolFakeClient) RespondError(int64, int, string) error { return nil }

func (c *dynamicToolFakeClient) ListThreads() ([]agentcore.ThreadInfo, error) { return nil, nil }

func (c *dynamicToolFakeClient) ResumeThread(agentcore.ResumeThreadRequest) error { return nil }

func (c *dynamicToolFakeClient) ForkThread(agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error) {
	return &agentcore.ForkThreadResponse{ThreadID: c.threadID}, nil
}

func (c *dynamicToolFakeClient) Shutdown() error {
	c.running = false
	return nil
}

func (c *dynamicToolFakeClient) Kill() error {
	c.running = false
	return nil
}

func (c *dynamicToolFakeClient) Running() bool { return c.running }

func newDynamicToolTestManager(t *testing.T, codexThreadID string) *runner.AgentManager {
	t.Helper()
	factory := func(port int, agentID string) agentcore.Client {
		threadID := strings.TrimSpace(codexThreadID)
		if threadID == "" {
			threadID = agentID
		}
		return &dynamicToolFakeClient{
			port:     port,
			threadID: threadID,
			running:  true,
		}
	}
	mgr, err := runner.NewAgentManager(factory, factory)
	if err != nil {
		t.Fatalf("NewAgentManager() error = %v", err)
	}
	return mgr
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

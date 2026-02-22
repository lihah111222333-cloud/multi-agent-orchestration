package apiserver

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/executor"
)

func TestCodeRunWithAgent_ProjectCmdDeniedWithoutFrontend(t *testing.T) {
	r, err := executor.NewCodeRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Cleanup)

	s := &Server{
		codeRunner: r,
		conns:      map[string]*connEntry{},
		pending:    make(map[int64]chan *Response),
	}

	args := json.RawMessage(`{"mode":"project_cmd","command":"echo hello"}`)
	resp := s.codeRunWithAgent(context.Background(), "agent-1", "call-1", args)
	if !strings.Contains(resp, "execution denied by user") {
		t.Fatalf("expected denied response, got: %s", resp)
	}
}

func TestCodeRunWithAgent_GoRun_DefaultAutoWrap(t *testing.T) {
	r, err := executor.NewCodeRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Cleanup)

	s := &Server{
		codeRunner: r,
		conns:      map[string]*connEntry{},
		pending:    make(map[int64]chan *Response),
	}

	args := json.RawMessage(`{"mode":"run","language":"go","code":"fmt.Println(\"hello-from-code-run\")"}`)
	resp := s.codeRunWithAgent(context.Background(), "agent-1", "call-2", args)
	if !strings.Contains(resp, `"success":true`) {
		t.Fatalf("expected success response, got: %s", resp)
	}
	if !strings.Contains(resp, "hello-from-code-run") {
		t.Fatalf("expected output content, got: %s", resp)
	}
}

func TestCodeRunTestWithAgent_EmptyTestFuncReturnsError(t *testing.T) {
	r, err := executor.NewCodeRunner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Cleanup)

	s := &Server{
		codeRunner: r,
		conns:      map[string]*connEntry{},
		pending:    make(map[int64]chan *Response),
	}

	resp := s.codeRunTestWithAgent(context.Background(), "agent-1", "call-3", json.RawMessage(`{}`))
	if !strings.Contains(resp, "test_func is required") {
		t.Fatalf("expected test_func validation error, got: %s", resp)
	}
}

func TestWaitForFrontendDecision_NoFrontendFailClose(t *testing.T) {
	s := &Server{
		conns:   map[string]*connEntry{},
		pending: make(map[int64]chan *Response),
	}
	if ok := s.waitForFrontendDecision("item/commandExecution/requestApproval", map[string]any{"x": 1}); ok {
		t.Fatal("expected fail-close false when no websocket and no notifyHook")
	}
}

func TestParseCodeRunTimeout(t *testing.T) {
	if got := parseCodeRunTimeout(0); got != 0 {
		t.Fatalf("timeout(0) = %v, want 0", got)
	}
	if got := parseCodeRunTimeout(-1); got != 0 {
		t.Fatalf("timeout(-1) = %v, want 0", got)
	}
	if got := parseCodeRunTimeout(math.NaN()); got != 0 {
		t.Fatalf("timeout(NaN) = %v, want 0", got)
	}
	if got := parseCodeRunTimeout(0.2); got != 200*time.Millisecond {
		t.Fatalf("timeout(0.2) = %v, want 200ms", got)
	}
}

func TestCodeRunWithAgent_UsesAgentWorkDirByDefault(t *testing.T) {
	root := t.TempDir()
	agentDir := filepath.Join(root, "agent-workdir")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := executor.NewCodeRunner(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Cleanup)

	s := &Server{
		codeRunner:     r,
		conns:          map[string]*connEntry{},
		pending:        make(map[int64]chan *Response),
		activeCodeRuns: make(map[string]map[string]context.CancelFunc),
		agentWorkDirs:  make(map[string]string),
	}
	s.setAgentWorkDir("agent-1", agentDir)

	args := json.RawMessage(`{"mode":"run","language":"go","code":"wd, _ := os.Getwd(); fmt.Println(wd)"}`)
	resp := s.codeRunWithAgent(context.Background(), "agent-1", "call-agent-cwd", args)
	if !strings.Contains(resp, `"success":true`) {
		t.Fatalf("expected success response, got: %s", resp)
	}
	if !strings.Contains(resp, agentDir) {
		t.Fatalf("expected output to contain agent cwd %q, got: %s", agentDir, resp)
	}
}

func TestCancelCodeRuns_ByAgent(t *testing.T) {
	s := &Server{
		activeCodeRuns: make(map[string]map[string]context.CancelFunc),
	}

	var canceled atomic.Int32
	runKey := s.registerCodeRunCancel("agent-1", "call-1", func() {
		canceled.Add(1)
	})
	if strings.TrimSpace(runKey) == "" {
		t.Fatal("expected non-empty run key")
	}

	count := s.cancelCodeRuns("agent-1")
	if count != 1 {
		t.Fatalf("cancelCodeRuns count = %d, want 1", count)
	}
	if got := canceled.Load(); got != 1 {
		t.Fatalf("canceled callbacks = %d, want 1", got)
	}
}

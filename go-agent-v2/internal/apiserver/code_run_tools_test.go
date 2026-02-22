package apiserver

import (
	"encoding/json"
	"strings"
	"testing"

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
	resp := s.codeRunWithAgent("agent-1", "call-1", args)
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
	resp := s.codeRunWithAgent("agent-1", "call-2", args)
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

	resp := s.codeRunTestWithAgent("agent-1", "call-3", json.RawMessage(`{}`))
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

package codexadapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
)

type fakeClient struct {
	port     int
	threadID string
	running  bool
}

func (c *fakeClient) GetPort() int { return c.port }
func (c *fakeClient) GetThreadID() string {
	return c.threadID
}
func (c *fakeClient) SetEventHandler(agentcore.EventHandler) {}
func (c *fakeClient) SpawnAndConnect(context.Context, string, string, string, string, []agentcore.DynamicTool) error {
	c.running = true
	return nil
}
func (c *fakeClient) Submit(string, []string, []string, json.RawMessage) error { return nil }
func (c *fakeClient) SendCommand(string, string) error                         { return nil }
func (c *fakeClient) SendDynamicToolResult(string, string, *int64) error       { return nil }
func (c *fakeClient) RespondError(int64, int, string) error                    { return nil }
func (c *fakeClient) ListThreads() ([]agentcore.ThreadInfo, error)             { return nil, nil }
func (c *fakeClient) ResumeThread(agentcore.ResumeThreadRequest) error         { return nil }
func (c *fakeClient) ForkThread(agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error) {
	return &agentcore.ForkThreadResponse{ThreadID: c.threadID}, nil
}
func (c *fakeClient) Shutdown() error {
	c.running = false
	return nil
}
func (c *fakeClient) Kill() error {
	c.running = false
	return nil
}
func (c *fakeClient) Running() bool { return c.running }

func testDeps(mgr *runner.AgentManager) *Deps {
	return normalizeDeps(Deps{
		Manager: mgr,
	})
}

func newTestManager(t *testing.T) *runner.AgentManager {
	t.Helper()
	factory := func(port int, agentID string) agentcore.Client {
		return &fakeClient{port: port, threadID: agentID, running: true}
	}
	mgr, err := runner.NewAgentManager(factory, factory)
	if err != nil {
		t.Fatalf("NewAgentManager() error = %v", err)
	}
	return mgr
}

func TestResolveProcess_EmptyThreadID(t *testing.T) {
	a := &Adapter{}
	_, err := a.resolveProcess("Test.resolveProcess", "   ")
	if err == nil || !strings.Contains(err.Error(), "threadId is required") {
		t.Fatalf("resolveProcess() err = %v, want threadId is required", err)
	}
}

func TestResolveProcess_ResolverNotConfigured(t *testing.T) {
	var a *Adapter
	_, err := a.resolveProcess("Test.resolveProcess", "thread-1")
	if err == nil || !strings.Contains(err.Error(), "thread resolver is not configured") {
		t.Fatalf("resolveProcess() err = %v, want resolver not configured", err)
	}
}

func TestResolveProcess_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	a := &Adapter{ctx: testDeps(mgr)}
	_, err := a.resolveProcess("Test.resolveProcess", "thread-404")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("resolveProcess() err = %v, want not found", err)
	}
}

func TestWithProcess_Success(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.Launch(context.Background(), "thread-1", "thread-1", "", ".", "", nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("thread-1") })

	a := &Adapter{ctx: testDeps(mgr)}
	got, err := withProcess(a, "Test.withProcess", "thread-1", func(proc *runner.AgentProcess) (string, error) {
		if proc == nil {
			t.Fatal("proc is nil")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("withProcess() error = %v", err)
	}
	if got != "ok" {
		t.Fatalf("withProcess() = %q, want %q", got, "ok")
	}
}

func TestWithProcess_TrimmedThreadID(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.Launch(context.Background(), "thread-1", "thread-1", "", ".", "", nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("thread-1") })

	a := &Adapter{ctx: testDeps(mgr)}
	_, err := withProcess(a, "Test.withProcess", "  thread-1  ", func(proc *runner.AgentProcess) (int, error) {
		if proc == nil {
			t.Fatal("proc is nil")
		}
		return 1, nil
	})
	if err != nil {
		t.Fatalf("withProcess() error = %v", err)
	}
}

func TestWithProcess_PropagatesFnError(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.Launch(context.Background(), "thread-1", "thread-1", "", ".", "", nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("thread-1") })

	a := &Adapter{ctx: testDeps(mgr)}
	_, err := withProcess(a, "Test.withProcess", "thread-1", func(*runner.AgentProcess) (int, error) {
		return 0, errors.New("inner error")
	})
	if err == nil || !strings.Contains(err.Error(), "inner error") {
		t.Fatalf("withProcess() err = %v, want inner error", err)
	}
}

package apiserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/runner"
)

func TestResolveSlashCommandThread_UsesExistingProcess(t *testing.T) {
	existing := &runner.AgentProcess{ID: "thread-1"}
	ensureCalled := false

	proc, err := resolveSlashCommandThread(
		context.Background(),
		" thread-1 ",
		func(id string) *runner.AgentProcess {
			if id == "thread-1" {
				return existing
			}
			return nil
		},
		func(_ context.Context, _, _ string) (*runner.AgentProcess, error) {
			ensureCalled = true
			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf("resolveSlashCommandThread error = %v, want nil", err)
	}
	if proc != existing {
		t.Fatalf("resolveSlashCommandThread proc = %p, want %p", proc, existing)
	}
	if ensureCalled {
		t.Fatalf("ensureReady should not be called when process exists")
	}
}

func TestResolveSlashCommandThread_EnsuresWhenProcessMissing(t *testing.T) {
	ensured := &runner.AgentProcess{ID: "thread-2"}
	called := false
	calledID := ""
	calledCwd := ""

	proc, err := resolveSlashCommandThread(
		context.Background(),
		"thread-2",
		func(string) *runner.AgentProcess { return nil },
		func(_ context.Context, threadID, cwd string) (*runner.AgentProcess, error) {
			called = true
			calledID = threadID
			calledCwd = cwd
			return ensured, nil
		},
	)
	if err != nil {
		t.Fatalf("resolveSlashCommandThread error = %v, want nil", err)
	}
	if !called {
		t.Fatalf("ensureReady was not called")
	}
	if calledID != "thread-2" {
		t.Fatalf("ensureReady threadID = %q, want %q", calledID, "thread-2")
	}
	if calledCwd != "" {
		t.Fatalf("ensureReady cwd = %q, want empty", calledCwd)
	}
	if proc != ensured {
		t.Fatalf("resolveSlashCommandThread proc = %p, want %p", proc, ensured)
	}
}

func TestResolveSlashCommandThread_PropagatesEnsureError(t *testing.T) {
	proc, err := resolveSlashCommandThread(
		context.Background(),
		"thread-3",
		func(string) *runner.AgentProcess { return nil },
		func(_ context.Context, _, _ string) (*runner.AgentProcess, error) {
			return nil, errors.New("ensure failed")
		},
	)
	if err == nil {
		t.Fatal("resolveSlashCommandThread error = nil, want non-nil")
	}
	if proc != nil {
		t.Fatalf("resolveSlashCommandThread proc = %v, want nil", proc)
	}
	if !strings.Contains(err.Error(), "ensure failed") {
		t.Fatalf("resolveSlashCommandThread error = %v, want contains %q", err, "ensure failed")
	}
}

func TestResolveSlashCommandThread_RejectsEmptyThreadID(t *testing.T) {
	called := false
	proc, err := resolveSlashCommandThread(
		context.Background(),
		"   ",
		func(string) *runner.AgentProcess {
			called = true
			return nil
		},
		func(_ context.Context, _, _ string) (*runner.AgentProcess, error) {
			called = true
			return nil, nil
		},
	)
	if err == nil {
		t.Fatal("resolveSlashCommandThread error = nil, want non-nil")
	}
	if proc != nil {
		t.Fatalf("resolveSlashCommandThread proc = %v, want nil", proc)
	}
	if called {
		t.Fatalf("callbacks should not be called for empty thread id")
	}
}

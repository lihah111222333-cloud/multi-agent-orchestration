package executor

import (
	"context"
	"strings"
	"testing"
)

func TestRunShellCommandSuccess(t *testing.T) {
	output, exitCode, err := runShellCommand(context.Background(), "echo hello", 5)
	if err != nil {
		t.Fatalf("runShellCommand error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if !strings.Contains(output, "hello") {
		t.Fatalf("output = %q, expected hello", output)
	}
}

func TestRunShellCommandFailureExitCode(t *testing.T) {
	_, exitCode, err := runShellCommand(context.Background(), "exit 7", 5)
	if err == nil {
		t.Fatal("expected non-nil error on failing command")
	}
	if exitCode != 7 {
		t.Fatalf("exitCode = %d, want 7", exitCode)
	}
}

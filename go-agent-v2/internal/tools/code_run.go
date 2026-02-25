package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// CodeRunTools builds code_run and code_run_test tool definitions.
func CodeRunTools(provider CodeRunProvider, runtime AgentRuntimeProvider, approvals ApprovalProvider) []Tool {
	if provider == nil || provider.CodeRunner() == nil {
		return nil
	}

	return []Tool{
		{
			Schema: agentcore.DynamicTool{
				Name:        "code_run",
				Description: "Execute a code snippet (Go, JavaScript, TypeScript) or a project shell command. Go snippets can be auto-wrapped with main function and imports. Use mode='project_cmd' for shell commands (only high-risk commands require approval).",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"language":  map[string]any{"type": "string", "description": "Language: go, javascript, typescript. Required for run mode."},
						"code":      map[string]any{"type": "string", "description": "Code snippet to execute (for run mode)"},
						"command":   map[string]any{"type": "string", "description": "Shell command (for project_cmd mode)"},
						"mode":      map[string]any{"type": "string", "enum": []string{"run", "project_cmd"}, "description": "Execution mode. Default: run"},
						"auto_wrap": map[string]any{"type": "boolean", "description": "Auto-wrap Go code with package main and imports. Default: true for Go"},
						"work_dir":  map[string]any{"type": "string", "description": "Custom working directory (must be within project root)"},
						"timeout":   map[string]any{"type": "number", "description": "Timeout in seconds. Default: 30"},
					},
					"required": []string{"mode"},
				},
			},
			Handler: func(ctx ToolCallContext, args json.RawMessage) string {
				return handleCodeRun(ctx, provider, runtime, approvals, args)
			},
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "code_run_test",
				Description: "Run a specific Go test function. Equivalent to: go test -v -run ^TestFunc$ [package]",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"test_func": map[string]any{"type": "string", "description": "Test function name (e.g. TestMyFunction)"},
						"test_pkg":  map[string]any{"type": "string", "description": "Package path (e.g. ./internal/executor/). Default: ./..."},
						"timeout":   map[string]any{"type": "number", "description": "Timeout in seconds. Default: 30"},
					},
					"required": []string{"test_func"},
				},
			},
			Handler: func(ctx ToolCallContext, args json.RawMessage) string {
				return handleCodeRunTest(ctx, provider, runtime, args)
			},
		},
	}
}

func handleCodeRun(callCtx ToolCallContext, provider CodeRunProvider, runtime AgentRuntimeProvider, approvals ApprovalProvider, args json.RawMessage) string {
	runner, cleanupRunner, runnerErr := resolveCodeRunner(provider, runtime, callCtx.AgentID)
	if runnerErr != nil {
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, runnerErr.Error())
	}
	defer cleanupRunner()

	var p struct {
		Language string  `json:"language"`
		Code     string  `json:"code"`
		Command  string  `json:"command"`
		Mode     string  `json:"mode"`
		AutoWrap *bool   `json:"auto_wrap,omitempty"`
		WorkDir  string  `json:"work_dir"`
		Timeout  float64 `json:"timeout"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(err)
	}

	if p.Mode == "" {
		p.Mode = executor.ModeRun
	}

	resolvedCallID := util.ResolveCodeRunCallID(callCtx.CallID, callCtx.RequestID)

	if p.Mode == executor.ModeProjectCmd {
		isDangerous := executor.DetectDangerous(p.Command) != ""
		if isDangerous {
			approved := approvals != nil && approvals.AwaitApproval(callCtx.AgentID, resolvedCallID, p.Mode, p.Command, isDangerous)
			if !approved {
				writeCodeRunAudit(provider, callCtx.AgentID, p.Language, p.Mode, "denied", 0, 0, p.Code, p.Command, "")
				return `{"error":"execution denied by user","exit_code":-1}`
			}
		}
	}

	autoWrap := p.Mode == executor.ModeRun && strings.EqualFold(p.Language, "go")
	if p.AutoWrap != nil {
		autoWrap = *p.AutoWrap
	}

	workDir := strings.TrimSpace(p.WorkDir)
	if workDir == "" && runtime != nil {
		workDir = runtime.GetAgentWorkDir(callCtx.AgentID)
	}
	timeout := parseCodeRunTimeout(p.Timeout)

	result, err := runner.Run(contextOrBackground(callCtx.Ctx), executor.RunRequest{
		Language: p.Language,
		Code:     p.Code,
		Command:  p.Command,
		Mode:     p.Mode,
		AutoWrap: autoWrap,
		WorkDir:  workDir,
		Timeout:  timeout,
	})
	if err != nil {
		writeCodeRunAudit(provider, callCtx.AgentID, p.Language, p.Mode, "failed", -1, 0, p.Code, p.Command, err.Error())
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, err.Error())
	}

	writeCodeRunAudit(provider, callCtx.AgentID, p.Language, p.Mode, resultStatus(result),
		result.ExitCode, result.Duration.Milliseconds(), p.Code, p.Command, result.Output)

	return codeRunResultJSON(result)
}

func handleCodeRunTest(callCtx ToolCallContext, provider CodeRunProvider, runtime AgentRuntimeProvider, args json.RawMessage) string {
	runner, cleanupRunner, runnerErr := resolveCodeRunner(provider, runtime, callCtx.AgentID)
	if runnerErr != nil {
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, runnerErr.Error())
	}
	defer cleanupRunner()

	var p struct {
		TestFunc string  `json:"test_func"`
		TestPkg  string  `json:"test_pkg"`
		Timeout  float64 `json:"timeout"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(err)
	}

	timeout := parseCodeRunTimeout(p.Timeout)

	result, err := runner.Run(contextOrBackground(callCtx.Ctx), executor.RunRequest{
		Mode:     executor.ModeTest,
		TestFunc: p.TestFunc,
		TestPkg:  p.TestPkg,
		Timeout:  timeout,
	})
	if err != nil {
		writeCodeRunAudit(provider, callCtx.AgentID, "go", executor.ModeTest, "failed", -1, 0, "", p.TestFunc, err.Error())
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, err.Error())
	}

	writeCodeRunAudit(provider, callCtx.AgentID, "go", executor.ModeTest, resultStatus(result),
		result.ExitCode, result.Duration.Milliseconds(), "", p.TestFunc, result.Output)

	return codeRunResultJSON(result)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func parseCodeRunTimeout(seconds float64) time.Duration {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0
	}
	ns := seconds * float64(time.Second)
	if ns >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	timeout := time.Duration(ns)
	if timeout <= 0 {
		return time.Nanosecond
	}
	return timeout
}

func samePath(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

// NormalizeAgentWorkDir resolves and cleans a working directory path.
func NormalizeAgentWorkDir(cwd string) string {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		return ""
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(abs)
}

func resolveCodeRunner(provider CodeRunProvider, runtime AgentRuntimeProvider, agentID string) (*executor.CodeRunner, func(), error) {
	if provider == nil {
		return nil, nil, fmt.Errorf("code runner not available")
	}
	defaultRunner := provider.CodeRunner()
	agentCwd := ""
	if runtime != nil {
		agentCwd = NormalizeAgentWorkDir(runtime.GetAgentWorkDir(agentID))
	}
	if agentCwd == "" {
		if defaultRunner == nil {
			return nil, nil, fmt.Errorf("code runner not available")
		}
		return defaultRunner, func() {}, nil
	}

	if defaultRunner != nil && samePath(defaultRunner.WorkDir(), agentCwd) {
		return defaultRunner, func() {}, nil
	}

	runner, err := executor.NewCodeRunner(agentCwd)
	if err != nil {
		if defaultRunner != nil {
			logger.Warn("code-run: agent runner init failed, fallback to default runner",
				logger.FieldAgentID, agentID,
				logger.FieldCwd, agentCwd,
				logger.FieldError, err,
			)
			return defaultRunner, func() {}, nil
		}
		return nil, nil, err
	}
	return runner, runner.Cleanup, nil
}

func writeCodeRunAudit(provider CodeRunProvider, agentID, language, mode, result string, exitCode int, durationMS int64, code, command, output string) {
	if provider == nil || provider.AuditLogStore() == nil {
		return
	}

	extra := map[string]any{
		"exit_code":        exitCode,
		"duration_ms":      durationMS,
		"language":         language,
		"output_truncated": len(output) > executor.MaxAuditPayloadSize(),
	}
	if code != "" {
		extra["code"] = executor.TruncateForAudit(code, 0)
	}
	if command != "" {
		extra["command"] = executor.TruncateForAudit(command, 0)
	}
	if output != "" {
		extra["output"] = executor.TruncateForAudit(output, 0)
	}

	event := &store.AuditEvent{
		EventType: "code_run",
		Action:    mode,
		Result:    result,
		Actor:     agentID,
		Target:    language + "/" + mode,
		Detail:    fmt.Sprintf("exit_code=%d duration_ms=%d", exitCode, durationMS),
		Level:     "INFO",
		Extra:     extra,
	}
	if err := provider.AuditLogStore().Append(context.Background(), event); err != nil {
		logger.Warn("code-run: audit write failed", logger.FieldAgentID, agentID, logger.FieldError, err)
	}
}

func resultStatus(r *executor.RunResult) string {
	if r.Success {
		return "success"
	}
	return "failed"
}

func codeRunResultJSON(r *executor.RunResult) string {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal result: %s"}`, err.Error())
	}
	return string(data)
}

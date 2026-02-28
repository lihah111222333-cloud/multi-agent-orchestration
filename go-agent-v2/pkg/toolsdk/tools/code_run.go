package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	codeRunModeRun         = "run"
	codeRunModeProjectCmd  = "project_cmd"
	codeRunModeTest        = "test"
	codeRunMaxAuditPayload = 4096
)

var codeRunDangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[;&|()\s])rm\s+-rf(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|[;&|()\s])shutdown(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|[;&|()\s])reboot(?:\s|$)`),
	regexp.MustCompile(`(?i)curl[^\n|]*\|\s*(?:bash|sh)(?:\s|$)`),
	regexp.MustCompile(`(?i)wget[^\n|]*\|\s*(?:bash|sh)(?:\s|$)`),
}

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
						"mode":      map[string]any{"type": "string", "enum": []string{codeRunModeRun, codeRunModeProjectCmd}, "description": "Execution mode. Default: run"},
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
						"test_pkg":  map[string]any{"type": "string", "description": "Package path (e.g. ./internal/engine/executor/). Default: ./..."},
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
	runner, err := resolveCodeRunner(provider, runtime, callCtx.AgentID)
	if err != nil {
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, err.Error())
	}

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
		p.Mode = codeRunModeRun
	}
	if p.Mode == codeRunModeProjectCmd && detectDangerous(p.Command) != "" {
		resolvedCallID := util.ResolveCodeRunCallID(callCtx.CallID, callCtx.RequestID)
		if approvals == nil || !approvals.AwaitApproval(callCtx.AgentID, resolvedCallID, p.Mode, p.Command, true) {
			writeCodeRunAudit(provider, callCtx.AgentID, p.Language, p.Mode, "denied", 0, 0, p.Code, p.Command, "")
			return `{"error":"execution denied by user","exit_code":-1}`
		}
	}

	autoWrap := p.Mode == codeRunModeRun && strings.EqualFold(p.Language, "go")
	if p.AutoWrap != nil {
		autoWrap = *p.AutoWrap
	}

	workDir := strings.TrimSpace(p.WorkDir)
	if workDir == "" && runtime != nil {
		workDir = runtime.GetAgentWorkDir(callCtx.AgentID)
	}
	timeout := parseCodeRunTimeout(p.Timeout)

	result, err := runner.Run(contextOrBackground(callCtx.Ctx), CodeRunRequest{
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
	runner, err := resolveCodeRunner(provider, runtime, callCtx.AgentID)
	if err != nil {
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, err.Error())
	}

	var p struct {
		TestFunc string  `json:"test_func"`
		TestPkg  string  `json:"test_pkg"`
		Timeout  float64 `json:"timeout"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(err)
	}
	timeout := parseCodeRunTimeout(p.Timeout)

	result, err := runner.Run(contextOrBackground(callCtx.Ctx), CodeRunRequest{
		Mode:     codeRunModeTest,
		TestFunc: p.TestFunc,
		TestPkg:  p.TestPkg,
		Timeout:  timeout,
	})
	if err != nil {
		writeCodeRunAudit(provider, callCtx.AgentID, "go", codeRunModeTest, "failed", -1, 0, "", p.TestFunc, err.Error())
		return fmt.Sprintf(`{"error":%q,"exit_code":-1}`, err.Error())
	}

	writeCodeRunAudit(provider, callCtx.AgentID, "go", codeRunModeTest, resultStatus(result),
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

type workDirCodeRunner struct {
	base    CodeExecRunner
	workDir string
}

func (w workDirCodeRunner) Run(ctx context.Context, req CodeRunRequest) (*CodeRunResult, error) {
	if strings.TrimSpace(req.WorkDir) == "" {
		req.WorkDir = w.workDir
	}
	return w.base.Run(ctx, req)
}

func resolveCodeRunner(provider CodeRunProvider, runtime AgentRuntimeProvider, agentID string) (CodeExecRunner, error) {
	if provider == nil {
		return nil, fmt.Errorf("code runner not available")
	}
	runner := provider.CodeRunner()
	if runner == nil {
		return nil, fmt.Errorf("code runner not available")
	}
	agentCwd := ""
	if runtime != nil {
		agentCwd = NormalizeAgentWorkDir(runtime.GetAgentWorkDir(agentID))
	}
	if agentCwd == "" {
		return runner, nil
	}
	return workDirCodeRunner{base: runner, workDir: agentCwd}, nil
}

func writeCodeRunAudit(provider CodeRunProvider, agentID, language, mode, result string, exitCode int, durationMS int64, code, command, output string) {
	if provider == nil || provider.AuditLogger() == nil {
		return
	}
	extra := map[string]any{
		"exit_code":        exitCode,
		"duration_ms":      durationMS,
		"language":         language,
		"output_truncated": len(output) > codeRunMaxAuditPayload,
	}
	if code != "" {
		extra["code"] = truncateForAudit(code, 0)
	}
	if command != "" {
		extra["command"] = truncateForAudit(command, 0)
	}
	if output != "" {
		extra["output"] = truncateForAudit(output, 0)
	}

	if err := provider.AuditLogger().Append(context.Background(), &AuditEvent{
		Ts:        time.Now(),
		EventType: "code_run",
		Action:    mode,
		Result:    result,
		Actor:     agentID,
		Target:    language + "/" + mode,
		Detail:    fmt.Sprintf("exit_code=%d duration_ms=%d", exitCode, durationMS),
		Level:     "INFO",
		Extra:     extra,
	}); err != nil {
		logger.Warn("code-run: audit write failed", logger.FieldAgentID, agentID, logger.FieldError, err)
	}
}

func detectDangerous(command string) string {
	for _, p := range codeRunDangerousPatterns {
		if p.MatchString(command) {
			return p.String()
		}
	}
	return ""
}

func truncateForAudit(s string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = codeRunMaxAuditPayload
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...[truncated]"
}

func resultStatus(r *CodeRunResult) string {
	if r != nil && r.Success {
		return "success"
	}
	return "failed"
}

func codeRunResultJSON(r *CodeRunResult) string {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal result: %s"}`, err.Error())
	}
	return string(data)
}

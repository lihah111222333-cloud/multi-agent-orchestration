package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	maxConcurrentRuns = 3
	defaultRunTimeout = 30 * time.Second
	maxOutputBytes    = 512 * 1024 // 512KB
	maxAuditPayload   = 4096
)

const (
	ModeRun        = "run"
	ModeTest       = "test"
	ModeProjectCmd = "project_cmd"
)

type CodeRunner struct {
	workDir  string
	hasNode  bool
	hasTsx   bool
	sem      chan struct{}
	tempRoot string
}

type RunRequest struct {
	Language string        `json:"language"`
	Code     string        `json:"code,omitempty"`
	Command  string        `json:"command,omitempty"`
	Mode     string        `json:"mode"`
	AutoWrap bool          `json:"auto_wrap"`
	TestFunc string        `json:"test_func,omitempty"`
	TestPkg  string        `json:"test_pkg,omitempty"`
	WorkDir  string        `json:"work_dir,omitempty"`
	Timeout  time.Duration `json:"timeout,omitempty"`
}

type RunResult struct {
	Success   bool          `json:"success"`
	Output    string        `json:"output"`
	ExitCode  int           `json:"exit_code"`
	Duration  time.Duration `json:"duration"`
	Language  string        `json:"language"`
	Mode      string        `json:"mode"`
	Truncated bool          `json:"truncated"`
}

func NewCodeRunner(workDir string) (*CodeRunner, error) {
	if workDir == "" {
		workDir = "."
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return nil, pkgerr.Wrap(err, "NewCodeRunner", "resolve workDir")
	}

	tempParent := filepath.Join(abs, ".codex", "code_exec")
	if mkErr := os.MkdirAll(tempParent, 0o755); mkErr != nil {
		logger.Warn("code-runner: create in-workspace temp parent failed, fallback to os temp",
			logger.FieldPath, tempParent,
			logger.FieldError, mkErr,
		)
		tempParent = ""
	}

	tempPattern := "code_exec_"
	if tempParent != "" {
		tempPattern = "session_"
	}
	tempRoot, err := os.MkdirTemp(tempParent, tempPattern)
	if err != nil {
		return nil, pkgerr.Wrap(err, "NewCodeRunner", "create tempRoot")
	}

	r := &CodeRunner{
		workDir:  abs,
		hasNode:  commandExists("node"),
		hasTsx:   commandExists("tsx") || resolveLocalTsxPath(abs) != "",
		sem:      make(chan struct{}, maxConcurrentRuns),
		tempRoot: tempRoot,
	}

	logger.Info("code-runner: initialized",
		logger.FieldCwd, abs,
		"has_node", r.hasNode,
		"has_tsx", r.hasTsx,
		"temp_root", tempRoot,
	)
	return r, nil
}

func (r *CodeRunner) HasNode() bool { return r.hasNode }

func (r *CodeRunner) HasTsx() bool { return r.hasTsx }

func (r *CodeRunner) WorkDir() string { return r.workDir }

func (r *CodeRunner) Cleanup() {
	if r.tempRoot != "" {
		if err := os.RemoveAll(r.tempRoot); err != nil {
			logger.Warn("code-runner: cleanup tempRoot failed", logger.FieldError, err, logger.FieldPath, r.tempRoot)
		}
	}
}

func (r *CodeRunner) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	if req.Timeout <= 0 {
		req.Timeout = defaultRunTimeout
	}

	if req.WorkDir == "" {
		req.WorkDir = r.workDir
	} else if err := r.validateWorkDir(req.WorkDir); err != nil {
		return nil, err
	}

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	start := time.Now()

	var result *RunResult
	var err error

	switch req.Mode {
	case ModeRun:
		result, err = r.dispatchRun(ctx, req)
	case ModeTest:
		result, err = r.runGoTest(ctx, req)
	case ModeProjectCmd:
		result, err = r.runProjectCmd(ctx, req)
	default:
		return nil, pkgerr.Newf("CodeRunner.Run", "unknown mode: %s", req.Mode)
	}

	if err != nil {
		return nil, err
	}
	result.Duration = time.Since(start)

	logger.Info("code-runner: completed",
		logger.FieldLanguage, result.Language,
		"mode", result.Mode,
		logger.FieldExitCode, result.ExitCode,
		logger.FieldDurationMS, result.Duration.Milliseconds(),
		"output_len", len(result.Output),
		"truncated", result.Truncated,
	)
	return result, nil
}

func (r *CodeRunner) dispatchRun(ctx context.Context, req RunRequest) (*RunResult, error) {
	lang := strings.ToLower(strings.TrimSpace(req.Language))
	switch lang {
	case "go", "golang":
		return r.runGo(ctx, req)
	case "javascript", "js":
		return r.runJS(ctx, req)
	case "typescript", "ts":
		return r.runTS(ctx, req)
	default:
		return nil, pkgerr.Newf("CodeRunner.Run", "unsupported language: %s", req.Language)
	}
}

func (r *CodeRunner) runGo(ctx context.Context, req RunRequest) (*RunResult, error) {
	dir, err := os.MkdirTemp(r.tempRoot, "go_")
	if err != nil {
		return nil, pkgerr.Wrap(err, "CodeRunner.runGo", "mkdir")
	}
	defer r.cleanup(dir)

	code := req.Code
	if req.AutoWrap {
		code = wrapGoMain(code)
	}

	mainFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(mainFile, []byte(code), 0o644); err != nil {
		return nil, pkgerr.Wrap(err, "CodeRunner.runGo", "write main.go")
	}

	output, exitCode, truncated := r.execCommand(ctx, req.Timeout, req.WorkDir, "go", "run", mainFile)
	return &RunResult{
		Success:   exitCode == 0,
		Output:    output,
		ExitCode:  exitCode,
		Language:  "go",
		Mode:      ModeRun,
		Truncated: truncated,
	}, nil
}

func (r *CodeRunner) runGoTest(ctx context.Context, req RunRequest) (*RunResult, error) {
	testFunc := strings.TrimSpace(req.TestFunc)
	if testFunc == "" {
		return nil, pkgerr.New("CodeRunner.runGoTest", "test_func is required")
	}
	pkg := req.TestPkg
	if pkg == "" {
		pkg = "./..."
	}

	pattern := "^" + regexp.QuoteMeta(testFunc) + "$"
	output, exitCode, truncated := r.execCommand(ctx, req.Timeout, req.WorkDir, "go", "test", "-v", "-run", pattern, pkg)
	return &RunResult{
		Success:   exitCode == 0,
		Output:    output,
		ExitCode:  exitCode,
		Language:  "go",
		Mode:      ModeTest,
		Truncated: truncated,
	}, nil
}

func (r *CodeRunner) runJS(ctx context.Context, req RunRequest) (*RunResult, error) {
	if !r.hasNode {
		return nil, pkgerr.New("CodeRunner.runJS", "node not available on PATH")
	}

	dir, err := os.MkdirTemp(r.tempRoot, "js_")
	if err != nil {
		return nil, pkgerr.Wrap(err, "CodeRunner.runJS", "mkdir")
	}
	defer r.cleanup(dir)

	scriptFile := filepath.Join(dir, "script.js")
	if err := os.WriteFile(scriptFile, []byte(req.Code), 0o644); err != nil {
		return nil, pkgerr.Wrap(err, "CodeRunner.runJS", "write script.js")
	}

	output, exitCode, truncated := r.execCommand(ctx, req.Timeout, dir, "node", scriptFile)
	return &RunResult{
		Success:   exitCode == 0,
		Output:    output,
		ExitCode:  exitCode,
		Language:  "javascript",
		Mode:      ModeRun,
		Truncated: truncated,
	}, nil
}

func (r *CodeRunner) runTS(ctx context.Context, req RunRequest) (*RunResult, error) {
	if !r.hasTsx {
		return nil, pkgerr.New("CodeRunner.runTS", "tsx not available on PATH or node_modules/.bin/tsx")
	}

	dir, err := os.MkdirTemp(r.tempRoot, "ts_")
	if err != nil {
		return nil, pkgerr.Wrap(err, "CodeRunner.runTS", "mkdir")
	}
	defer r.cleanup(dir)

	scriptFile := filepath.Join(dir, "script.ts")
	if err := os.WriteFile(scriptFile, []byte(req.Code), 0o644); err != nil {
		return nil, pkgerr.Wrap(err, "CodeRunner.runTS", "write script.ts")
	}

	name := "tsx"
	if !commandExists(name) {
		name = resolveLocalTsxPath(r.workDir)
		if name == "" {
			return nil, pkgerr.New("CodeRunner.runTS", "tsx not available on PATH or node_modules/.bin/tsx")
		}
	}

	output, exitCode, truncated := r.execCommand(ctx, req.Timeout, dir, name, scriptFile)
	return &RunResult{
		Success:   exitCode == 0,
		Output:    output,
		ExitCode:  exitCode,
		Language:  "typescript",
		Mode:      ModeRun,
		Truncated: truncated,
	}, nil
}

func (r *CodeRunner) runProjectCmd(ctx context.Context, req RunRequest) (*RunResult, error) {
	if strings.TrimSpace(req.Command) == "" {
		return nil, pkgerr.New("CodeRunner.runProjectCmd", "command is required")
	}

	output, exitCode, truncated := r.execCommand(ctx, req.Timeout, req.WorkDir, "sh", "-c", req.Command)
	return &RunResult{
		Success:   exitCode == 0,
		Output:    output,
		ExitCode:  exitCode,
		Language:  "shell",
		Mode:      ModeProjectCmd,
		Truncated: truncated,
	}, nil
}

var importHintRe = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.[A-Z]`)

var stdlibPackages = map[string]string{
	"fmt":      "fmt",
	"strings":  "strings",
	"strconv":  "strconv",
	"math":     "math",
	"sort":     "sort",
	"os":       "os",
	"io":       "io",
	"time":     "time",
	"regexp":   "regexp",
	"bytes":    "bytes",
	"bufio":    "bufio",
	"encoding": "encoding",
	"json":     "encoding/json",
	"xml":      "encoding/xml",
	"csv":      "encoding/csv",
	"http":     "net/http",
	"url":      "net/url",
	"filepath": "path/filepath",
	"path":     "path",
	"reflect":  "reflect",
	"errors":   "errors",
	"log":      "log",
	"sync":     "sync",
	"atomic":   "sync/atomic",
	"context":  "context",
	"rand":     "math/rand",
	"unicode":  "unicode",
	"utf8":     "unicode/utf8",
	"base64":   "encoding/base64",
	"hex":      "encoding/hex",
	"binary":   "encoding/binary",
	"hash":     "hash",
}

func wrapGoMain(code string) string {
	trimmed := strings.TrimSpace(code)
	if strings.HasPrefix(trimmed, "package ") {
		return code
	}

	var codeLines []string
	for line := range strings.SplitSeq(trimmed, "\n") {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}
		codeLines = append(codeLines, line)
	}
	codeForScan := strings.Join(codeLines, "\n")
	matches := importHintRe.FindAllStringSubmatch(codeForScan, -1)
	seen := make(map[string]bool)
	var imports []string
	for _, m := range matches {
		pkgAlias := m[1]
		if seen[pkgAlias] {
			continue
		}
		seen[pkgAlias] = true
		if fullPath, ok := stdlibPackages[pkgAlias]; ok {
			imports = append(imports, fmt.Sprintf("\t%q", fullPath))
		}
	}

	var sb strings.Builder
	sb.WriteString("package main\n\n")

	if len(imports) > 0 {
		sb.WriteString("import (\n")
		for _, imp := range imports {
			sb.WriteString(imp)
			sb.WriteString("\n")
		}
		sb.WriteString(")\n\n")
	}

	if strings.Contains(trimmed, "func main()") {
		sb.WriteString(trimmed)
	} else {
		sb.WriteString("func main() {\n")
		sb.WriteString(trimmed)
		sb.WriteString("\n}\n")
	}

	return sb.String()
}

func (r *CodeRunner) execCommand(ctx context.Context, timeout time.Duration, dir string, name string, args ...string) (output string, exitCode int, truncated bool) {
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, name, args...)
	cmd.Dir = dir

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		r.killProcessGroup(cmd)
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	var combined bytes.Buffer
	lw := util.NewLimitedWriter(&combined, maxOutputBytes)
	cmd.Stdout = lw
	cmd.Stderr = lw

	if err := cmd.Run(); err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			output = combined.String() + "\n--- TIMEOUT ---\n"
			return output, -1, lw.Overflow()
		}
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return combined.String(), exitCode, lw.Overflow()
}

func (r *CodeRunner) killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		logger.Debug("code-runner: kill process group failed", logger.FieldPID, cmd.Process.Pid, logger.FieldError, err)
	}
}

func (r *CodeRunner) validateWorkDir(dir string) error {
	rootAbs, err := filepath.Abs(r.workDir)
	if err != nil {
		return pkgerr.Wrap(err, "CodeRunner.validateWorkDir", "resolve project root")
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return pkgerr.Wrap(err, "CodeRunner.validateWorkDir", "resolve path")
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return pkgerr.Wrap(err, "CodeRunner.validateWorkDir", "compute relative path")
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return pkgerr.Newf("CodeRunner.validateWorkDir", "path %q is outside project root %q", dir, rootAbs)
	}

	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return pkgerr.Wrap(err, "CodeRunner.validateWorkDir", "resolve project root symlink")
	}

	pathReal, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return pkgerr.Wrap(err, "CodeRunner.validateWorkDir", "resolve path symlink")
	}

	realRel, err := filepath.Rel(rootReal, pathReal)
	if err != nil {
		return pkgerr.Wrap(err, "CodeRunner.validateWorkDir", "compute real relative path")
	}
	realRel = filepath.Clean(realRel)
	if realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
		return pkgerr.Newf("CodeRunner.validateWorkDir", "path %q is outside project root %q", dir, rootAbs)
	}
	return nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func resolveLocalTsxPath(workDir string) string {
	candidate := filepath.Join(workDir, "node_modules", ".bin", "tsx")
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return ""
	}
	if info.Mode().Perm()&0o111 == 0 {
		return ""
	}
	return candidate
}

func (r *CodeRunner) cleanup(path string) {
	if err := os.RemoveAll(path); err != nil {
		logger.Debug("code-runner: cleanup failed", logger.FieldPath, path, logger.FieldError, err)
	}
}

func TruncateForAudit(s string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = maxAuditPayload
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...[truncated]"
}

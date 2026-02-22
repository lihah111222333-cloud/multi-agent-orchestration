// code_runner.go — 代码块执行引擎: AI agent 生成的代码片段隔离执行。
//
// 支持语言: Go (run/test) · JavaScript (node) · TypeScript (npx tsx)
// 安全约束: 临时目录隔离 · 进程组管理 · 信号量限流 · 输出聚合裁剪
// 审批范围: 仅 project_cmd 强制审批; run/test 受隔离+超时+输出上限约束
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

// ========================================
// 常量
// ========================================

const (
	// maxConcurrentRuns 信号量容量 — 单实例最大并发执行数。
	maxConcurrentRuns = 3

	// defaultRunTimeout Run() 入口默认超时 (秒)。
	defaultRunTimeout = 30 * time.Second

	// maxOutputBytes stdout+stderr 聚合输出上限。
	maxOutputBytes = 512 * 1024 // 512KB

	// maxAuditPayload 审计写入时代码/命令/输出的裁剪上限。
	maxAuditPayload = 4096
)

// ========================================
// 执行模式
// ========================================

const (
	ModeRun        = "run"         // 直接执行代码片段
	ModeTest       = "test"        // go test -run
	ModeProjectCmd = "project_cmd" // sh -c (强制审批)
)

// ========================================
// 类型定义
// ========================================

// CodeRunner 代码块执行引擎。
//
// 设计:
//   - 每个实例拥有独立的临时目录根 (tempRoot), 互不干扰
//   - 信号量 (sem) 限制并发执行数, 防止资源耗尽
//   - 语言可用性在创建时探测, 不可用的语言不注册工具
type CodeRunner struct {
	workDir  string        // 项目工作目录 (go test/project_cmd 的 cmd.Dir)
	hasNode  bool          // node 可用
	hasTsx   bool          // tsx 可用 (PATH 或 node_modules/.bin/tsx)
	sem      chan struct{} // 并发信号量
	tempRoot string        // 实例级临时目录根
}

// RunRequest 执行请求。
type RunRequest struct {
	Language string        `json:"language"`            // go, javascript, typescript
	Code     string        `json:"code,omitempty"`      // 代码片段 (run 模式)
	Command  string        `json:"command,omitempty"`   // shell 命令 (project_cmd 模式)
	Mode     string        `json:"mode"`                // run, test, project_cmd
	AutoWrap bool          `json:"auto_wrap"`           // 自动包裹 main 函数 (Go)
	TestFunc string        `json:"test_func,omitempty"` // go test -run 目标
	TestPkg  string        `json:"test_pkg,omitempty"`  // go test 包路径
	WorkDir  string        `json:"work_dir,omitempty"`  // 自定义工作目录 (需校验)
	Timeout  time.Duration `json:"timeout,omitempty"`   // 超时 (零值 → defaultRunTimeout)
}

// RunResult 执行结果。
type RunResult struct {
	Success   bool          `json:"success"`
	Output    string        `json:"output"`
	ExitCode  int           `json:"exit_code"`
	Duration  time.Duration `json:"duration"`
	Language  string        `json:"language"`
	Mode      string        `json:"mode"`
	Truncated bool          `json:"truncated"` // 输出是否被截断
}

// ========================================
// 构造
// ========================================

// NewCodeRunner 创建代码执行引擎。
//
// workDir 为项目根目录, 用于 go test 和 project_cmd 的工作目录。
// 构造时探测 node/tsx 可用性, 创建实例级临时目录。
func NewCodeRunner(workDir string) (*CodeRunner, error) {
	if workDir == "" {
		workDir = "."
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return nil, pkgerr.Wrap(err, "NewCodeRunner", "resolve workDir")
	}

	tempRoot, err := os.MkdirTemp("", "code_exec_")
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

// HasNode 返回 node 是否可用。
func (r *CodeRunner) HasNode() bool { return r.hasNode }

// HasTsx 返回 tsx 是否可用 (PATH 或 node_modules/.bin/tsx)。
func (r *CodeRunner) HasTsx() bool { return r.hasTsx }

// Cleanup 清理实例级临时目录。应在 Server 关闭时调用。
func (r *CodeRunner) Cleanup() {
	if r.tempRoot != "" {
		if err := os.RemoveAll(r.tempRoot); err != nil {
			logger.Warn("code-runner: cleanup tempRoot failed", logger.FieldError, err, logger.FieldPath, r.tempRoot)
		}
	}
}

// ========================================
// Run — 统一入口
// ========================================

// Run 执行代码块。信号量限流 → 按 Mode 分发。
func (r *CodeRunner) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	// 超时兜底
	if req.Timeout <= 0 {
		req.Timeout = defaultRunTimeout
	}

	// 工作目录校验
	if req.WorkDir != "" {
		if err := r.validateWorkDir(req.WorkDir); err != nil {
			return nil, err
		}
	}

	// 信号量限流
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

// ========================================
// 语言分发
// ========================================

// dispatchRun 按语言分发 run 模式请求。
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

// ========================================
// Go 执行
// ========================================

// runGo 执行 Go 代码片段: MkdirTemp → main.go → go run。
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

	output, exitCode, truncated := r.execCommand(ctx, req.Timeout, r.workDir, "go", "run", mainFile)
	return &RunResult{
		Success:   exitCode == 0,
		Output:    output,
		ExitCode:  exitCode,
		Language:  "go",
		Mode:      ModeRun,
		Truncated: truncated,
	}, nil
}

// runGoTest 执行 go test -v -run ^TestFunc$ TestPkg。
func (r *CodeRunner) runGoTest(ctx context.Context, req RunRequest) (*RunResult, error) {
	if req.TestFunc == "" {
		return nil, pkgerr.New("CodeRunner.runGoTest", "test_func is required")
	}
	pkg := req.TestPkg
	if pkg == "" {
		pkg = "./..."
	}

	pattern := buildGoTestPattern(req.TestFunc)
	output, exitCode, truncated := r.execCommand(ctx, req.Timeout, r.workDir, "go", "test", "-v", "-run", pattern, pkg)
	return &RunResult{
		Success:   exitCode == 0,
		Output:    output,
		ExitCode:  exitCode,
		Language:  "go",
		Mode:      ModeTest,
		Truncated: truncated,
	}, nil
}

func buildGoTestPattern(testFunc string) string {
	return "^" + regexp.QuoteMeta(strings.TrimSpace(testFunc)) + "$"
}

// ========================================
// JavaScript / TypeScript 执行
// ========================================

// runJS 执行 JavaScript 代码片段: MkdirTemp → script.js → node。
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

// runTS 执行 TypeScript 代码片段: MkdirTemp → script.ts → npx tsx。
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
		localTsx := resolveLocalTsxPath(r.workDir)
		if localTsx == "" {
			return nil, pkgerr.New("CodeRunner.runTS", "tsx not available on PATH or node_modules/.bin/tsx")
		}
		name = localTsx
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

// ========================================
// Project Command 执行
// ========================================

// runProjectCmd 执行 shell 命令 (唯一强制审批的模式)。
//
// 审批由上层 apiserver 处理, 此处仅负责执行。
func (r *CodeRunner) runProjectCmd(ctx context.Context, req RunRequest) (*RunResult, error) {
	if strings.TrimSpace(req.Command) == "" {
		return nil, pkgerr.New("CodeRunner.runProjectCmd", "command is required")
	}

	dir := r.workDir
	if req.WorkDir != "" {
		dir = req.WorkDir
	}

	output, exitCode, truncated := r.execCommand(ctx, req.Timeout, dir, "sh", "-c", req.Command)
	return &RunResult{
		Success:   exitCode == 0,
		Output:    output,
		ExitCode:  exitCode,
		Language:  "shell",
		Mode:      ModeProjectCmd,
		Truncated: truncated,
	}, nil
}

// ========================================
// wrapGoMain — 自动包裹 Go main 函数
// ========================================

// importHintRe 匹配代码中形如 pkgName.Identifier 的包引用。
var importHintRe = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.[A-Z]`)

// stdlibPackages 常用标准库包名 — 仅匹配代码中实际引用的包。
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

// wrapGoMain 将 Go 代码片段自动包裹为可执行 main 包。
//
// 规则:
//   - 已有 package 声明 → 原样返回
//   - 扫描 pkgName.Identifier 模式, 仅导入实际引用的标准库包
//   - 已有 func main() → 仅补 package header + imports
//   - 否则包裹进 func main() { ... }
func wrapGoMain(code string) string {
	trimmed := strings.TrimSpace(code)
	if strings.HasPrefix(trimmed, "package ") {
		return code
	}

	// 扫描引用的包 (过滤注释行, 减少假阳性)
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

// ========================================
// execCommand — 进程管理核心
// ========================================

// execCommand 执行外部命令, 返回聚合输出、退出码和是否截断。
//
// 安全:
//   - Setpgid=true: 创建独立进程组, 超时时 kill 整个组
//   - stdout+stderr 聚合写入同一个 LimitedWriter, 总量限制 maxOutputBytes
//   - context 超时由调用方 (Run) 设定
func (r *CodeRunner) execCommand(ctx context.Context, timeout time.Duration, dir string, name string, args ...string) (output string, exitCode int, truncated bool) {
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, name, args...)
	cmd.Dir = dir

	// 进程组隔离: 超时时 kill 整个进程组
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// 避免 "shell 被杀但子进程仍持有 pipe" 导致 Wait 长时间阻塞。
	cmd.Cancel = func() error {
		r.killProcessGroup(cmd)
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	// stdout+stderr → 聚合输出 (合计 512KB 上限)
	var combined bytes.Buffer
	lw := util.NewLimitedWriter(&combined, maxOutputBytes)
	cmd.Stdout = lw
	cmd.Stderr = lw

	err := cmd.Run()
	exitCode = 0

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			// cmd.Cancel 已 kill 进程组, 此处仅拼接超时标记
			output = combined.String() + "\n--- TIMEOUT ---\n"
			return output, -1, lw.Overflow()
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return combined.String(), exitCode, lw.Overflow()
}

// killProcessGroup 终止整个进程组 (防止子进程泄漏)。
func (r *CodeRunner) killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		logger.Debug("code-runner: kill process group failed", logger.FieldPID, cmd.Process.Pid, logger.FieldError, err)
	}
}

// ========================================
// 安全校验
// ========================================

// validateWorkDir 校验自定义工作目录是否在项目根内。
//
// 使用 filepath.Rel 判断, 拒绝 "../" 开头的路径 (防止路径穿越)。
// NEVER 用 strings.HasPrefix — 无法区分 /root/work 和 /root/work2。
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

	// 软链接校验: 防止路径字面在根内, 但解析后跳到根外。
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return pkgerr.Wrap(err, "CodeRunner.validateWorkDir", "resolve project root symlink")
	}

	pathReal, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// 不存在路径交由后续执行阶段报错; 这里仅做越界拦截。
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

// DetectDangerous 检测命令中的危险模式 (复用 command_card.go 的 dangerousPatterns)。
//
// 返回匹配的危险模式字符串, 空串表示安全。
func DetectDangerous(command string) string {
	return detectDangerous(command)
}

// ========================================
// 工具函数
// ========================================

// commandExists 检测命令是否在 PATH 中可用。
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

// cleanup 清理临时目录 (仅清理当前实例创建的目录)。
func (r *CodeRunner) cleanup(path string) {
	if err := os.RemoveAll(path); err != nil {
		logger.Debug("code-runner: cleanup failed", logger.FieldPath, path, logger.FieldError, err)
	}
}

// TruncateForAudit 裁剪字符串用于审计写入 (防止超大内容直存)。
func TruncateForAudit(s string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = maxAuditPayload
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...[truncated]"
}

// MaxAuditPayloadSize 返回审计裁剪上限 (供外部判断是否截断)。
func MaxAuditPayloadSize() int { return maxAuditPayload }

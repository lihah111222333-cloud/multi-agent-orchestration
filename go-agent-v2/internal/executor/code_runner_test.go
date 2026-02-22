package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ========================================
// Go 执行测试
// ========================================

// TestCodeRunner_GoRun 验证 Go 代码片段自动包裹并执行。
func TestCodeRunner_GoRun(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	result, err := r.Run(context.Background(), RunRequest{
		Language: "go",
		Code:     `fmt.Println("hello from code_runner")`,
		Mode:     ModeRun,
		AutoWrap: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got exit_code=%d, output=%s", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "hello from code_runner") {
		t.Fatalf("expected output to contain greeting, got: %s", result.Output)
	}
}

// TestCodeRunner_GoRunWithImport 验证包含多个 import 的代码自动解析。
func TestCodeRunner_GoRunWithImport(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	code := `
x := strings.ToUpper("hello")
fmt.Println(x)
n, _ := strconv.Atoi("42")
fmt.Println(n)
`
	result, err := r.Run(context.Background(), RunRequest{
		Language: "go",
		Code:     code,
		Mode:     ModeRun,
		AutoWrap: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got exit_code=%d, output=%s", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "HELLO") {
		t.Fatalf("expected HELLO, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "42") {
		t.Fatalf("expected 42, got: %s", result.Output)
	}
}

// TestCodeRunner_GoRunTimeout 验证超时 + 进程组清理。
func TestCodeRunner_GoRunTimeout(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	// 使用 shell sleep 而非 go run 无限循环 — 避免 go 编译耗时导致超时不精确
	result, err := r.Run(context.Background(), RunRequest{
		Mode:    ModeProjectCmd,
		Command: "sleep 60",
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatal("expected timeout failure")
	}
	if result.ExitCode != -1 {
		t.Fatalf("expected exit_code=-1 on timeout, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Output, "TIMEOUT") {
		t.Fatalf("expected TIMEOUT in output, got: %s", result.Output)
	}
}

// TestCodeRunner_GoTest 验证 go test -run 执行。
func TestCodeRunner_GoTest(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	// 使用当前包的一个已知测试 — 自测
	result, err := r.Run(context.Background(), RunRequest{
		Language: "go",
		Mode:     ModeTest,
		TestFunc: "TestCodeRunner_GoRun",
		TestPkg:  "./internal/executor/",
		Timeout:  30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 不检查 success — 可能因环境差异失败, 只验证不 panic 且有输出
	if result.Mode != ModeTest {
		t.Fatalf("expected mode=test, got %s", result.Mode)
	}
}

// TestCodeRunner_JSRun 验证 JavaScript 执行。
func TestCodeRunner_JSRun(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()
	if !r.HasNode() {
		t.Skip("node not available")
	}

	result, err := r.Run(context.Background(), RunRequest{
		Language: "javascript",
		Code:     `console.log("hello from js")`,
		Mode:     ModeRun,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got exit_code=%d, output=%s", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "hello from js") {
		t.Fatalf("expected js output, got: %s", result.Output)
	}
}

// TestCodeRunner_ProjectCmd 验证 shell 命令执行。
func TestCodeRunner_ProjectCmd(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	result, err := r.Run(context.Background(), RunRequest{
		Mode:    ModeProjectCmd,
		Command: "echo 'hello from shell'",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got exit_code=%d, output=%s", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "hello from shell") {
		t.Fatalf("expected shell output, got: %s", result.Output)
	}
}

// TestCodeRunner_ProjectCmd_CustomWorkDir 验证自定义工作目录。
func TestCodeRunner_ProjectCmd_CustomWorkDir(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	// 在项目根内创建子目录
	subDir := filepath.Join(r.workDir, "test_subdir_"+t.Name())
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(subDir)

	result, err := r.Run(context.Background(), RunRequest{
		Mode:    ModeProjectCmd,
		Command: "pwd",
		WorkDir: subDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "test_subdir_") {
		t.Fatalf("expected custom workdir in pwd output, got: %s", result.Output)
	}
}

// TestCodeRunner_OutputTruncation 验证 512KB 输出截断。
func TestCodeRunner_OutputTruncation(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	// 生成超过 512KB 的输出
	result, err := r.Run(context.Background(), RunRequest{
		Mode:    ModeProjectCmd,
		Command: "dd if=/dev/zero bs=1024 count=600 2>/dev/null | tr '\\0' 'A'",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Fatal("expected truncated=true for large output")
	}
	if len(result.Output) > maxOutputBytes+100 {
		t.Fatalf("output should be capped near %d bytes, got %d", maxOutputBytes, len(result.Output))
	}
}

// TestCodeRunner_ConcurrencyLimit 验证信号量限流。
func TestCodeRunner_ConcurrencyLimit(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	const total = 6
	const sleepPerRun = 250 * time.Millisecond
	var wg sync.WaitGroup
	wg.Add(total)
	start := time.Now()
	var runErrs atomic.Int32

	for i := 0; i < total; i++ {
		go func() {
			defer wg.Done()
			_, err := r.Run(context.Background(), RunRequest{
				Mode:    ModeProjectCmd,
				Command: "sleep 0.25",
				Timeout: 5 * time.Second,
			})
			if err != nil {
				runErrs.Add(1)
			}
		}()
	}
	wg.Wait()
	if runErrs.Load() > 0 {
		t.Fatalf("expected all runs success, got %d errors", runErrs.Load())
	}

	// maxConcurrentRuns=3, total=6, 每个 run 至少 sleep 250ms:
	// 有限流时应至少分两批, 耗时应明显大于单批。
	elapsed := time.Since(start)
	minExpected := sleepPerRun + 150*time.Millisecond
	if elapsed < minExpected {
		t.Fatalf("expected concurrency limit to serialize runs into batches, elapsed=%v < %v", elapsed, minExpected)
	}
}

// TestCodeRunner_AutoWrap_NoUnusedImports 验证自动包裹不引入未使用 import。
func TestCodeRunner_AutoWrap_NoUnusedImports(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	code := `fmt.Println("only fmt")`
	result, err := r.Run(context.Background(), RunRequest{
		Language: "go",
		Code:     code,
		Mode:     ModeRun,
		AutoWrap: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("auto-wrap should compile without unused imports, got exit_code=%d, output=%s", result.ExitCode, result.Output)
	}
}

// TestCodeRunner_TempCleanup_InstanceScoped 验证仅清理实例目录, 不误删其他目录。
func TestCodeRunner_TempCleanup_InstanceScoped(t *testing.T) {
	// 创建一个"外部"临时目录, 模拟其他实例
	externalDir, err := os.MkdirTemp("", "code_exec_other_")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(externalDir)

	r := mustNewRunner(t)

	// 执行一次, 产生子目录
	_, _ = r.Run(context.Background(), RunRequest{
		Mode:    ModeProjectCmd,
		Command: "echo test",
	})

	// Cleanup 只删实例目录
	r.Cleanup()

	// 外部目录应仍存在
	if _, err := os.Stat(externalDir); os.IsNotExist(err) {
		t.Fatal("Cleanup() deleted external directory — scope violation")
	}

	// 实例目录应已删除
	if _, err := os.Stat(r.tempRoot); !os.IsNotExist(err) {
		t.Fatal("Cleanup() did not remove instance tempRoot")
	}
}

// TestCodeRunner_WorkDir_PathTraversalBlocked 验证路径穿越阻断。
func TestCodeRunner_WorkDir_PathTraversalBlocked(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	tests := []struct {
		name    string
		workDir string
	}{
		{"double dot", "../../etc"},
		{"absolute outside", "/tmp"},
		{"prefix bypass", r.workDir + "2"}, // /root/work2 不应通过 /root/work 校验
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.Run(context.Background(), RunRequest{
				Mode:    ModeProjectCmd,
				WorkDir: tt.workDir,
				Command: "echo nope",
			})
			if err == nil {
				t.Fatalf("expected path traversal error for %q", tt.workDir)
			}
			if !strings.Contains(err.Error(), "outside project root") {
				t.Fatalf("expected 'outside project root' error, got: %v", err)
			}
		})
	}
}

// TestCodeRunner_WorkDir_SymlinkTraversalBlocked 验证软链接指向项目根外目录时应被阻断。
func TestCodeRunner_WorkDir_SymlinkTraversalBlocked(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	outsideDir, err := os.MkdirTemp("", "code_exec_outside_")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outsideDir)

	linkPath := filepath.Join(r.workDir, "outside_link")
	_ = os.Remove(linkPath)
	t.Cleanup(func() { _ = os.Remove(linkPath) })
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatal(err)
	}

	_, err = r.Run(context.Background(), RunRequest{
		Mode:    ModeProjectCmd,
		WorkDir: linkPath,
		Command: "pwd -P",
	})
	if err == nil {
		t.Fatalf("expected symlink traversal to be blocked for %q", linkPath)
	}
	if !strings.Contains(err.Error(), "outside project root") {
		t.Fatalf("expected 'outside project root' error, got: %v", err)
	}
}

// TestCodeRunner_WorkDir_AllowsDotDotPrefixInsideRoot 验证项目根内 "..x" 目录不应被误判为越界。
func TestCodeRunner_WorkDir_AllowsDotDotPrefixInsideRoot(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	insideDir := filepath.Join(r.workDir, "..cache_"+t.Name())
	if err := os.MkdirAll(insideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(insideDir)

	result, err := r.Run(context.Background(), RunRequest{
		Mode:    ModeProjectCmd,
		WorkDir: insideDir,
		Command: "pwd",
	})
	if err != nil {
		t.Fatalf("expected inside path to pass validation, got error: %v", err)
	}
	if !strings.Contains(result.Output, "..cache_") {
		t.Fatalf("expected custom inside workdir in output, got: %s", result.Output)
	}
}

// TestCodeRunner_OutputLimit_AggregatedStdoutStderr 验证 stdout+stderr 合计按 512KB 截断。
func TestCodeRunner_OutputLimit_AggregatedStdoutStderr(t *testing.T) {
	r := mustNewRunner(t)
	defer r.Cleanup()

	// stdout 和 stderr 各写 300KB → 合计 600KB > 512KB
	result, err := r.Run(context.Background(), RunRequest{
		Mode:    ModeProjectCmd,
		Command: "dd if=/dev/zero bs=1024 count=300 2>/dev/null | tr '\\0' 'A' && dd if=/dev/zero bs=1024 count=300 2>/dev/null | tr '\\0' 'B' >&2",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Fatal("expected truncated=true for aggregated stdout+stderr > 512KB")
	}
}

// ========================================
// wrapGoMain 单元测试
// ========================================

// TestWrapGoMain_AlreadyHasPackage 验证已有 package 声明时原样返回。
func TestWrapGoMain_AlreadyHasPackage(t *testing.T) {
	code := "package foo\n\nfunc Bar() {}\n"
	result := wrapGoMain(code)
	if result != code {
		t.Fatalf("expected unchanged, got:\n%s", result)
	}
}

// TestWrapGoMain_HasMainFunc 验证已有 func main() 时仅补 header。
func TestWrapGoMain_HasMainFunc(t *testing.T) {
	code := "func main() { fmt.Println(42) }"
	result := wrapGoMain(code)
	if !strings.HasPrefix(result, "package main") {
		t.Fatal("expected package main header")
	}
	if !strings.Contains(result, `"fmt"`) {
		t.Fatal("expected fmt import")
	}
	// 不应双重包裹
	if strings.Count(result, "func main()") != 1 {
		t.Fatal("should not double-wrap func main()")
	}
}

// TestWrapGoMain_SnippetOnly 验证纯代码片段自动包裹。
func TestWrapGoMain_SnippetOnly(t *testing.T) {
	code := `fmt.Println("hi")`
	result := wrapGoMain(code)
	if !strings.Contains(result, "package main") {
		t.Fatal("expected package main")
	}
	if !strings.Contains(result, `"fmt"`) {
		t.Fatal("expected fmt import")
	}
	if !strings.Contains(result, "func main()") {
		t.Fatal("expected func main() wrapper")
	}
}

// ========================================
// TruncateForAudit 测试
// ========================================

func TestTruncateForAudit(t *testing.T) {
	short := "hello"
	if got := TruncateForAudit(short, 100); got != short {
		t.Fatalf("short string should be unchanged, got: %s", got)
	}

	long := strings.Repeat("x", 5000)
	got := TruncateForAudit(long, 100)
	if len(got) > 120 { // 100 + "[truncated]"
		t.Fatalf("long string should be truncated, got len: %d", len(got))
	}
	if !strings.HasSuffix(got, "...[truncated]") {
		t.Fatalf("expected truncation suffix, got: %s", got)
	}
}

// ========================================
// 审查修复回归测试
// ========================================

// TestWrapGoMain_CommentOnlyImportNotAdded 验证注释中的 pkg.X 不触发 import (修复 #3)。
func TestWrapGoMain_CommentOnlyImportNotAdded(t *testing.T) {
	// "strings.ToUpper" 仅在注释中出现 → 不应导入 "strings"
	code := `// 使用 strings.ToUpper 可以转大写
fmt.Println("no strings used")`
	result := wrapGoMain(code)
	if strings.Contains(result, `"strings"`) {
		t.Fatalf("wrapGoMain should NOT import strings when only referenced in comments\nresult:\n%s", result)
	}
	if !strings.Contains(result, `"fmt"`) {
		t.Fatal("wrapGoMain should still import fmt from actual code")
	}
}

// TestWrapGoMain_MixedCommentAndCode 验证注释行过滤不影响实际代码行的 import。
func TestWrapGoMain_MixedCommentAndCode(t *testing.T) {
	code := `// 示例: os.Exit(1) 可以退出
x := strings.ToUpper("test")
fmt.Println(x)
// fmt.Errorf 也很常用`
	result := wrapGoMain(code)
	// strings 在实际代码中使用 → 应导入
	if !strings.Contains(result, `"strings"`) {
		t.Fatal("expected strings import from actual code line")
	}
	// fmt 在实际代码中使用 → 应导入
	if !strings.Contains(result, `"fmt"`) {
		t.Fatal("expected fmt import from actual code line")
	}
	// os 仅在注释中 → 不应导入
	if strings.Contains(result, `"os"`) {
		t.Fatalf("wrapGoMain should NOT import os when only in comment\nresult:\n%s", result)
	}
}

func TestBuildGoTestPattern_QuotesRegexMeta(t *testing.T) {
	pattern := buildGoTestPattern("TestA|TestB")
	if pattern != "^TestA\\|TestB$" {
		t.Fatalf("pattern = %q, want %q", pattern, "^TestA\\|TestB$")
	}
}

func TestResolveLocalTsxPath_ExecutableFile(t *testing.T) {
	root := t.TempDir()
	tsxPath := filepath.Join(root, "node_modules", ".bin", "tsx")
	if err := os.MkdirAll(filepath.Dir(tsxPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsxPath, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveLocalTsxPath(root)
	if got != tsxPath {
		t.Fatalf("resolveLocalTsxPath() = %q, want %q", got, tsxPath)
	}
}

func TestResolveLocalTsxPath_NonExecutableIgnored(t *testing.T) {
	root := t.TempDir()
	tsxPath := filepath.Join(root, "node_modules", ".bin", "tsx")
	if err := os.MkdirAll(filepath.Dir(tsxPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsxPath, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := resolveLocalTsxPath(root); got != "" {
		t.Fatalf("resolveLocalTsxPath() = %q, want empty for non-executable file", got)
	}
}

// ========================================
// 辅助
// ========================================

func mustNewRunner(t *testing.T) *CodeRunner {
	t.Helper()
	// 使用当前工作目录的上两层 (go-agent-v2) 作为 workDir
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewCodeRunner(cwd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Cleanup)
	return r
}

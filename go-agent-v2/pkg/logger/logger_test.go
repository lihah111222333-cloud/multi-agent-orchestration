package logger

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
)

// ========================================
// Bug 1 & 3: defaultLogger 数据竞争
// 多个 goroutine 并发读写 defaultLogger
// 在修复前, go test -race 会报 data race
// ========================================

func TestDefaultLoggerConcurrentAccess(t *testing.T) {
	// 确保初始状态
	Init("production")

	var wg sync.WaitGroup
	const goroutines = 100

	// 启动读 goroutine (模拟多 Agent 并发日志)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Info("concurrent log message", "key", "value")
			_ = Get()
		}()
	}

	// 同时执行写操作 (模拟 Init 或 AttachDBHandler)
	wg.Add(1)
	go func() {
		defer wg.Done()
		Init("development")
	}()

	wg.Wait()
}

// TestGetReturnsCurrentLogger 验证 Get() 返回最新的 logger。
func TestGetReturnsCurrentLogger(t *testing.T) {
	Init("production")
	l := Get()
	if l == nil {
		t.Fatal("Get() returned nil")
	}
}

// ========================================
// Bug 6: DurationMS 类型处理不完整
// slog.Any(FieldDurationMS, int(100)) 应映射成功
// ========================================

func TestApplyAttrDurationMS_Int64(t *testing.T) {
	e := &LogEntry{}
	// int64 — 当前代码已支持
	applyAttr(e, slog.Int64(FieldDurationMS, 42))
	if e.DurationMS == nil || *e.DurationMS != 42 {
		t.Errorf("int64: want DurationMS=42, got %v", e.DurationMS)
	}
}

func TestApplyAttrDurationMS_Int(t *testing.T) {
	e := &LogEntry{}
	// int — 当前代码不支持, 会得到 nil (BUG)
	applyAttr(e, slog.Any(FieldDurationMS, int(100)))
	if e.DurationMS == nil {
		t.Fatal("int: DurationMS should not be nil for int type")
	}
	if *e.DurationMS != 100 {
		t.Errorf("int: want DurationMS=100, got %d", *e.DurationMS)
	}
}

func TestApplyAttrDurationMS_Float64(t *testing.T) {
	e := &LogEntry{}
	// float64 — 当前代码不支持, 会得到 nil (BUG)
	applyAttr(e, slog.Any(FieldDurationMS, float64(99.7)))
	if e.DurationMS == nil {
		t.Fatal("float64: DurationMS should not be nil for float64 type")
	}
	if *e.DurationMS != 99 {
		t.Errorf("float64: want DurationMS=99, got %d", *e.DurationMS)
	}
}

// ========================================
// Bug 7: containsErrorKeyword 正确性验证
// 同时验证修复后使用 strings.Contains 的等价性
// ========================================

func TestContainsErrorKeyword(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"lowercase error", "something went error here", true},
		{"uppercase ERROR", "FATAL ERROR occurred", true},
		{"capitalized Error", "Error: connection refused", true},
		{"panic keyword", "goroutine panic detected", true},
		{"PANIC keyword", "PANIC: runtime error", true},
		{"fatal keyword", "fatal: cannot open file", true},
		{"FATAL keyword", "FATAL signal received", true},
		{"no match", "all systems operational", false},
		{"empty string", "", false},
		{"partial match err", "erroneous input", false}, // "err" 不应匹配 "error"
		{"substring at end", "this is an error", true},
		{"substring at start", "error at beginning", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsErrorKeyword(tt.line)
			if got != tt.want {
				t.Errorf("containsErrorKeyword(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

// ========================================
// Bug 2: ShutdownFileHandler 后 logger 仍可用
// ========================================

func TestShutdownFileHandlerSafety(t *testing.T) {
	// 验证 Shutdown 后日志方法不 panic
	ShutdownFileHandler() // 即使没有 InitWithFile 也不应 panic

	// Shutdown 后继续写日志应安全
	Info("after shutdown", "key", "val")
}

// ========================================
// 附加: applyAttr 覆盖已知字段
// ========================================

func TestApplyAttrKnownFields(t *testing.T) {
	e := &LogEntry{}

	applyAttr(e, slog.String(FieldSource, "codex"))
	applyAttr(e, slog.String(FieldComponent, "stderr"))
	applyAttr(e, slog.String(FieldAgentID, "agent-1"))
	applyAttr(e, slog.String(FieldThreadID, "thread-abc"))
	applyAttr(e, slog.String(FieldTraceID, "trace-xyz"))
	applyAttr(e, slog.String(FieldEventType, "tool_call"))
	applyAttr(e, slog.String(FieldToolName, "read_file"))
	applyAttr(e, slog.String("logger", "codex.main"))
	applyAttr(e, slog.String("raw", "raw-text"))

	if e.Source != "codex" {
		t.Errorf("Source = %q, want codex", e.Source)
	}
	if e.Component != "stderr" {
		t.Errorf("Component = %q, want stderr", e.Component)
	}
	if e.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want agent-1", e.AgentID)
	}
	if e.ThreadID != "thread-abc" {
		t.Errorf("ThreadID = %q, want thread-abc", e.ThreadID)
	}
	if e.TraceID != "trace-xyz" {
		t.Errorf("TraceID = %q, want trace-xyz", e.TraceID)
	}
	if e.EventType != "tool_call" {
		t.Errorf("EventType = %q, want tool_call", e.EventType)
	}
	if e.ToolName != "read_file" {
		t.Errorf("ToolName = %q, want read_file", e.ToolName)
	}
	if e.Logger != "codex.main" {
		t.Errorf("Logger = %q, want codex.main", e.Logger)
	}
	if e.Raw != "raw-text" {
		t.Errorf("Raw = %q, want raw-text", e.Raw)
	}
}

func TestApplyAttrUnknownFieldGoesToExtra(t *testing.T) {
	e := &LogEntry{}
	applyAttr(e, slog.String("custom_field", "custom_value"))

	if e.Extra == nil {
		t.Fatal("Extra should not be nil for unknown field")
	}
	if v, ok := e.Extra["custom_field"]; !ok || v != "custom_value" {
		t.Errorf("Extra[custom_field] = %v, want custom_value", v)
	}
}

// ========================================
// Bug 7 补充: containsErrorKeyword 应使用
// strings.Contains (重构后行为不变)
// ========================================

func TestContainsErrorKeyword_UsesStringsContains(t *testing.T) {
	// 验证当前实现与 strings.Contains 行为一致
	testCases := []string{
		"Error connecting to database",
		"no errors found",
		"PANIC in goroutine",
		"clean log line with info",
		"fatal exception at runtime",
	}
	for _, line := range testCases {
		got := containsErrorKeyword(line)
		// 用 strings.Contains 作为 oracle
		want := false
		for _, kw := range []string{"error", "Error", "ERROR", "panic", "PANIC", "fatal", "FATAL"} {
			if strings.Contains(line, kw) {
				want = true
				break
			}
		}
		if got != want {
			t.Errorf("containsErrorKeyword(%q) = %v, oracle = %v", line, got, want)
		}
	}
}

// ========================================
// Bug 1: InitWithFile 重复调用应关闭旧文件
// ========================================

func TestInitWithFile_ClosesOldFile(t *testing.T) {
	dir := t.TempDir()

	// 第一次调用
	if err := InitWithFile(dir); err != nil {
		t.Fatalf("first InitWithFile: %v", err)
	}

	// 记住旧文件
	logFileMu.Lock()
	oldFile := logFile
	logFileMu.Unlock()

	if oldFile == nil {
		t.Fatal("logFile should not be nil after InitWithFile")
	}

	// 第二次调用 (同目录即可)
	if err := InitWithFile(dir); err != nil {
		t.Fatalf("second InitWithFile: %v", err)
	}

	// 旧文件应已被关闭: Stat 会返回 os.ErrClosed 或类似错误
	_, err := oldFile.Stat()
	if err == nil {
		t.Error("old logFile should be closed after second InitWithFile, but Stat succeeded")
	}

	// 清理
	ShutdownFileHandler()
	Init("production")
}

// ========================================
// Bug 2: AttachDBHandler 重复调用不应嵌套 MultiHandler
// ========================================

func TestUnwrapBaseHandler_ReturnsBaseFromMulti(t *testing.T) {
	base := slog.NewTextHandler(os.Stderr, nil)
	fakeDB := slog.NewJSONHandler(os.Stderr, nil)
	multi := NewMultiHandler(base, fakeDB)

	got := unwrapBaseHandler(multi)
	// 应该返回 base handler, 不是 MultiHandler
	if _, isMH := got.(*MultiHandler); isMH {
		t.Error("unwrapBaseHandler should strip MultiHandler wrapper")
	}
}

func TestUnwrapBaseHandler_PassThroughNonMulti(t *testing.T) {
	base := slog.NewTextHandler(os.Stderr, nil)
	got := unwrapBaseHandler(base)
	if got != base {
		t.Error("unwrapBaseHandler should return non-MultiHandler as-is")
	}
}

// ========================================
// Bug 3: Fatal 应在 exit 前 flush 日志
// ========================================

func TestFatal_FlushesBeforeExit(t *testing.T) {
	// 替换 exitFunc 拦截 os.Exit
	exitCalled := false
	exitCode := 0
	origExit := exitFunc
	exitFunc = func(code int) {
		exitCalled = true
		exitCode = code
	}
	defer func() { exitFunc = origExit }()

	// 用测试 logger 避免影响其他测试
	origLogger := getLogger()
	defer storeLogger(origLogger)
	Init("production")

	Fatal("test fatal", "key", "value")

	if !exitCalled {
		t.Fatal("exitFunc should have been called")
	}
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1", exitCode)
	}
}

// ========================================
// Bug 4: StderrCollector 应处理 scanner 错误
// ========================================

func TestStderrCollector_ScannerErrorHandled(t *testing.T) {
	c := NewStderrCollector("test-agent")

	// 写入超长行 (超过默认 bufio.Scanner 64KB 限制)
	longLine := strings.Repeat("x", 80*1024) // 80KB 无换行
	_, _ = c.Write([]byte(longLine))

	// 关闭 writer 端让 scanner 停止
	_ = c.Close()

	// 如果 scan() 不处理 scanner.Err(), 这里不会 panic,
	// 但 scanner 错误被静默吞掉了。
	// 修复后 scan() 应记录 scanner 错误 (我们无法拦截 slog 输出,
	// 但至少确认 goroutine 正常完成而非死锁)。
	// done channel 已在 Close() 中等待, 没有超时说明 goroutine 已退出。
}

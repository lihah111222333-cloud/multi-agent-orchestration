// util_test.go — EscapeLike / ClampInt / SafeGo 表驱动测试。
// Python 对应: tests/test_utils.py (escape_like + normalize_limit)。
package util

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"percent", "100%", `100\%`},
		{"underscore", "a_b", `a\_b`},
		{"backslash", `a\b`, `a\\b`},
		{"combined", `%_\`, `\%\_\\`},
		{"no_special", "hello", "hello"},
		{"empty", "", ""},
		{"multiple_percent", "%%", `\%\%`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeLike(tt.in)
			if got != tt.want {
				t.Errorf("EscapeLike(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestClampInt(t *testing.T) {
	tests := []struct {
		name      string
		v, lo, hi int
		want      int
	}{
		{"below_min", -1, 0, 10, 0},
		{"above_max", 20, 0, 10, 10},
		{"in_range", 5, 0, 10, 5},
		{"at_min", 0, 0, 10, 0},
		{"at_max", 10, 0, 10, 10},
		{"negative_range", -5, -10, -1, -5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampInt(tt.v, tt.lo, tt.hi)
			if got != tt.want {
				t.Errorf("ClampInt(%d, %d, %d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
			}
		})
	}
}

func TestSafeGo_NormalExecution(t *testing.T) {
	var done atomic.Bool
	SafeGo(func() {
		done.Store(true)
	})
	time.Sleep(50 * time.Millisecond)
	if !done.Load() {
		t.Error("SafeGo: function was not executed")
	}
}

func TestSafeGo_PanicDoesNotPropagate(t *testing.T) {
	// SafeGo 应捕获 panic，不扩散到调用方
	var wg sync.WaitGroup
	wg.Add(1)

	SafeGo(func() {
		defer wg.Done()
		panic("test panic")
	})

	// 如果 panic 扩散，测试进程会崩溃 — 走到这里说明捕获成功
	wg.Wait()
	// 能到这里本身就证明 panic 没有扩散
}

func TestSafeGo_PanicWithError(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	SafeGo(func() {
		defer wg.Done()
		panic(42) // 非 string 类型的 panic
	})
	wg.Wait()
	// 非 string panic 也应被捕获
}

func TestSafeGo_MultipleConcurrent(t *testing.T) {
	const n = 100
	var counter atomic.Int32
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		SafeGo(func() {
			defer wg.Done()
			counter.Add(1)
		})
	}

	wg.Wait()
	if got := counter.Load(); got != n {
		t.Errorf("SafeGo concurrent: executed %d/%d", got, n)
	}
}

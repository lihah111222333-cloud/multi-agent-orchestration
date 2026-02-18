package util

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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

// safego.go — 安全 goroutine 启动器，捕获 panic 防止进程崩溃。
package util

import (
	"runtime/debug"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// SafeGo 在新 goroutine 中安全执行 fn，捕获 panic 并记录日志 + 堆栈。
func SafeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("goroutine panicked",
					logger.FieldError, r,
					"stack", string(debug.Stack()),
				)
			}
		}()
		fn()
	}()
}

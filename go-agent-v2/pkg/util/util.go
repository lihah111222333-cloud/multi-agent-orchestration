// Package util 提供通用工具函数。
//
// 核心工具:
//   - EscapeLike  ← escape_like (SQL LIKE 转义)
//   - ClampInt    ← normalize_limit (整数范围限制)
//   - ToMapAny    ← 任意值转 map
//   - SafeGo      ← 安全 goroutine
package util

import (
	"encoding/json"
	"runtime/debug"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ToMapAny 将任意值转为 map[string]any。
//
// 已经是 map[string]any 则直接返回 (零分配)。
// 否则通过 json marshal+unmarshal 转换，失败返回空 map。
func ToMapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	if raw, err := json.Marshal(v); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// EscapeLike 转义 SQL LIKE 模式中的特殊字符 (%, _, \)。
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// ClampInt 将值限制在 [lo, hi] 范围内。
func ClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

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

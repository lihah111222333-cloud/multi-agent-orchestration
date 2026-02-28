package util

import (
	"encoding/json"
	"runtime/debug"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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

func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func ClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func SafeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("goroutine panicked", logger.FieldError, r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}

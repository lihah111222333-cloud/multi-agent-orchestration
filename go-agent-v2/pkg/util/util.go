// Package util 提供通用工具函数。
//
// 1:1 对应 Python utils.py:
//   - EscapeLike  ← escape_like
//   - ClampInt    ← normalize_limit
//   - EnvInt      ← as_int_env
//   - EnvFloat    ← as_float_env
//   - EnvBool     ← _bool_env
package util

import (
	"encoding/json"
	"os"
	"reflect"
	"runtime/debug"
	"strconv"
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
// 对应 Python escape_like。
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// ClampInt 将值限制在 [lo, hi] 范围内。
// 对应 Python normalize_limit。
func ClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// EnvInt 读取整型环境变量，无效时返回 def，并确保不小于 min。
// 对应 Python as_int_env。
func EnvInt(name string, def, min int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	return v
}

// EnvFloat 读取浮点型环境变量，无效时返回 def，并确保不小于 min。
// 对应 Python as_float_env。
func EnvFloat(name string, def, min float64) float64 {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	return v
}

// EnvBool 读取布尔环境变量，无效时返回 def。
// 接受: 1/true/yes/on → true, 0/false/no/off → false。
// 对应 Python _bool_env。
func EnvBool(name string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

// EnvStr 读取字符串环境变量，为空时返回 def。
func EnvStr(name, def string) string {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	return v
}

// LoadFromEnv 通过反射从 struct tag 加载环境变量。
//
// 支持的 tag:
//   - env:"VAR_NAME"   — 环境变量名
//   - default:"value"  — 默认值
//   - min:"N"          — 最小值 (int/float64)
//
// 支持的字段类型: string, int, float64, bool。
func LoadFromEnv(ptr any) {
	if ptr == nil {
		logger.Error("util.LoadFromEnv: ptr must not be nil")
		return
	}
	rv := reflect.ValueOf(ptr)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		logger.Error("util.LoadFromEnv: ptr must be a non-nil pointer to struct")
		return
	}
	v := rv.Elem()
	t := v.Type()

	for i := range t.NumField() {
		field := t.Field(i)
		envName := field.Tag.Get("env")
		if envName == "" {
			continue
		}

		def := field.Tag.Get("default")
		minStr := field.Tag.Get("min")
		fv := v.Field(i)

		switch field.Type.Kind() {
		case reflect.String:
			fv.SetString(EnvStr(envName, def))

		case reflect.Int:
			defInt, _ := strconv.Atoi(def)
			minInt, _ := strconv.Atoi(minStr)
			fv.SetInt(int64(EnvInt(envName, defInt, minInt)))

		case reflect.Float64:
			defFloat, _ := strconv.ParseFloat(def, 64)
			minFloat, _ := strconv.ParseFloat(minStr, 64)
			fv.SetFloat(EnvFloat(envName, defFloat, minFloat))

		case reflect.Bool:
			defBool := def == "true" || def == "1" || def == "yes"
			fv.SetBool(EnvBool(envName, defBool))
		}
	}
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

// Package logger 提供基于 slog 的结构化日志。
//
// 核心功能:
//   - Init() 配置默认日志器 (JSON/Text)
//   - FromContext() 上下文感知日志
//   - 包级便捷方法 (Info/Error/Warn/Debug/Fatal)
package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

var defaultLogger = newLogger(false)

func newLogger(development bool) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: development,
	}
	var handler slog.Handler
	if development {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// Init 初始化日志配置。env: "development"/"dev" 或 "production" (默认)。
func Init(env string) {
	dev := env == "development" || env == "dev"
	defaultLogger = newLogger(dev)
	slog.SetDefault(defaultLogger)
}

// ========================================
// Context 感知日志
// ========================================

type ctxKey struct{}

// WithContext 将日志器注入 context。
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext 从 context 提取日志器，若不存在则返回默认日志器。
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return defaultLogger
}

// ========================================
// 包级便捷方法
// ========================================

// Info/Error/Warn/Debug 记录结构化日志。args 为 key-value 对。
func Info(msg string, args ...any)  { defaultLogger.Info(msg, args...) }
func Error(msg string, args ...any) { defaultLogger.Error(msg, args...) }
func Warn(msg string, args ...any)  { defaultLogger.Warn(msg, args...) }
func Debug(msg string, args ...any) { defaultLogger.Debug(msg, args...) }

// Infof/Errorf/Warnf/Debugf 记录格式化日志。
func Infof(format string, args ...any)  { defaultLogger.Info(fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { defaultLogger.Error(fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { defaultLogger.Warn(fmt.Sprintf(format, args...)) }
func Debugf(format string, args ...any) { defaultLogger.Debug(fmt.Sprintf(format, args...)) }

// Fatal 记录致命错误并退出。
func Fatal(msg string, args ...any) {
	defaultLogger.Error(msg, args...)
	os.Exit(1)
}

// Infow/Warnw/Errorw/Debugw 等同于 Info/Warn/Error/Debug (兼容别名)。
func Infow(msg string, keysAndValues ...any)  { defaultLogger.Info(msg, keysAndValues...) }
func Warnw(msg string, keysAndValues ...any)  { defaultLogger.Warn(msg, keysAndValues...) }
func Errorw(msg string, keysAndValues ...any) { defaultLogger.Error(msg, keysAndValues...) }
func Debugw(msg string, keysAndValues ...any) { defaultLogger.Debug(msg, keysAndValues...) }

// With 返回带附加上下文的日志器。
func With(args ...any) *slog.Logger { return defaultLogger.With(args...) }

// Get 返回底层 slog.Logger。
func Get() *slog.Logger { return defaultLogger }

// Attr 类型别名 (避免调用方直接 import slog)。
type Attr = slog.Attr

// Any 创建任意类型属性。
func Any(key string, value any) Attr { return slog.Any(key, value) }

// 预留字段常量 — MUST 使用常量键名，勿硬编码。
const (
	FieldTraceID   = "trace_id"
	FieldAgentID   = "agent_id"
	FieldGatewayID = "gateway_id"
	FieldThreadID  = "thread_id"
	FieldAction    = "action"
	FieldComponent = "component"
	FieldModule    = "module"
	FieldError     = "error"
	FieldStatus    = "status"
	FieldLatencyMS = "latency_ms"
	FieldCount     = "count"
	FieldPath      = "path"
	FieldMethod    = "method"
	FieldUserID    = "user_id"
	// v2: 统一日志接入
	FieldSource     = "source"
	FieldEventType  = "event_type"
	FieldToolName   = "tool_name"
	FieldDurationMS = "duration_ms"
)

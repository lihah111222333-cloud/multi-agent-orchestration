// Package logger 提供基于 slog 的结构化日志。
//
// 核心功能:
//   - Init() 配置默认日志器 (JSON/Text)
//   - InitWithFile() 同时输出到 stdout 和日志文件
//   - FromContext() 上下文感知日志
//   - 包级便捷方法 (Info/Error/Warn/Debug/Fatal)
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

var (
	defaultLogger = newLogger(false)
	logFile       *os.File // 全局日志文件, Shutdown 时关闭
)

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

// InitWithFile 初始化日志, 同时输出到 stdout 和日志文件。
//
// 日志文件: {logDir}/agent-terminal-{date}.log (JSON 格式)。
// 调用者应在退出前调用 ShutdownFileHandler() 关闭文件。
func InitWithFile(logDir string) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	date := time.Now().Format("2006-01-02")
	logPath := filepath.Join(logDir, fmt.Sprintf("agent-terminal-%s.log", date))

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	logFile = f

	// MultiWriter: stdout + file
	multi := io.MultiWriter(os.Stdout, f)
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	handler := slog.NewJSONHandler(multi, opts)
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)

	slog.Info("log file opened", "path", logPath)
	return nil
}

// ShutdownFileHandler 关闭日志文件。
func ShutdownFileHandler() {
	if logFile != nil {
		_ = logFile.Sync()
		_ = logFile.Close()
		logFile = nil
	}
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

// String 创建字符串属性。
func String(key, value string) Attr { return slog.String(key, value) }

// Int64 创建 int64 属性。
func Int64(key string, value int64) Attr { return slog.Int64(key, value) }

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
	// v3: 补充常用字段
	FieldAddr      = "addr"
	FieldConn      = "conn"
	FieldRemote    = "remote"
	FieldKey       = "key"
	FieldSkill     = "skill"
	FieldOrigin    = "origin"
	FieldMax       = "max"
	FieldDataLen   = "data_len"
	FieldParamsLen = "params_len"
	FieldID        = "id"
	FieldName      = "name"
	FieldCwd       = "cwd"
	FieldRunKey    = "run_key"
	FieldRoot      = "root"
	FieldBytes     = "bytes"
	FieldLen       = "len"
	FieldListen    = "listen"
	FieldPort      = "port"
	FieldVersion   = "version"
	// v4: 补充剩余文件所需常量
	FieldTopic   = "topic"
	FieldSeq     = "seq"
	FieldDAG     = "dag"
	FieldNode    = "node"
	FieldURL     = "url"
	FieldVarsSet = "vars_set"
	FieldReqID   = "req_id"
	FieldCallID  = "call_id"
	FieldRaw     = "raw"
	// v5: 高风险点日志所需常量
	FieldCommand    = "command"
	FieldRunID      = "run_id"
	FieldExitCode   = "exit_code"
	FieldCardKey    = "card_key"
	FieldLanguage   = "language"
	FieldSubscriber = "subscriber"
	FieldFilter     = "filter"
	FieldDecision   = "decision"
	FieldPID        = "pid"
	FieldState      = "state"
)

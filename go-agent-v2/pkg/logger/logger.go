package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

var (
	defaultLogger     atomic.Pointer[slog.Logger]
	logFile           *os.File
	logFilePath       string
	logFileMu         sync.Mutex
	fileWatcherStop   chan struct{}
	utc8              = time.FixedZone("UTC+8", 8*60*60)
	exitFunc          = os.Exit
	fileWatchInterval = 30 * time.Second
)

func init() { defaultLogger.Store(newLogger(false)) }

func getLogger() *slog.Logger { return defaultLogger.Load() }

func storeLogger(l *slog.Logger) {
	defaultLogger.Store(l)
	slog.SetDefault(l)
}

func closeLogFileLocked() {
	if logFile != nil {
		_ = logFile.Sync()
		_ = logFile.Close()
	}
}

func replaceTimeAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		if t, ok := a.Value.Any().(time.Time); ok {
			a.Value = slog.StringValue(t.In(utc8).Format("2006-01-02 15:04:05"))
		}
	}
	return a
}

func newLogger(development bool) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		AddSource:   development,
		ReplaceAttr: replaceTimeAttr,
	}
	if development {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

func Init(env string) {
	dev := env == "development" || env == "dev"
	storeLogger(newLogger(dev))
}

func InitWithFile(logDir string) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return pkgerr.Wrap(err, "Logger.Init", "create log dir")
	}

	date := time.Now().Format("2006-01-02")
	logPath := filepath.Join(logDir, fmt.Sprintf("agent-terminal-%s.log", date))

	absPath, err := filepath.Abs(logPath)
	if err != nil {
		absPath = logPath
	}

	f, err := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return pkgerr.Wrap(err, "Logger.Init", "open log file")
	}
	logFileMu.Lock()
	stopFileWatcherLocked()
	closeLogFileLocked()
	logFile = f
	logFilePath = absPath
	stopCh := make(chan struct{})
	fileWatcherStop = stopCh
	logFileMu.Unlock()

	rebuildLoggerWithFile(f)

	go watchLogFile(absPath, stopCh)

	slog.Info("log file opened", "path", absPath)
	return nil
}

// rebuildLoggerWithFile replaces the global logger to write to both stdout and the given file.
func rebuildLoggerWithFile(f *os.File) {
	multi := io.MultiWriter(os.Stdout, f)
	opts := &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: replaceTimeAttr}
	handler := slog.NewJSONHandler(multi, opts)
	storeLogger(slog.New(handler))
}

// watchLogFile periodically checks if the log file was deleted and reopens it.
func watchLogFile(path string, stop chan struct{}) {
	ticker := time.NewTicker(fileWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				continue // file exists, all good
			}
			// File was deleted — reopen it.
			dir := filepath.Dir(path)
			_ = os.MkdirAll(dir, 0o755)
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				// Can't reopen — log to stdout only, will retry next tick.
				slog.Warn("log file watchdog: reopen failed", "path", path, "error", err)
				continue
			}
			logFileMu.Lock()
			closeLogFileLocked()
			logFile = f
			logFileMu.Unlock()
			rebuildLoggerWithFile(f)
			slog.Info("log file watchdog: reopened deleted log file", "path", path)
		}
	}
}

func stopFileWatcherLocked() {
	if fileWatcherStop != nil {
		close(fileWatcherStop)
		fileWatcherStop = nil
	}
}

func ShutdownFileHandler() {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	stopFileWatcherLocked()
	if logFile != nil {
		closeLogFileLocked()
		logFile = nil
	}
}

type ctxKey struct{}

func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return getLogger()
}

func Info(msg string, args ...any)  { getLogger().Info(msg, args...) }
func Error(msg string, args ...any) { getLogger().Error(msg, args...) }
func Warn(msg string, args ...any)  { getLogger().Warn(msg, args...) }
func Debug(msg string, args ...any) { getLogger().Debug(msg, args...) }

func Infof(format string, args ...any)  { getLogger().Info(fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { getLogger().Error(fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { getLogger().Warn(fmt.Sprintf(format, args...)) }
func Debugf(format string, args ...any) { getLogger().Debug(fmt.Sprintf(format, args...)) }

func Fatal(msg string, args ...any) {
	getLogger().Error(msg, args...)
	ShutdownDBHandler()
	ShutdownFileHandler()
	exitFunc(1)
}

func Infow(msg string, keysAndValues ...any)  { getLogger().Info(msg, keysAndValues...) }
func Warnw(msg string, keysAndValues ...any)  { getLogger().Warn(msg, keysAndValues...) }
func Errorw(msg string, keysAndValues ...any) { getLogger().Error(msg, keysAndValues...) }
func Debugw(msg string, keysAndValues ...any) { getLogger().Debug(msg, keysAndValues...) }

func With(args ...any) *slog.Logger { return getLogger().With(args...) }

func Get() *slog.Logger { return getLogger() }

func SetForTest(l *slog.Logger) { defaultLogger.Store(l) }

type Attr = slog.Attr

func Any(key string, value any) Attr { return slog.Any(key, value) }

func String(key, value string) Attr { return slog.String(key, value) }

func Int64(key string, value int64) Attr { return slog.Int64(key, value) }

const (
	FieldTraceID    = "trace_id"
	FieldAgentID    = "agent_id"
	FieldGatewayID  = "gateway_id"
	FieldThreadID   = "thread_id"
	FieldAction     = "action"
	FieldComponent  = "component"
	FieldModule     = "module"
	FieldError      = "error"
	FieldStatus     = "status"
	FieldLatencyMS  = "latency_ms"
	FieldCount      = "count"
	FieldPath       = "path"
	FieldMethod     = "method"
	FieldUserID     = "user_id"
	FieldSource     = "source"
	FieldEventType  = "event_type"
	FieldToolName   = "tool_name"
	FieldDurationMS = "duration_ms"
	FieldAddr       = "addr"
	FieldConn       = "conn"
	FieldRemote     = "remote"
	FieldKey        = "key"
	FieldSkill      = "skill"
	FieldOrigin     = "origin"
	FieldMax        = "max"
	FieldDataLen    = "data_len"
	FieldParamsLen  = "params_len"
	FieldID         = "id"
	FieldName       = "name"
	FieldCwd        = "cwd"
	FieldRunKey     = "run_key"
	FieldRoot       = "root"
	FieldBytes      = "bytes"
	FieldLen        = "len"
	FieldListen     = "listen"
	FieldPort       = "port"
	FieldVersion    = "version"
	FieldTopic      = "topic"
	FieldSeq        = "seq"
	FieldDAG        = "dag"
	FieldNode       = "node"
	FieldURL        = "url"
	FieldVarsSet    = "vars_set"
	FieldReqID      = "req_id"
	FieldCallID     = "call_id"
	FieldRaw        = "raw"
	FieldTurnID     = "turn_id"
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

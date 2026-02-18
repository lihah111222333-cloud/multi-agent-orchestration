package logger

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LogEntry 对应 system_logs 表的一行。
type LogEntry struct {
	Ts         time.Time
	Level      string
	Logger     string
	Message    string
	Raw        string
	Source     string
	Component  string
	AgentID    string
	ThreadID   string
	TraceID    string
	EventType  string
	ToolName   string
	DurationMS *int
	Extra      map[string]any
}

// ========================================
// DBHandler — slog.Handler → PG 异步批量写入
// ========================================

const (
	bufSize    = 1024
	batchSize  = 100
	flushDelay = 500 * time.Millisecond
)

// DBHandler 实现 slog.Handler，将日志异步批量写入 PostgreSQL system_logs 表。
type DBHandler struct {
	pool  *pgxpool.Pool
	buf   chan LogEntry
	attrs []slog.Attr
	group string
	level slog.Level
	done  chan struct{}
	// closed 在 handler clone(WithAttrs/WithGroup) 间共享，避免 shutdown 后继续写入已关闭通道 panic。
	closed *atomic.Bool
}

// NewDBHandler 创建并启动后台写入 goroutine。
func NewDBHandler(pool *pgxpool.Pool, level slog.Level) *DBHandler {
	h := &DBHandler{
		pool:   pool,
		buf:    make(chan LogEntry, bufSize),
		level:  level,
		done:   make(chan struct{}),
		closed: &atomic.Bool{},
	}
	go h.consumeLoop()
	return h
}

// Enabled 实现 slog.Handler。
func (h *DBHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle 实现 slog.Handler — 构造 LogEntry 推入异步缓冲。
func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
	if h.closed != nil && h.closed.Load() {
		return nil
	}

	entry := LogEntry{
		Ts:      r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
	}

	// 收集 With() 的固定 attrs
	for _, a := range h.attrs {
		applyAttr(&entry, a)
	}

	// 收集 Record 上的 attrs
	r.Attrs(func(a slog.Attr) bool {
		applyAttr(&entry, a)
		return true
	})

	// 非阻塞推入 — chan 满时 drop
	func() {
		defer func() {
			if recover() != nil {
				// shutdown 期间通道被关闭: 丢弃该条日志，避免 panic 影响主流程。
			}
		}()
		select {
		case h.buf <- entry:
		default:
			// drop: 避免 DB 慢时阻塞主流程
		}
	}()
	return nil
}

// WithAttrs 实现 slog.Handler。
func (h *DBHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &DBHandler{
		pool:   h.pool,
		buf:    h.buf,
		attrs:  newAttrs,
		group:  h.group,
		level:  h.level,
		done:   h.done,
		closed: h.closed,
	}
}

// WithGroup 实现 slog.Handler。
func (h *DBHandler) WithGroup(name string) slog.Handler {
	return &DBHandler{
		pool:   h.pool,
		buf:    h.buf,
		attrs:  h.attrs,
		group:  name,
		level:  h.level,
		done:   h.done,
		closed: h.closed,
	}
}

// Shutdown 停止后台 goroutine 并 flush 剩余日志。
func (h *DBHandler) Shutdown() {
	if h.closed != nil && !h.closed.CompareAndSwap(false, true) {
		return
	}
	close(h.buf)
	<-h.done
}

// consumeLoop 后台批量消费 chan → INSERT。
func (h *DBHandler) consumeLoop() {
	defer close(h.done)

	batch := make([]LogEntry, 0, batchSize)
	ticker := time.NewTicker(flushDelay)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-h.buf:
			if !ok {
				// chan 关闭: flush 剩余
				if len(batch) > 0 {
					h.flush(batch)
				}
				return
			}
			batch = append(batch, entry)
			if len(batch) >= batchSize {
				h.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				h.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

// flush 批量写入 PG。
func (h *DBHandler) flush(batch []LogEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, e := range batch {
		var extraJSON []byte
		if len(e.Extra) > 0 {
			var marshalErr error
			extraJSON, marshalErr = json.Marshal(e.Extra)
			if marshalErr != nil {
				slog.Default().Debug("db_handler: marshal extra", "error", marshalErr)
				extraJSON = nil
			}
		}

		_, err := h.pool.Exec(ctx,
			`INSERT INTO system_logs
				(ts, level, logger, message, raw,
				 source, component, agent_id, thread_id, trace_id,
				 event_type, tool_name, duration_ms, extra)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			e.Ts, e.Level, e.Logger, e.Message, e.Raw,
			e.Source, e.Component, e.AgentID, e.ThreadID, e.TraceID,
			e.EventType, e.ToolName, e.DurationMS, extraJSON,
		)
		if err != nil {
			// 写入失败仅 stderr 输出，不影响主流程
			slog.Default().Warn("db_handler: flush failed", "error", err)
		}
	}
}

// applyAttr 将 slog.Attr 映射到 LogEntry 的结构化字段。
func applyAttr(e *LogEntry, a slog.Attr) {
	switch a.Key {
	case FieldSource:
		e.Source = a.Value.String()
	case FieldComponent:
		e.Component = a.Value.String()
	case FieldAgentID:
		e.AgentID = a.Value.String()
	case FieldThreadID:
		e.ThreadID = a.Value.String()
	case FieldTraceID:
		e.TraceID = a.Value.String()
	case FieldEventType:
		e.EventType = a.Value.String()
	case FieldToolName:
		e.ToolName = a.Value.String()
	case FieldDurationMS:
		if v, ok := a.Value.Any().(int64); ok {
			ms := int(v)
			e.DurationMS = &ms
		}
	case "logger":
		e.Logger = a.Value.String()
	case "raw":
		e.Raw = a.Value.String()
	default:
		if e.Extra == nil {
			e.Extra = make(map[string]any)
		}
		e.Extra[a.Key] = a.Value.Any()
	}
}

// ========================================
// MultiHandler — 同时写多个 Handler (TextHandler + DBHandler)
// ========================================

// MultiHandler 扇出日志到多个 slog.Handler。
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler 创建多路 Handler。
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{handlers: handlers}
}

// Enabled 只要有一个 Handler 接受该级别就返回 true。
func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle 分发到所有 Handler。
func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r)
		}
	}
	return nil
}

// WithAttrs 对所有 Handler 调用 WithAttrs。
func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: handlers}
}

// WithGroup 对所有 Handler 调用 WithGroup。
func (m *MultiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: handlers}
}

// ========================================
// AttachDBHandler — pool ready 后动态挂载
// ========================================

var (
	dbHandler atomic.Pointer[DBHandler]
	attachMu  sync.Mutex
)

// AttachDBHandler 在 pool 初始化后调用，将 DBHandler 作为第二路 Handler 挂载。
// 调用前的日志只写 stdout; 调用后开始双写。
func AttachDBHandler(pool *pgxpool.Pool) {
	attachMu.Lock()
	defer attachMu.Unlock()

	h := NewDBHandler(pool, slog.LevelInfo)
	dbHandler.Store(h)

	// 重建 defaultLogger: 原始 text/json handler + dbHandler
	origHandler := defaultLogger.Handler()
	multi := NewMultiHandler(origHandler, h)
	defaultLogger = slog.New(multi)
	slog.SetDefault(defaultLogger)
}

// ShutdownDBHandler 关闭 DBHandler 并 flush 剩余日志。
func ShutdownDBHandler() {
	if h := dbHandler.Load(); h != nil {
		h.Shutdown()
	}
}

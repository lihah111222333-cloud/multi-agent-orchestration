package logger

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	slogmulti "github.com/samber/slog-multi"
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

const (
	bufSize    = 1024
	batchSize  = 100
	flushDelay = 500 * time.Millisecond
)

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

func (h *DBHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
	if h.closed != nil && h.closed.Load() {
		return nil
	}

	entry := LogEntry{
		Ts:      r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
	}

	for _, a := range h.attrs {
		applyAttr(&entry, a)
	}

	r.Attrs(func(a slog.Attr) bool {
		applyAttr(&entry, a)
		return true
	})

	func() {
		defer func() {
			recover() //nolint:errcheck // 恢复值无需处理
		}()
		select {
		case h.buf <- entry:
		default:
			// drop: 避免 DB 慢时阻塞主流程
		}
	}()
	return nil
}

func (h *DBHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}

func (h *DBHandler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.group = name
	return &clone
}

func (h *DBHandler) Shutdown() {
	if h.closed != nil && !h.closed.CompareAndSwap(false, true) {
		return
	}
	close(h.buf)
	<-h.done
}

func (h *DBHandler) consumeLoop() {
	defer close(h.done)

	batch := make([]LogEntry, 0, batchSize)
	flushBatch := func() {
		h.flush(batch)
		batch = batch[:0]
	}
	ticker := time.NewTicker(flushDelay)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-h.buf:
			if !ok {
				if len(batch) > 0 {
					flushBatch()
				}
				return
			}
			batch = append(batch, entry)
			if len(batch) >= batchSize {
				flushBatch()
			}
		case <-ticker.C:
			if len(batch) > 0 {
				flushBatch()
			}
		}
	}
}

func (h *DBHandler) flush(batch []LogEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const insertSQL = `INSERT INTO system_logs
		(ts, level, logger, message, raw,
		 source, component, agent_id, thread_id, trace_id,
		 event_type, tool_name, duration_ms, extra)
	 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`

	pgBatch := &pgx.Batch{}
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
		pgBatch.Queue(insertSQL,
			e.Ts, e.Level, e.Logger, e.Message, e.Raw,
			e.Source, e.Component, e.AgentID, e.ThreadID, e.TraceID,
			e.EventType, e.ToolName, e.DurationMS, extraJSON,
		)
	}

	br := h.pool.SendBatch(ctx, pgBatch)
	defer func() {
		if err := br.Close(); err != nil {
			slog.Default().Debug("db_handler: close batch", "error", err)
		}
	}()

	for range batch {
		if _, err := br.Exec(); err != nil {
			slog.Default().Warn("db_handler: flush row failed", "error", err)
		}
	}
}

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
		switch v := a.Value.Any().(type) {
		case int64:
			ms := int(v)
			e.DurationMS = &ms
		case int:
			e.DurationMS = &v
		case float64:
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

var (
	dbHandler   atomic.Pointer[DBHandler]
	attachMu    sync.Mutex
	baseHandler slog.Handler
)

func AttachDBHandler(pool *pgxpool.Pool) {
	attachMu.Lock()
	defer attachMu.Unlock()

	if old := dbHandler.Load(); old != nil {
		old.Shutdown()
	}

	h := NewDBHandler(pool, slog.LevelInfo)
	dbHandler.Store(h)

	if baseHandler == nil {
		baseHandler = getLogger().Handler()
	}

	multi := slogmulti.Fanout(baseHandler, h)
	storeLogger(slog.New(multi))
}

func ShutdownDBHandler() {
	if h := dbHandler.Swap(nil); h != nil {
		h.Shutdown()
	}
}

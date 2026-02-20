// system_log.go — 系统日志 CRUD (对应 Python system_log.py)。
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SystemLogStore 系统日志存储。
type SystemLogStore struct{ BaseStore }

// NewSystemLogStore 创建系统日志存储。
func NewSystemLogStore(pool *pgxpool.Pool) *SystemLogStore {
	return &SystemLogStore{NewBaseStore(pool)}
}

const sysLogCols = `id, ts, level, logger, message, raw,
	source, component, agent_id, thread_id, trace_id,
	event_type, tool_name, duration_ms, extra`

// Append 追加系统日志 (v1 兼容: 只写基础 6 列)。
func (s *SystemLogStore) Append(ctx context.Context, level, loggerName, message, raw string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO system_logs (ts, level, logger, message, raw) VALUES (NOW(), $1, $2, $3, $4)`,
		level, loggerName, message, raw)
	return err
}

// ListParams 统一日志查询参数。
type ListParams struct {
	Level     string
	Logger    string
	Source    string
	Component string
	AgentID   string
	ThreadID  string
	EventType string
	ToolName  string
	Keyword   string
	Limit     int
}

// List 查询系统日志 (v1 兼容: level + logger + keyword)。
func (s *SystemLogStore) List(ctx context.Context, level, loggerName, keyword string, limit int) ([]SystemLog, error) {
	return s.ListV2(ctx, ListParams{
		Level:   level,
		Logger:  loggerName,
		Keyword: keyword,
		Limit:   limit,
	})
}

// ListV2 查询系统日志 (v2: 支持全部字段过滤)。
func (s *SystemLogStore) ListV2(ctx context.Context, p ListParams) ([]SystemLog, error) {
	q := NewQueryBuilder().
		Eq("level", p.Level).
		Eq("logger", p.Logger).
		Eq("source", p.Source).
		Eq("component", p.Component).
		Eq("agent_id", p.AgentID).
		Eq("thread_id", p.ThreadID).
		Eq("event_type", p.EventType).
		Eq("tool_name", p.ToolName).
		KeywordLike(p.Keyword, "level", "logger", "message", "raw", "source", "component")
	sql, params := q.Build("SELECT "+sysLogCols+" FROM system_logs", "ts DESC, id DESC", p.Limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[SystemLog](rows)
}

// ListFilterValues 返回去重筛选值。
func (s *SystemLogStore) ListFilterValues(ctx context.Context) (map[string][]string, error) {
	return DistinctMap(ctx, s.pool, "system_logs", "level", "logger", "source", "component", "event_type", "tool_name")
}

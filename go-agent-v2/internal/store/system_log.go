package store

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SystemLogStore struct{ BaseStore }

func NewSystemLogStore(pool *pgxpool.Pool) *SystemLogStore {
	return &SystemLogStore{NewBaseStore(pool)}
}

const sysLogCols = `id, ts, level, logger, message, raw,
	source, component, agent_id, thread_id, trace_id,
	event_type, tool_name, duration_ms, extra`

func (s *SystemLogStore) Append(ctx context.Context, level, loggerName, message, raw string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO system_logs (ts, level, logger, message, raw) VALUES (NOW(), $1, $2, $3, $4)`,
		level, loggerName, message, raw)
	return err
}

type ListParams struct {
	Level, Logger, Source, Component                string
	AgentID, ThreadID, EventType, ToolName, Keyword string
	Limit                                           int
}

func (s *SystemLogStore) List(ctx context.Context, level, loggerName, keyword string, limit int) ([]SystemLog, error) {
	return s.ListV2(ctx, ListParams{
		Level: level, Logger: loggerName, Keyword: keyword, Limit: limit,
	})
}

func (s *SystemLogStore) ListV2(ctx context.Context, p ListParams) ([]SystemLog, error) {
	sql, params := NewQueryBuilder().
		Eq("level", p.Level).
		Eq("logger", p.Logger).
		Eq("source", p.Source).
		Eq("component", p.Component).
		Eq("agent_id", p.AgentID).
		Eq("thread_id", p.ThreadID).
		Eq("event_type", p.EventType).
		Eq("tool_name", p.ToolName).
		KeywordLike(p.Keyword, "level", "logger", "message", "raw", "source", "component").
		Build("SELECT "+sysLogCols+" FROM system_logs", "ts DESC, id DESC", p.Limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[SystemLog](rows)
}

func (s *SystemLogStore) ListFilterValues(ctx context.Context) (map[string][]string, error) {
	return DistinctMap(ctx, s.pool, "system_logs", "level", "logger", "source", "component", "event_type", "tool_name")
}

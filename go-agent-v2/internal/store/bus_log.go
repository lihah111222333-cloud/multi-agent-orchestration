// bus_log.go — 消息总线异常日志 CRUD (对应 Python bus_log.py)。
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BusLogStore 总线异常日志存储。
type BusLogStore struct{ BaseStore }

// NewBusLogStore 创建总线异常日志存储。
func NewBusLogStore(pool *pgxpool.Pool) *BusLogStore { return &BusLogStore{NewBaseStore(pool)} }

// List 查询异常日志。
func (s *BusLogStore) List(ctx context.Context, category, severity, keyword string, limit int) ([]BusException, error) {
	q := NewQueryBuilder().
		Eq("category", category).
		Eq("severity", severity).
		KeywordLike(keyword, "source", "tool_name", "message", "traceback")
	sql, params := q.Build(
		"SELECT ts, category, severity, source, tool_name, message, traceback, extra FROM bus_exception_logs",
		"ts DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[BusException](rows)
}

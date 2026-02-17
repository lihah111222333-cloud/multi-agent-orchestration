// bus_log.go — 消息总线异常日志 CRUD (对应 Python bus_log.py)。
package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// BusLogStore 总线异常日志存储。
type BusLogStore struct{ BaseStore }

// NewBusLogStore 创建总线异常日志存储。
func NewBusLogStore(pool *pgxpool.Pool) *BusLogStore { return &BusLogStore{NewBaseStore(pool)} }

// Record 记录异常 (写入失败仅 debug 日志，不影响主流程)。
func (s *BusLogStore) Record(ctx context.Context, e *BusException) error {
	extraJSON, _ := json.Marshal(e.Extra)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO bus_exception_logs (ts, category, severity, source, tool_name, message, traceback, extra)
		 VALUES (NOW(), $1, $2, $3, $4, $5, $6, $7::jsonb)`,
		e.Category, e.Severity, e.Source, e.ToolName, e.Message, e.Traceback, string(extraJSON))
	if err != nil {
		logger.Debugw("bus_log write failed", "error", err)
	}
	return nil
}

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

// ListFilterValues 返回去重筛选值。
func (s *BusLogStore) ListFilterValues(ctx context.Context) (map[string][]string, error) {
	return DistinctMap(ctx, s.pool, "bus_exception_logs", "category", "severity")
}

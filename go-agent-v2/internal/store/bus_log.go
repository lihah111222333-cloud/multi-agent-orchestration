package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type BusLogStore struct{ BaseStore }

func NewBusLogStore(pool *pgxpool.Pool) *BusLogStore { return &BusLogStore{NewBaseStore(pool)} }

func (s *BusLogStore) List(ctx context.Context, category, severity, keyword string, limit int) ([]BusException, error) {
	sql, params := NewQueryBuilder().
		Eq("category", category).
		Eq("severity", severity).
		KeywordLike(keyword, "source", "tool_name", "message", "traceback").
		Build(
			"SELECT ts, category, severity, source, tool_name, message, traceback, extra FROM bus_exception_logs",
			"ts DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[BusException](rows)
}

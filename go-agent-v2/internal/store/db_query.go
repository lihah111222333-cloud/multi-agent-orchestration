package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type DBQueryStore struct{ BaseStore }

func NewDBQueryStore(pool *pgxpool.Pool) *DBQueryStore { return &DBQueryStore{NewBaseStore(pool)} }

func (s *DBQueryStore) Query(ctx context.Context, sqlText string, limit int) ([]map[string]any, error) {
	if err := ValidateReadOnlyQuery(sqlText); err != nil {
		return nil, err
	}
	limit = util.ClampInt(limit, 1, 2000)
	safeSql := strings.TrimRight(strings.TrimSpace(sqlText), ";")
	rows, err := s.pool.Query(ctx, "WITH q AS ("+safeSql+") SELECT * FROM q LIMIT $1", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	var results []map[string]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(fields))
		for i, fd := range fields {
			row[string(fd.Name)] = values[i]
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

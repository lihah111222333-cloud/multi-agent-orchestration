// db_query.go — 通用 SQL 查询/执行 (对应 Python agent_ops_store.py db_query / db_execute)。
// 使用 sql_safety.go 的 5 个验证函数确保安全。
package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// DBQueryStore 通用 SQL 执行器。
type DBQueryStore struct{ BaseStore }

// NewDBQueryStore 创建。
func NewDBQueryStore(pool *pgxpool.Pool) *DBQueryStore { return &DBQueryStore{NewBaseStore(pool)} }

// Query 执行只读查询 (对应 Python db_query)。
// 使用 ValidateReadOnlyQuery 确保安全 (strip literals + 写入关键词检测 + 单语句)。
func (s *DBQueryStore) Query(ctx context.Context, sqlText string, limit int) ([]map[string]any, error) {
	if err := ValidateReadOnlyQuery(sqlText); err != nil {
		return nil, err
	}
	limit = util.ClampInt(limit, 1, 2000)
	// 将用户 SQL 包装为 CTE，确保 LIMIT 始终作用于最终结果集 (避免 UNION/子查询中 LIMIT 歧义)
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

// Execute 执行变更语句 (对应 Python db_execute)。
// 使用 ValidateExecuteQuery 确保安全 (白名单 + 危险模式检测 + 单语句)。
func (s *DBQueryStore) Execute(ctx context.Context, sqlText string) (int64, error) {
	if err := ValidateExecuteQuery(sqlText); err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, sqlText)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

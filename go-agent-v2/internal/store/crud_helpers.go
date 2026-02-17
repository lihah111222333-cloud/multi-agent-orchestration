// crud_helpers.go — 通用 CRUD 操作 (消除 store 间重复的 Delete/DeleteBatch/SetEnabled)。
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeleteByKey 按主键删除单条记录。
func DeleteByKey(ctx context.Context, pool *pgxpool.Pool, table, keyCol, keyVal string) error {
	sql := fmt.Sprintf("DELETE FROM %s WHERE %s = $1",
		pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{keyCol}.Sanitize())
	_, err := pool.Exec(ctx, sql, keyVal)
	return err
}

// DeleteBatchByKeys 按主键批量删除。
func DeleteBatchByKeys(ctx context.Context, pool *pgxpool.Pool, table, keyCol string, keys []string) (int64, error) {
	sql := fmt.Sprintf("DELETE FROM %s WHERE %s = ANY($1::text[])",
		pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{keyCol}.Sanitize())
	tag, err := pool.Exec(ctx, sql, keys)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SetEnabledByKey 启用/禁用记录。
func SetEnabledByKey(ctx context.Context, pool *pgxpool.Pool, table, keyCol, keyVal, updatedBy string, enabled bool) error {
	sql := fmt.Sprintf("UPDATE %s SET enabled=$1, updated_by=$2, updated_at=NOW() WHERE %s=$3",
		pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{keyCol}.Sanitize())
	_, err := pool.Exec(ctx, sql, enabled, updatedBy, keyVal)
	return err
}

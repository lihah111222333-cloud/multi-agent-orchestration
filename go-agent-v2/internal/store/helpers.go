// helpers.go — Store 层 DRY 通用工具。
//
// 14 个 store 共享的查询模式:
//   - QueryBuilder: 动态 WHERE + LIKE 关键词搜索 + 分页
//   - collectRows:  pgx row → Go struct 泛型扫描
//   - DistinctValues: 去重列值 (筛选器下拉)
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// emptyJSON fallback 值: 不可序列化时返回空 JSON 对象。
var emptyJSON = []byte("{}")

// mustMarshalJSON 安全序列化: 失败时记录警告并返回 "{}"，不会 panic。
//
// 替代 store 层反复出现的 `data, _ := json.Marshal(v)` 模式，
// 消除静默丢弃序列化错误的合规风险。
func mustMarshalJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		logger.Warn("mustMarshalJSON: marshal failed, using fallback",
			"value_type", fmt.Sprintf("%T", v),
			logger.FieldError, err)
		return emptyJSON
	}
	return data
}

// BaseStore 所有 Store 的嵌入基底，持有连接池。
//
// 16 个 store 不再需要各自声明 struct{ pool *pgxpool.Pool } + NewXxxStore(pool)。
// 用法:
//
//	type FooStore struct{ BaseStore }
//	func NewFooStore(pool *pgxpool.Pool) *FooStore { return &FooStore{NewBaseStore(pool)} }
type BaseStore struct{ pool *pgxpool.Pool }

// NewBaseStore 创建 BaseStore。
func NewBaseStore(pool *pgxpool.Pool) BaseStore { return BaseStore{pool: pool} }

// ========================================
// QueryBuilder — 动态 WHERE 子句构造
// ========================================

// QueryBuilder 渐进式 SQL WHERE 拼接器。
// 14 个 store 共用，消除重复的 where/params/keyword 逻辑。
type QueryBuilder struct {
	where  []string
	params []any
	n      int // $N 参数计数器 (pgx 用 $1, $2, ...)
}

// NewQueryBuilder 创建空构造器。
func NewQueryBuilder() *QueryBuilder {
	return &QueryBuilder{}
}

// Eq 添加等值条件。空值跳过。
func (q *QueryBuilder) Eq(col, val string) *QueryBuilder {
	if val == "" {
		return q
	}
	q.n++
	q.where = append(q.where, fmt.Sprintf("%s = $%d", col, q.n))
	q.params = append(q.params, val)
	return q
}

// KeywordLike 添加多列 LIKE 关键词搜索。
// 对应 Python 中反复出现的 "(LOWER(a) LIKE $N OR LOWER(b) LIKE $N ...)" 模式。
func (q *QueryBuilder) KeywordLike(keyword string, cols ...string) *QueryBuilder {
	if keyword == "" || len(cols) == 0 {
		return q
	}
	kw := "%" + util.EscapeLike(strings.ToLower(keyword)) + "%"
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		q.n++
		parts = append(parts, fmt.Sprintf("LOWER(%s) LIKE $%d ESCAPE E'\\\\'", c, q.n))
		q.params = append(q.params, kw)
	}
	q.where = append(q.where, "("+strings.Join(parts, " OR ")+")")
	return q
}

// Build 构建完整 SQL: baseSql + WHERE + ORDER BY + LIMIT。
func (q *QueryBuilder) Build(baseSql, orderBy string, limit int) (string, []any) {
	limit = util.ClampInt(limit, 1, 2000)
	sql := baseSql
	if len(q.where) > 0 {
		sql += " WHERE " + strings.Join(q.where, " AND ")
	}
	if orderBy != "" {
		sql += " ORDER BY " + orderBy
	}
	q.n++
	sql += fmt.Sprintf(" LIMIT $%d", q.n)
	q.params = append(q.params, limit)
	return sql, q.params
}

// ========================================
// collectRows — 泛型行扫描
// ========================================

// collectRows 使用 pgx.CollectRows + RowToStructByNameLax 扫描行到 struct slice。
// 消除 Python 中 9 个 _row_to_* 转换函数 (~156 行)。
func collectRows[T any](rows pgx.Rows) ([]T, error) {
	return pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
}

// collectOne 扫描单行，无结果返回 nil。
func collectOne[T any](rows pgx.Rows) (*T, error) {
	items, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return &items[0], nil
}

// CollectOneExported 是 collectOne 的导出版本，供 executor 等外部包使用。
func CollectOneExported[T any](rows pgx.Rows) (*T, error) {
	return collectOne[T](rows)
}

// CollectRowsExported 是 collectRows 的导出版本，供 executor 等外部包使用。
func CollectRowsExported[T any](rows pgx.Rows) ([]T, error) {
	return collectRows[T](rows)
}

// ========================================
// DistinctValues — 筛选器下拉值
// ========================================

// DistinctValues 查询表中指定列的去重值 (筛选 UI 用)。
// 消除 Python 中 5 个 list_filter_values 的重复 DISTINCT 查询。
func DistinctValues(ctx context.Context, pool *pgxpool.Pool, table, column string) ([]string, error) {
	safeTable := pgx.Identifier{table}.Sanitize()
	safeCol := pgx.Identifier{column}.Sanitize()
	sql := fmt.Sprintf(
		"SELECT DISTINCT %s AS value FROM %s WHERE %s <> '' ORDER BY value",
		safeCol, safeTable, safeCol,
	)
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

// DistinctMap 批量查询多列去重值。
// 用于一次性返回 filters = {"levels": [...], "loggers": [...]} 的场景。
func DistinctMap(ctx context.Context, pool *pgxpool.Pool, table string, columns ...string) (map[string][]string, error) {
	result := make(map[string][]string, len(columns))
	for _, col := range columns {
		vals, err := DistinctValues(ctx, pool, table, col)
		if err != nil {
			return nil, err
		}
		result[col] = vals
	}
	return result, nil
}

// ========================================
// 通用 CRUD 操作 (DRY: 消除 store 间重复的 Delete/SetEnabled)
// ========================================

// DeleteByKey 按主键删除单条记录。
func DeleteByKey(ctx context.Context, pool *pgxpool.Pool, table, keyCol, keyVal string) error {
	sql := fmt.Sprintf("DELETE FROM %s WHERE %s = $1",
		pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{keyCol}.Sanitize())
	_, err := pool.Exec(ctx, sql, keyVal)
	return err
}

// SetEnabledByKey 启用/禁用记录。
func SetEnabledByKey(ctx context.Context, pool *pgxpool.Pool, table, keyCol, keyVal, updatedBy string, enabled bool) error {
	sql := fmt.Sprintf("UPDATE %s SET enabled=$1, updated_by=$2, updated_at=NOW() WHERE %s=$3",
		pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{keyCol}.Sanitize())
	_, err := pool.Exec(ctx, sql, enabled, updatedBy, keyVal)
	return err
}

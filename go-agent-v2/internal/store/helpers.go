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

	sq "github.com/Masterminds/squirrel"
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

// defaultStr 空字符串返回默认值。
//
// 14 个 store 共用: 给 status / priority / risk_level 等字段提供默认值。
func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
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

// sanitizeCol 消毒 SQL 列名，防止注入。
//
// QueryBuilder 的 Eq/Gte/KeywordLike 使用此函数，与 DeleteByKey/SetEnabledByKey 中
// 的 pgx.Identifier{}.Sanitize() 保持一致。
func sanitizeCol(col string) string {
	return pgx.Identifier{col}.Sanitize()
}

// ========================================
// QueryBuilder — 动态 WHERE 子句构造 (基于 squirrel)
// ========================================

// QueryBuilder 渐进式 SQL WHERE 拼接器。
// 14 个 store 共用。内部使用 squirrel 构建 SQL，外部 API 保持不变。
type QueryBuilder struct {
	where sq.And // squirrel And 条件组
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
	q.where = append(q.where, sq.Eq{sanitizeCol(col): val})
	return q
}

// KeywordLike 添加多列 LIKE 关键词搜索。
// 生成: (LOWER(a) LIKE $N ESCAPE E'\\' OR LOWER(b) LIKE $N ESCAPE E'\\' ...)
func (q *QueryBuilder) KeywordLike(keyword string, cols ...string) *QueryBuilder {
	if keyword == "" || len(cols) == 0 {
		return q
	}
	kw := "%" + util.EscapeLike(strings.ToLower(keyword)) + "%"
	var or sq.Or
	for _, c := range cols {
		or = append(or, sq.Expr(
			fmt.Sprintf("LOWER(%s) LIKE ? ESCAPE E'\\\\'", sanitizeCol(c)),
			kw,
		))
	}
	q.where = append(q.where, or)
	return q
}

// Gte 添加 >= 条件。nil 值跳过。
func (q *QueryBuilder) Gte(col string, val any) *QueryBuilder {
	if val == nil {
		return q
	}
	q.where = append(q.where, sq.GtOrEq{sanitizeCol(col): val})
	return q
}

// Build 构建完整 SQL: baseSql + WHERE + ORDER BY + LIMIT。
func (q *QueryBuilder) Build(baseSql, orderBy string, limit int) (string, []any) {
	limit = util.ClampInt(limit, 1, 2000)

	builder := sq.Select("1").PlaceholderFormat(sq.Dollar) // 占位: 以 baseSql 为基准重构
	if len(q.where) > 0 {
		builder = builder.Where(q.where)
	}

	// 从 builder 中提取 WHERE 子句和参数
	// squirrel 的 Select builder 不支持直接拼 baseSql，因此手动拼接
	_, args, _ := builder.ToSql()

	result := baseSql
	if len(q.where) > 0 {
		whereSql, whereArgs, _ := q.where.ToSql()
		// squirrel 用 ? 占位符，转换为 $N (Dollar format)
		converted := dollarReplace(whereSql, 1)
		result += " WHERE " + converted
		args = whereArgs
	}

	if orderBy != "" {
		result += " ORDER BY " + orderBy
	}

	result += fmt.Sprintf(" LIMIT $%d", len(args)+1)
	args = append(args, limit)
	return result, args
}

// dollarReplace 将 ? 占位符替换为 $1, $2, $3...（从 start 开始编号）。
func dollarReplace(sql string, start int) string {
	var buf strings.Builder
	buf.Grow(len(sql) + 20)
	n := start
	for i := 0; i < len(sql); i++ {
		if sql[i] == '?' {
			buf.WriteString(fmt.Sprintf("$%d", n))
			n++
		} else {
			buf.WriteByte(sql[i])
		}
	}
	return buf.String()
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

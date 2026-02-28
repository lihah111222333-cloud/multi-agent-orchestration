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

var emptyJSON = []byte("{}")

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

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

type BaseStore struct{ pool *pgxpool.Pool }

func NewBaseStore(pool *pgxpool.Pool) BaseStore { return BaseStore{pool: pool} }

func sanitizeCol(col string) string { return pgx.Identifier{col}.Sanitize() }

type QueryBuilder struct {
	where sq.And
}

func NewQueryBuilder() *QueryBuilder { return &QueryBuilder{} }

func (q *QueryBuilder) Eq(col, val string) *QueryBuilder {
	if val == "" {
		return q
	}
	q.where = append(q.where, sq.Eq{sanitizeCol(col): val})
	return q
}

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

func (q *QueryBuilder) Gte(col string, val any) *QueryBuilder {
	if val == nil {
		return q
	}
	q.where = append(q.where, sq.GtOrEq{sanitizeCol(col): val})
	return q
}

func (q *QueryBuilder) Build(baseSQL, orderBy string, limit int) (string, []any) {
	limit = util.ClampInt(limit, 1, 2000)
	result := baseSQL
	args := make([]any, 0, 4)

	if len(q.where) > 0 {
		whereSQL, whereArgs, _ := q.where.ToSql()
		result += " WHERE " + dollarReplace(whereSQL, 1)
		args = whereArgs
	}
	if orderBy != "" {
		result += " ORDER BY " + orderBy
	}

	result += fmt.Sprintf(" LIMIT $%d", len(args)+1)
	args = append(args, limit)
	return result, args
}

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

func collectRows[T any](rows pgx.Rows) ([]T, error) {
	return pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
}

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

func CollectOneExported[T any](rows pgx.Rows) (*T, error) { return collectOne[T](rows) }

func CollectRowsExported[T any](rows pgx.Rows) ([]T, error) { return collectRows[T](rows) }

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

func DistinctMap(ctx context.Context, pool *pgxpool.Pool, table string, columns ...string) (map[string][]string, error) {
	result := make(map[string][]string, len(columns))
	var err error
	for _, col := range columns {
		if result[col], err = DistinctValues(ctx, pool, table, col); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func DeleteByKey(ctx context.Context, pool *pgxpool.Pool, table, keyCol, keyVal string) error {
	sql := fmt.Sprintf("DELETE FROM %s WHERE %s = $1",
		pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{keyCol}.Sanitize())
	_, err := pool.Exec(ctx, sql, keyVal)
	return err
}

func SetEnabledByKey(ctx context.Context, pool *pgxpool.Pool, table, keyCol, keyVal, updatedBy string, enabled bool) error {
	sql := fmt.Sprintf("UPDATE %s SET enabled=$1, updated_by=$2, updated_at=NOW() WHERE %s=$3",
		pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{keyCol}.Sanitize())
	_, err := pool.Exec(ctx, sql, enabled, updatedBy, keyVal)
	return err
}

// Package database 提供 PostgreSQL 连接池管理。
//
// 使用 pgxpool 直接管理连接，裸写 SQL (不使用 ORM)。
// 对应 Python db/postgres.py。
package database

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// NewPool 创建 PostgreSQL 连接池。
// 对应 Python db/postgres.py 的 _init_pool。
func NewPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	if cfg.PostgresConnStr == "" {
		return nil, fmt.Errorf("POSTGRES_CONNECTION_STRING is required")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.PostgresConnStr)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}

	poolCfg.MinConns = safeInt32(cfg.PostgresPoolMinSize, "PostgresPoolMinSize")
	poolCfg.MaxConns = safeInt32(cfg.PostgresPoolMaxSize, "PostgresPoolMaxSize")

	// AfterConnect: 设置 search_path (使用 quote_ident 防止 SQL 注入)
	schema := cfg.PostgresSchema
	if schema != "" && schema != "public" {
		poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s", pgx.Identifier{schema}.Sanitize()))
			return err
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// 验证连接
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	logger.Infow("postgres pool created",
		"min_conns", cfg.PostgresPoolMinSize,
		"max_conns", cfg.PostgresPoolMaxSize,
		"schema", schema,
	)
	return pool, nil
}

// safeInt32 将 int 安全转为 int32，超出范围时 clamp 并记录警告。
func safeInt32(v int, name string) int32 {
	if v > math.MaxInt32 {
		logger.Warn("pool config overflow, clamped to MaxInt32", "field", name, "value", v)
		return math.MaxInt32
	}
	if v < 0 {
		logger.Warn("pool config negative, clamped to 0", "field", name, "value", v)
		return 0
	}
	return int32(v)
}

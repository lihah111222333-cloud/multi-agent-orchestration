package database

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/internal/config"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func NewPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	if cfg.PostgresConnStr == "" { return nil, apperrors.New("NewPool", "POSTGRES_CONNECTION_STRING is required") }

	poolCfg, err := pgxpool.ParseConfig(cfg.PostgresConnStr)
	if err != nil { return nil, apperrors.Wrap(err, "NewPool", "parse postgres config") }

	poolCfg.MinConns = safeInt32(cfg.PostgresPoolMinSize, "PostgresPoolMinSize")
	poolCfg.MaxConns = safeInt32(cfg.PostgresPoolMaxSize, "PostgresPoolMaxSize")

	if schema := cfg.PostgresSchema; schema != "" && schema != "public" {
		poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s", pgx.Identifier{schema}.Sanitize()))
			return err
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil { return nil, apperrors.Wrap(err, "NewPool", "create pool") }

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, apperrors.Wrap(err, "NewPool", "ping postgres")
	}

	logger.Info("database: postgres pool created",
		"min_conns", cfg.PostgresPoolMinSize,
		"max_conns", cfg.PostgresPoolMaxSize,
		"schema", cfg.PostgresSchema,
	)
	return pool, nil
}

func safeInt32(v int, name string) int32 {
	if v > math.MaxInt32 {
		logger.Warn("pool config overflow, clamped to MaxInt32", "field", name, "value", v)
		return math.MaxInt32
	}
	if v < 0 { logger.Warn("pool config negative, clamped to 0", "field", name, "value", v); return 0 }
	return int32(v)
}

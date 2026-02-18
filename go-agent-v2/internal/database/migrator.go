package database

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// Migrate 执行 migrations 目录下的 SQL 迁移脚本 (按文件名排序)。
// 使用 schema_version 表追踪已执行版本。
// 对应 Python db/migrator.py。
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	if pool == nil {
		return apperrors.New("Migrate", "pool is required")
	}

	// 确保 schema_version 表存在
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		logger.Error("migrate: create schema_version table failed", logger.FieldError, err)
		return apperrors.Wrap(err, "Migrate", "create schema_version table")
	}

	// 读取迁移文件
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("no migrations directory found, skipping")
			return nil
		}
		return apperrors.Wrap(err, "Migrate", "read migrations dir")
	}

	// 过滤并排序 .sql 文件
	var sqlFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			sqlFiles = append(sqlFiles, e.Name())
		}
	}
	sort.Strings(sqlFiles)

	applied, err := loadAppliedVersions(ctx, pool)
	if err != nil {
		return err
	}

	// 执行未应用的迁移
	pending := countPendingMigrations(sqlFiles, applied)
	if pending > 0 {
		logger.Infow("migrate: applying pending migrations", logger.FieldCount, pending)
	}
	for _, name := range sqlFiles {
		if applied[name] {
			continue
		}
		if err := applyOneMigration(ctx, pool, migrationsDir, name); err != nil {
			return err
		}
		logger.Infow("migration applied", logger.FieldVersion, name)
	}

	return nil
}

func loadAppliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	if pool == nil {
		return nil, apperrors.New("Migrate", "pool is required")
	}
	rows, err := pool.Query(ctx, `SELECT version FROM schema_version`)
	if err != nil {
		return nil, apperrors.Wrap(err, "Migrate", "query schema_version")
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, apperrors.Wrap(err, "Migrate", "scan schema_version")
		}
		applied[version] = true
	}
	return applied, nil
}

func applyOneMigration(ctx context.Context, pool *pgxpool.Pool, migrationsDir, name string) error {
	if pool == nil {
		return apperrors.New("Migrate", "pool is required")
	}
	sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir, name))
	if err != nil {
		return apperrors.Wrapf(err, "Migrate", "read migration %s", name)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return apperrors.Wrapf(err, "Migrate", "begin tx for %s", name)
	}
	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		_ = tx.Rollback(ctx)
		return apperrors.Wrapf(err, "Migrate", "exec migration %s", name)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_version (version) VALUES ($1)`, name); err != nil {
		_ = tx.Rollback(ctx)
		return apperrors.Wrapf(err, "Migrate", "record migration %s", name)
	}
	if err := tx.Commit(ctx); err != nil {
		return apperrors.Wrapf(err, "Migrate", "commit migration %s", name)
	}
	return nil
}

func countPendingMigrations(sqlFiles []string, applied map[string]bool) int {
	pending := 0
	for _, name := range sqlFiles {
		if !applied[name] {
			pending++
		}
	}
	return pending
}

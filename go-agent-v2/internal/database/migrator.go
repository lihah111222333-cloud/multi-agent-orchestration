package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// Migrate 执行 migrations 目录下的 SQL 迁移脚本 (按文件名排序)。
// 使用 schema_version 表追踪已执行版本。
// 对应 Python db/migrator.py。
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	// 确保 schema_version 表存在
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	// 读取迁移文件
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("no migrations directory found, skipping")
			return nil
		}
		return fmt.Errorf("read migrations dir: %w", err)
	}

	// 过滤并排序 .sql 文件
	var sqlFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			sqlFiles = append(sqlFiles, e.Name())
		}
	}
	sort.Strings(sqlFiles)

	// 查询已执行版本
	rows, err := pool.Query(ctx, `SELECT version FROM schema_version`)
	if err != nil {
		return fmt.Errorf("query schema_version: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("scan schema_version: %w", err)
		}
		applied[v] = true
	}

	// 执行未应用的迁移
	for _, name := range sqlFiles {
		if applied[name] {
			continue
		}

		sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}

		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("exec migration %s: %w", name, err)
		}

		if _, err := tx.Exec(ctx, `INSERT INTO schema_version (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}

		logger.Infow("migration applied", "version", name)
	}

	return nil
}

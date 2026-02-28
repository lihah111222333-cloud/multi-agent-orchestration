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

func Migrate(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	if pool == nil {
		return apperrors.New("Migrate", "pool is required")
	}
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
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("no migrations directory found, skipping")
			return nil
		}
		return apperrors.Wrap(err, "Migrate", "read migrations dir")
	}
	var sqlFiles []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		sqlFiles = append(sqlFiles, e.Name())
	}
	sort.Strings(sqlFiles)

	applied, err := loadAppliedVersions(ctx, pool)
	if err != nil {
		return err
	}
	var pending []string
	for _, name := range sqlFiles {
		if applied[name] {
			continue
		}
		pending = append(pending, name)
	}
	if len(pending) == 0 {
		return nil
	}
	logger.Info("migrate: applying pending migrations", logger.FieldCount, len(pending))
	for _, name := range pending {
		if err := applyOneMigration(ctx, pool, migrationsDir, name); err != nil {
			return err
		}
		logger.Info("migrate: migration applied", logger.FieldVersion, name)
	}

	return nil
}

func loadAppliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
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
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap(err, "Migrate", "iterate schema_version")
	}
	return applied, nil
}

func applyOneMigration(ctx context.Context, pool *pgxpool.Pool, migrationsDir, name string) error {
	sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir, name))
	if err != nil {
		return apperrors.Wrapf(err, "Migrate", "read migration %s", name)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return apperrors.Wrapf(err, "Migrate", "begin tx for %s", name)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		return apperrors.Wrapf(err, "Migrate", "exec migration %s", name)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_version (version) VALUES ($1)`, name); err != nil {
		return apperrors.Wrapf(err, "Migrate", "record migration %s", name)
	}
	if err := tx.Commit(ctx); err != nil {
		return apperrors.Wrapf(err, "Migrate", "commit migration %s", name)
	}
	return nil
}

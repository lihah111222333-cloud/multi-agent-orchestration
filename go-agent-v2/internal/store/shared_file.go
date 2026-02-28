package store

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SharedFileStore struct{ BaseStore }

func NewSharedFileStore(pool *pgxpool.Pool) *SharedFileStore {
	return &SharedFileStore{NewBaseStore(pool)}
}

func normalizePath(path string) string {
	return strings.Trim(filepath.ToSlash(strings.TrimSpace(path)), "/")
}

func (s *SharedFileStore) Write(ctx context.Context, path, content, actor string) (*SharedFile, error) {
	if path = normalizePath(path); path == "" {
		return nil, ErrInvalidPath
	}
	rows, err := s.pool.Query(ctx,
		`INSERT INTO shared_files (path, content, updated_by, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())
		 ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_by=EXCLUDED.updated_by, updated_at=NOW()
		 RETURNING path, content, updated_by, created_at, updated_at`,
		path, content, actor)
	if err != nil {
		return nil, err
	}
	return collectOne[SharedFile](rows)
}

func (s *SharedFileStore) Read(ctx context.Context, path string) (*SharedFile, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT path, content, updated_by, created_at, updated_at FROM shared_files WHERE path = $1",
		normalizePath(path))
	if err != nil {
		return nil, err
	}
	return collectOne[SharedFile](rows)
}

func (s *SharedFileStore) List(ctx context.Context, prefix string, limit int) ([]SharedFile, error) {
	q := NewQueryBuilder()
	if prefix = normalizePath(prefix); prefix != "" {
		q.KeywordLike(prefix, "path")
	}
	sql, params := q.Build(
		"SELECT path, content, updated_by, created_at, updated_at FROM shared_files",
		"updated_at DESC, path ASC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[SharedFile](rows)
}

func (s *SharedFileStore) Delete(ctx context.Context, path, _ string) (bool, error) {
	tag, err := s.pool.Exec(ctx, "DELETE FROM shared_files WHERE path = $1", normalizePath(path))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

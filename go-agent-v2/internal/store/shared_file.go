// shared_file.go — 共享文件存储 CRUD (对应 Python shared_file_store.py)。
package store

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SharedFileStore 共享文件存储。
type SharedFileStore struct{ BaseStore }

// NewSharedFileStore 创建共享文件存储。
func NewSharedFileStore(pool *pgxpool.Pool) *SharedFileStore { return &SharedFileStore{NewBaseStore(pool)} }

// normalizePath 清理路径。
func normalizePath(path string) string {
	p := strings.TrimSpace(filepath.ToSlash(path))
	p = strings.Trim(p, "/")
	return p
}

// Write 写入文件 (UPSERT)。
func (s *SharedFileStore) Write(ctx context.Context, path, content, actor string) (*SharedFile, error) {
	p := normalizePath(path)
	if p == "" {
		return nil, ErrInvalidPath
	}
	rows, err := s.pool.Query(ctx,
		`INSERT INTO shared_files (path, content, updated_by, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())
		 ON CONFLICT (path) DO UPDATE SET content=EXCLUDED.content, updated_by=EXCLUDED.updated_by, updated_at=NOW()
		 RETURNING path, content, updated_by, created_at, updated_at`,
		p, content, actor)
	if err != nil {
		return nil, err
	}
	return collectOne[SharedFile](rows)
}

// Read 读取文件。
func (s *SharedFileStore) Read(ctx context.Context, path string) (*SharedFile, error) {
	p := normalizePath(path)
	rows, err := s.pool.Query(ctx,
		"SELECT path, content, updated_by, created_at, updated_at FROM shared_files WHERE path = $1", p)
	if err != nil {
		return nil, err
	}
	return collectOne[SharedFile](rows)
}

// List 列表查询文件。
func (s *SharedFileStore) List(ctx context.Context, prefix string, limit int) ([]SharedFile, error) {
	q := NewQueryBuilder()
	if prefix != "" {
		np := normalizePath(prefix)
		q.KeywordLike(np, "path")
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

// Delete 删除文件。
func (s *SharedFileStore) Delete(ctx context.Context, path, actor string) (bool, error) {
	p := normalizePath(path)
	tag, err := s.pool.Exec(ctx, "DELETE FROM shared_files WHERE path = $1", p)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

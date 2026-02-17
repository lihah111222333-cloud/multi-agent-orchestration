// codex_memory.go — codex_memory 表 Store CRUD。
//
// 管理 Agent 记忆的增删查改，每条记忆关联到 agent_id 和 thread_id。
// 记忆类型: fact (事实) / preference (偏好) / instruction (指令)。
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CodexMemoryStore 封装 codex_memory 表操作。
type CodexMemoryStore struct{ BaseStore }

// NewCodexMemoryStore 创建 CodexMemoryStore。
func NewCodexMemoryStore(pool *pgxpool.Pool) *CodexMemoryStore {
	return &CodexMemoryStore{NewBaseStore(pool)}
}

// memorySelectCols 查询列常量 (DRY: 所有查询复用)。
const memorySelectCols = `id, agent_id, thread_id, memory_type, content, source, created_at, updated_at`

// memoryBaseSql 基础 SELECT (DRY: List / ByThread 复用)。
const memoryBaseSql = `SELECT ` + memorySelectCols + ` FROM codex_memory`

// scanMemoryRow 从 pgx.Row 扫描到 CodexMemory (DRY: 所有单行查询复用)。
func scanMemoryRow(row pgx.Row) (CodexMemory, error) {
	var m CodexMemory
	err := row.Scan(&m.ID, &m.AgentID, &m.ThreadID, &m.MemoryType,
		&m.Content, &m.Source, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

// scanMemoryRows 从 pgx.Rows 收集所有 CodexMemory (DRY: 所有多行查询复用)。
func scanMemoryRows(rows pgx.Rows) ([]CodexMemory, error) {
	defer rows.Close()
	var result []CodexMemory
	for rows.Next() {
		var m CodexMemory
		err := rows.Scan(&m.ID, &m.AgentID, &m.ThreadID, &m.MemoryType,
			&m.Content, &m.Source, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("codex_memory: scan: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// Create 创建一条记忆。
func (s *CodexMemoryStore) Create(ctx context.Context, agentID, threadID, memoryType, content, source string) (*CodexMemory, error) {
	if content == "" {
		return nil, fmt.Errorf("codex_memory: content must not be empty")
	}
	if memoryType == "" {
		memoryType = "fact"
	}
	if source == "" {
		source = "auto"
	}

	sql := `INSERT INTO codex_memory (agent_id, thread_id, memory_type, content, source)
	        VALUES ($1, $2, $3, $4, $5)
	        RETURNING ` + memorySelectCols

	row := s.pool.QueryRow(ctx, sql, agentID, threadID, memoryType, content, source)
	m, err := scanMemoryRow(row)
	if err != nil {
		return nil, fmt.Errorf("codex_memory: create: %w", err)
	}
	return &m, nil
}

// Get 按 ID 查询单条记忆。
func (s *CodexMemoryStore) Get(ctx context.Context, id int64) (*CodexMemory, error) {
	sql := memoryBaseSql + ` WHERE id = $1`
	row := s.pool.QueryRow(ctx, sql, id)
	m, err := scanMemoryRow(row)
	if err != nil {
		return nil, fmt.Errorf("codex_memory: get %d: %w", id, err)
	}
	return &m, nil
}

// List 查询记忆列表 (按 agent_id / memory_type 过滤)。
func (s *CodexMemoryStore) List(ctx context.Context, agentID, memoryType string, limit int) ([]CodexMemory, error) {
	qb := NewQueryBuilder().
		Eq("agent_id", agentID).
		Eq("memory_type", memoryType)

	if limit <= 0 {
		limit = 200
	}

	sql, params := qb.Build(memoryBaseSql, "created_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("codex_memory: list: %w", err)
	}
	return scanMemoryRows(rows)
}

// ByThread 按 thread_id 查询记忆。
func (s *CodexMemoryStore) ByThread(ctx context.Context, threadID string, limit int) ([]CodexMemory, error) {
	qb := NewQueryBuilder().Eq("thread_id", threadID)
	if limit <= 0 {
		limit = 200
	}

	sql, params := qb.Build(memoryBaseSql, "created_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("codex_memory: by_thread: %w", err)
	}
	return scanMemoryRows(rows)
}

// Update 更新记忆内容。
func (s *CodexMemoryStore) Update(ctx context.Context, id int64, content string) error {
	if content == "" {
		return fmt.Errorf("codex_memory: content must not be empty")
	}
	sql := `UPDATE codex_memory SET content = $1, updated_at = $2 WHERE id = $3`
	ct, err := s.pool.Exec(ctx, sql, content, time.Now(), id)
	if err != nil {
		return fmt.Errorf("codex_memory: update %d: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("codex_memory: not found %d", id)
	}
	return nil
}

// Delete 删除一条记忆。
func (s *CodexMemoryStore) Delete(ctx context.Context, id int64) error {
	sql := `DELETE FROM codex_memory WHERE id = $1`
	ct, err := s.pool.Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("codex_memory: delete %d: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("codex_memory: not found %d", id)
	}
	return nil
}

// DeleteByAgent 删除指定 agent 的所有记忆。
func (s *CodexMemoryStore) DeleteByAgent(ctx context.Context, agentID string) (int64, error) {
	sql := `DELETE FROM codex_memory WHERE agent_id = $1`
	ct, err := s.pool.Exec(ctx, sql, agentID)
	if err != nil {
		return 0, fmt.Errorf("codex_memory: delete_by_agent: %w", err)
	}
	return ct.RowsAffected(), nil
}

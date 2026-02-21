// agent_codex_binding.go — Agent ↔ Codex Thread 1:1 共生绑定 CRUD。
//
// ╔══════════════════════════════════════════════════════════════════════╗
// ║  ⚠️  根基约束 — 不允许修改绑定逻辑  ⚠️                           ║
// ║                                                                    ║
// ║  agent_id 与 codex_thread_id 是 1:1 共生关系:                      ║
// ║    - Bind:   创建绑定 (INSERT, 主键+唯一约束保证 1:1)              ║
// ║    - Unbind: 删除绑定 (DELETE, 共生共灭)                           ║
// ║    - 不提供 UPDATE codex_thread_id 的方法 — 要换就先删再建         ║
// ╚══════════════════════════════════════════════════════════════════════╝
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentCodexBinding agent_id ↔ codex_thread_id 1:1 绑定记录。
//
// 约束由 DB 保证 (PK + UNIQUE), Go 侧只做 CRUD, 不做额外校验。
type AgentCodexBinding struct {
	AgentID       string `json:"agent_id"`
	CodexThreadID string `json:"codex_thread_id"`
	RolloutPath   string `json:"rollout_path,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

// AgentCodexBindingStore agent_codex_binding 表操作。
type AgentCodexBindingStore struct{ BaseStore }

// NewAgentCodexBindingStore 创建。
func NewAgentCodexBindingStore(pool *pgxpool.Pool) *AgentCodexBindingStore {
	return &AgentCodexBindingStore{NewBaseStore(pool)}
}

const acbCols = "agent_id, codex_thread_id, rollout_path, created_at, updated_at"

// Bind 创建 1:1 绑定 (幂等: 同一 agent_id 重复绑定同一 codex_thread_id 不报错)。
//
// 如果 agent_id 已绑定不同的 codex_thread_id, 会被 PK 约束拦截 → 返回错误。
// 调用方需要先 Unbind 旧绑定再 Bind 新的 (显式操作, 有审计痕迹)。
func (s *AgentCodexBindingStore) Bind(ctx context.Context, agentID, codexThreadID, rolloutPath string) error {
	now := time.Now().Unix()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agent_codex_binding (agent_id, codex_thread_id, rollout_path, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (agent_id) DO UPDATE SET
		   codex_thread_id = EXCLUDED.codex_thread_id,
		   rollout_path    = EXCLUDED.rollout_path,
		   updated_at      = EXCLUDED.updated_at`,
		agentID, codexThreadID, rolloutPath, now, now)
	return err
}

// Unbind 删除绑定 (共生共灭: agent 删除时调用)。
func (s *AgentCodexBindingStore) Unbind(ctx context.Context, agentID string) error {
	_, err := s.pool.Exec(ctx,
		"DELETE FROM agent_codex_binding WHERE agent_id = $1", agentID)
	return err
}

// FindByAgentID 按 agent_id 查找绑定的 codex_thread_id。
//
// 返回 nil 表示该 agent 尚未绑定 (首次启动)。
func (s *AgentCodexBindingStore) FindByAgentID(ctx context.Context, agentID string) (*AgentCodexBinding, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+acbCols+" FROM agent_codex_binding WHERE agent_id = $1", agentID)
	if err != nil {
		return nil, err
	}
	return collectOne[AgentCodexBinding](rows)
}

// Deprecated: FindByCodexThreadID 无外部调用者。
func (s *AgentCodexBindingStore) FindByCodexThreadID(ctx context.Context, codexThreadID string) (*AgentCodexBinding, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+acbCols+" FROM agent_codex_binding WHERE codex_thread_id = $1", codexThreadID)
	if err != nil {
		return nil, err
	}
	return collectOne[AgentCodexBinding](rows)
}

// ListAll 返回所有绑定 (调试/运维用)。
func (s *AgentCodexBindingStore) ListAll(ctx context.Context) ([]AgentCodexBinding, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+acbCols+" FROM agent_codex_binding ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	return collectRows[AgentCodexBinding](rows)
}

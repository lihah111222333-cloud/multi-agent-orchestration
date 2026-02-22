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
	"fmt"
	"strings"
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

// Bind 创建 1:1 绑定。
//
// 约束:
//   - agent_id 与 codex_thread_id 的关系一旦建立不可改写；
//   - 若同一 agent_id 再次绑定到不同 codex_thread_id，直接返回错误；
//   - 仅允许在关系不变时补充 rollout_path 元数据。
func (s *AgentCodexBindingStore) Bind(ctx context.Context, agentID, codexThreadID, rolloutPath string) error {
	agentID = strings.TrimSpace(agentID)
	codexThreadID = strings.TrimSpace(codexThreadID)
	rolloutPath = strings.TrimSpace(rolloutPath)
	if agentID == "" || codexThreadID == "" {
		return fmt.Errorf("bind requires non-empty agent_id and codex_thread_id")
	}

	existing, err := s.FindByAgentID(ctx, agentID)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if existing != nil {
		if strings.TrimSpace(existing.CodexThreadID) != codexThreadID {
			return fmt.Errorf("immutable binding violation: agent %q already bound to %q, cannot bind to %q",
				agentID, strings.TrimSpace(existing.CodexThreadID), codexThreadID)
		}
		// 关系未变化时允许补写/刷新 rollout_path，避免归档后历史入口丢失。
		if rolloutPath != "" && rolloutPath != strings.TrimSpace(existing.RolloutPath) {
			_, err = s.pool.Exec(ctx,
				`UPDATE agent_codex_binding
				 SET rollout_path = $1, updated_at = $2
				 WHERE agent_id = $3 AND codex_thread_id = $4`,
				rolloutPath, now, agentID, codexThreadID)
			return err
		}
		return nil
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO agent_codex_binding (agent_id, codex_thread_id, rollout_path, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
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

// agent_message.go — agent_messages 表 CRUD (消息持久化)。
//
// 记录所有 agent 事件消息, 支持前端按 agentID 加载历史。
package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentMessage agent 消息记录。
type AgentMessage struct {
	ID        int64           `db:"id" json:"id"`
	AgentID   string          `db:"agent_id" json:"agentId"`
	Role      string          `db:"role" json:"role"` // user | assistant | tool | system
	EventType string          `db:"event_type" json:"eventType"`
	Method    string          `db:"method" json:"method"`
	Content   string          `db:"content" json:"content"`
	Metadata  json.RawMessage `db:"metadata" json:"metadata,omitempty"`
	CreatedAt time.Time       `db:"created_at" json:"createdAt"`
}

// AgentMessageStore agent_messages 存储。
type AgentMessageStore struct{ BaseStore }

// NewAgentMessageStore 创建。
func NewAgentMessageStore(pool *pgxpool.Pool) *AgentMessageStore {
	return &AgentMessageStore{NewBaseStore(pool)}
}

const amCols = "id, agent_id, role, event_type, method, content, metadata, created_at"

// Insert 写入单条消息。
func (s *AgentMessageStore) Insert(ctx context.Context, msg *AgentMessage) error {
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agent_messages (agent_id, role, event_type, method, content, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		msg.AgentID, msg.Role, msg.EventType, msg.Method, msg.Content, msg.Metadata, msg.CreatedAt)
	return err
}

// ListByAgent 按 agentID 查询历史消息 (最新在前, 支持游标分页)。
//
//	before=0 → 从最新开始; before>0 → id < before
func (s *AgentMessageStore) ListByAgent(ctx context.Context, agentID string, limit int, before int64) ([]AgentMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	var sql string
	var args []any
	if before > 0 {
		sql = "SELECT " + amCols + " FROM agent_messages WHERE agent_id=$1 AND id < $2 ORDER BY id DESC LIMIT $3"
		args = []any{agentID, before, limit}
	} else {
		sql = "SELECT " + amCols + " FROM agent_messages WHERE agent_id=$1 ORDER BY id DESC LIMIT $2"
		args = []any{agentID, limit}
	}

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return collectRows[AgentMessage](rows)
}

// CountByAgent 统计某 agent 的消息总数。
func (s *AgentMessageStore) CountByAgent(ctx context.Context, agentID string) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM agent_messages WHERE agent_id=$1", agentID).Scan(&count)
	return count, err
}

// DeleteByAgent 删除某 agent 的所有消息。
func (s *AgentMessageStore) DeleteByAgent(ctx context.Context, agentID string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM agent_messages WHERE agent_id=$1", agentID)
	return err
}

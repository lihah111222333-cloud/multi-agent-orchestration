// agent_message.go — agent_messages 表 CRUD (消息持久化)。
//
// 记录所有 agent 事件消息, 支持前端按 agentID 加载历史。
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
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
	DedupKey  string          `db:"dedup_key" json:"-"`
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

func sanitizeMetadata(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	clean := bytes.ToValidUTF8(raw, []byte("�"))
	if json.Valid(clean) {
		return json.RawMessage(clean)
	}
	fallback, _ := json.Marshal(map[string]any{
		"raw": string(clean),
	})
	return json.RawMessage(fallback)
}

// BuildMessageDedupKey 生成消息去重键。
//
// 仅对已知可能重复的“幂等事件”生成 key（例如 turn/completed）。
// 返回空字符串表示不参与数据库去重。
func BuildMessageDedupKey(eventType, method string, metadata json.RawMessage) string {
	normalizedEvent := strings.ToLower(strings.TrimSpace(eventType))
	normalizedMethod := strings.ToLower(strings.TrimSpace(method))
	if !shouldBuildMessageDedupKey(normalizedEvent, normalizedMethod) {
		return ""
	}
	if len(metadata) == 0 {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(metadata, &payload); err != nil {
		return ""
	}

	id := firstNonEmpty(
		lookupNestedString(payload, "turn", "id"),
		lookupNestedString(payload, "msg", "turn_id"),
		lookupNestedString(payload, "turn_id"),
		lookupNestedString(payload, "id"),
		lookupNestedString(payload, "call_id"),
		lookupNestedString(payload, "callId"),
		lookupNestedString(payload, "tool_call_id"),
	)
	if id == "" {
		return ""
	}

	scope := normalizedMethod
	if scope == "" {
		scope = normalizedEvent
	}
	if scope == "" {
		return ""
	}
	return scope + "|" + id
}

func shouldBuildMessageDedupKey(eventType, method string) bool {
	switch method {
	case "turn/completed", "codex/event/task_complete", "dynamic-tool/called", "item/tool/call":
		return true
	}
	switch eventType {
	case "turn_complete", "codex/event/task_complete", "dynamic_tool_call":
		return true
	}
	return false
}

func lookupNestedString(payload map[string]any, path ...string) string {
	var current any = payload
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		next, ok := obj[key]
		if !ok {
			return ""
		}
		current = next
	}
	s, ok := current.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Insert 写入单条消息。
func (s *AgentMessageStore) Insert(ctx context.Context, msg *AgentMessage) error {
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	msg.DedupKey = strings.TrimSpace(msg.DedupKey)
	msg.Metadata = sanitizeMetadata(msg.Metadata)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agent_messages (agent_id, role, event_type, method, content, metadata, created_at, dedup_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (agent_id, dedup_key) WHERE dedup_key <> '' DO NOTHING`,
		msg.AgentID, msg.Role, msg.EventType, msg.Method, msg.Content, msg.Metadata, msg.CreatedAt, msg.DedupKey)
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
	items, err := collectRows[AgentMessage](rows)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Metadata = sanitizeMetadata(items[i].Metadata)
	}
	return items, nil
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

// AgentThreadInfo 线程历史信息 (供 thread/list 使用)。
type AgentThreadInfo struct {
	AgentID string    `json:"agentId"`
	LastAt  time.Time `json:"lastAt"`
}

// ListDistinctAgentIDs 返回所有有消息记录的 agent ID (按最近活跃排序)。
func (s *AgentMessageStore) ListDistinctAgentIDs(ctx context.Context, limit int) ([]AgentThreadInfo, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx,
		`SELECT agent_id, MAX(created_at) AS last_at
		 FROM agent_messages
		 GROUP BY agent_id
		 ORDER BY last_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return collectRows[AgentThreadInfo](rows)
}

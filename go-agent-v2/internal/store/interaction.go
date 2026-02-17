// interaction.go — 交互记录 CRUD (表 agent_interactions, 14 列)。
// Python: agent_ops_store.py create_interaction/list_interactions/review_interaction
package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// InteractionStore 交互记录存储。
type InteractionStore struct{ BaseStore }

// NewInteractionStore 创建交互存储。
func NewInteractionStore(pool *pgxpool.Pool) *InteractionStore { return &InteractionStore{NewBaseStore(pool)} }

const interactionCols = `id, thread_id, parent_id, sender, receiver, msg_type, status,
	requires_review, reviewed_by, review_note, reviewed_at,
	payload, created_at, updated_at`

// Create 创建交互记录 (对应 Python create_interaction)。
func (s *InteractionStore) Create(ctx context.Context, i *Interaction) (*Interaction, error) {
	payloadJSON, _ := json.Marshal(i.Payload)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO agent_interactions (thread_id, parent_id, sender, receiver, msg_type, status,
		   requires_review, payload, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, NOW())
		 RETURNING `+interactionCols,
		i.ThreadID, i.ParentID, i.Sender, i.Receiver, i.MsgType,
		defaultStr(i.Status, "pending"), i.RequiresReview, string(payloadJSON))
	if err != nil {
		return nil, err
	}
	return collectOne[Interaction](rows)
}

// Get 按 ID 查询。
func (s *InteractionStore) Get(ctx context.Context, id int) (*Interaction, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+interactionCols+" FROM agent_interactions WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	return collectOne[Interaction](rows)
}

// List 列表查询 (支持 thread_id / sender / receiver / msg_type / status / keyword)。
func (s *InteractionStore) List(ctx context.Context, threadID, keyword string, limit int) ([]Interaction, error) {
	q := NewQueryBuilder().
		Eq("thread_id", threadID).
		KeywordLike(keyword, "sender", "receiver", "msg_type")
	sql, params := q.Build(
		"SELECT "+interactionCols+" FROM agent_interactions",
		"created_at DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[Interaction](rows)
}

// Review 审批交互记录 (对应 Python review_interaction)。
func (s *InteractionStore) Review(ctx context.Context, id int, status, reviewer, note string) (*Interaction, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE agent_interactions
		 SET status = $1, reviewed_by = $2, review_note = $3, reviewed_at = NOW(), updated_at = NOW()
		 WHERE id = $4
		 RETURNING `+interactionCols,
		status, reviewer, note, id)
	if err != nil {
		return nil, err
	}
	return collectOne[Interaction](rows)
}

// defaultStr 空字符串返回默认值。
func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// bus_pending.go — bus_pending 表存储 (总线降级落盘)。
package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BusPendingMessage 对应 bus_pending 表行。
type BusPendingMessage struct {
	Seq       int64           `json:"seq"`
	Topic     string          `json:"topic"`
	FromID    string          `json:"from_id"`
	ToID      string          `json:"to_id"`
	MsgType   string          `json:"msg_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// BusPendingStore 总线降级存储。
type BusPendingStore struct{ BaseStore }

// NewBusPendingStore 创建。
func NewBusPendingStore(pool *pgxpool.Pool) *BusPendingStore {
	return &BusPendingStore{NewBaseStore(pool)}
}

// Save 保存一条 pending 消息。
func (s *BusPendingStore) Save(ctx context.Context, topic, fromID, toID, msgType string, payload json.RawMessage) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO bus_pending (topic, from_id, to_id, msg_type, payload) VALUES ($1, $2, $3, $4, $5)`,
		topic, fromID, toID, msgType, payload)
	return err
}

// LoadOldest 加载最早的 N 条 pending 消息。
func (s *BusPendingStore) LoadOldest(ctx context.Context, limit int) ([]BusPendingMessage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT seq, topic, from_id, to_id, msg_type, payload, created_at FROM bus_pending ORDER BY seq ASC LIMIT $1`,
		limit)
	if err != nil {
		return nil, err
	}
	return collectRows[BusPendingMessage](rows)
}

// Delete 删除已补发的消息。
func (s *BusPendingStore) Delete(ctx context.Context, seq int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM bus_pending WHERE seq = $1`, seq)
	return err
}

// Count 统计 pending 消息数量。
func (s *BusPendingStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bus_pending`).Scan(&count)
	return count, err
}

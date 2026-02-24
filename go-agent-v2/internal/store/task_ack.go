// task_ack.go — 任务确认 CRUD (表 task_acks, 18 列)。
// Python: agent_ops_store.py save_task_ack / list_task_acks / update_task_ack_status / delete_task_acks
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// TaskAckStore 任务确认存储。
type TaskAckStore struct{ BaseStore }

// NewTaskAckStore 创建。
func NewTaskAckStore(pool *pgxpool.Pool) *TaskAckStore { return &TaskAckStore{NewBaseStore(pool)} }

const taCols = `id, ack_key, title, description, assigned_to, requested_by,
	priority, status, progress, ack_message, result_summary,
	metadata, due_at, acked_at, started_at, finished_at, created_at, updated_at`

// Save 创建或更新 (UPSERT)。
func (s *TaskAckStore) Save(ctx context.Context, a *TaskAck) (*TaskAck, error) {
	metaJSON := mustMarshalJSON(a.Metadata)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO task_acks (ack_key, title, description, assigned_to, requested_by,
		   priority, status, progress, ack_message, result_summary, metadata, due_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12::timestamptz)
		 ON CONFLICT (ack_key) DO UPDATE SET
		   title=EXCLUDED.title, description=EXCLUDED.description,
		   assigned_to=EXCLUDED.assigned_to, requested_by=EXCLUDED.requested_by,
		   priority=EXCLUDED.priority, status=EXCLUDED.status,
		   progress=EXCLUDED.progress, ack_message=EXCLUDED.ack_message,
		   result_summary=EXCLUDED.result_summary, metadata=EXCLUDED.metadata,
		   due_at=EXCLUDED.due_at, updated_at=NOW()
		 RETURNING `+taCols,
		a.AckKey, a.Title, a.Description, a.AssignedTo, a.RequestedBy,
		defaultStr(a.Priority, "normal"), defaultStr(a.Status, "pending"),
		util.ClampInt(a.Progress, 0, 100), a.AckMessage, a.ResultSummary,
		string(metaJSON), a.DueAt)
	if err != nil {
		return nil, err
	}
	return collectOne[TaskAck](rows)
}

// List 列表查询 (对应 Python list_task_acks)。
func (s *TaskAckStore) List(ctx context.Context, keyword, status, priority, assignedTo string, limit int) ([]TaskAck, error) {
	q := NewQueryBuilder().
		Eq("status", status).
		Eq("priority", priority).
		Eq("assigned_to", assignedTo).
		KeywordLike(keyword, "ack_key", "title", "description")
	sql, params := q.Build("SELECT "+taCols+" FROM task_acks", "updated_at DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskAck](rows)
}

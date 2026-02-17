// task_ack.go — 任务确认 CRUD (表 task_acks, 18 列)。
// Python: agent_ops_store.py save_task_ack / list_task_acks / update_task_ack_status / delete_task_acks
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	metaJSON, _ := json.Marshal(a.Metadata)
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

// UpdateStatus 更新 ACK 状态 (对应 Python update_task_ack_status, 含自动时间戳)。
func (s *TaskAckStore) UpdateStatus(ctx context.Context, ackKey, status string, progress *int, ackMessage, resultSummary string) (*TaskAck, error) {
	sets := []string{"status = $1", "updated_at = NOW()"}
	params := []any{status}
	n := 1

	// 自动设置时间戳
	switch status {
	case "acked":
		sets = append(sets, "acked_at = COALESCE(acked_at, NOW())")
	case "in_progress":
		sets = append(sets, "started_at = COALESCE(started_at, NOW())")
	case "done", "failed", "cancelled":
		sets = append(sets, "finished_at = NOW()")
	}

	if progress != nil {
		n++
		sets = append(sets, fmt.Sprintf("progress = $%d", n))
		params = append(params, util.ClampInt(*progress, 0, 100))
	}
	if ackMessage != "" {
		n++
		sets = append(sets, fmt.Sprintf("ack_message = $%d", n))
		params = append(params, ackMessage)
	}
	if resultSummary != "" {
		n++
		sets = append(sets, fmt.Sprintf("result_summary = $%d", n))
		params = append(params, resultSummary)
	}

	n++
	params = append(params, ackKey)
	sql := fmt.Sprintf("UPDATE task_acks SET %s WHERE ack_key = $%d RETURNING %s",
		strings.Join(sets, ", "), n, taCols)

	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectOne[TaskAck](rows)
}

// DeleteBatch 批量删除 (对应 Python delete_task_acks)。
func (s *TaskAckStore) DeleteBatch(ctx context.Context, ackKeys []string) (int64, error) {
	return DeleteBatchByKeys(ctx, s.pool, "task_acks", "ack_key", ackKeys)
}

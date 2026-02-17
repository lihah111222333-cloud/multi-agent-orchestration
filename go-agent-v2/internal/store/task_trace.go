// task_trace.go — 任务追踪 CRUD (对应 Python agent_ops_store.py trace 部分)。
// 增加 StartSpan / FinishSpan 生命周期 (对应 Python start_task_trace_span / finish_task_trace_span)。
package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TaskTraceStore 任务追踪存储。
type TaskTraceStore struct{ BaseStore }

// NewTaskTraceStore 创建任务追踪存储。
func NewTaskTraceStore(pool *pgxpool.Pool) *TaskTraceStore { return &TaskTraceStore{NewBaseStore(pool)} }

const taskTraceCols = `id, trace_id, span_id, parent_span, agent_id, action,
	input, output, status, error, duration_ms, metadata, created_at, updated_at`

// StartSpan 开始跟踪 (对应 Python start_task_trace_span)。
func (s *TaskTraceStore) StartSpan(ctx context.Context, t *TaskTrace) (*TaskTrace, error) {
	inJSON, _ := json.Marshal(t.Input)
	metaJSON, _ := json.Marshal(t.Metadata)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO task_traces (trace_id, span_id, parent_span, agent_id, action,
		   input, status, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, 'running', $7::jsonb, NOW(), NOW())
		 RETURNING `+taskTraceCols,
		t.TraceID, t.SpanID, t.ParentSpan, t.AgentID, t.Action,
		string(inJSON), string(metaJSON))
	if err != nil {
		return nil, err
	}
	return collectOne[TaskTrace](rows)
}

// FinishSpan 完成跟踪 (对应 Python finish_task_trace_span, 自动计算 duration)。
func (s *TaskTraceStore) FinishSpan(ctx context.Context, traceID, spanID, status string, output any, errMsg *string) (*TaskTrace, error) {
	outJSON, _ := json.Marshal(output)
	rows, err := s.pool.Query(ctx,
		`UPDATE task_traces
		 SET status=$1, output=$2::jsonb, error=$3,
		     duration_ms=EXTRACT(EPOCH FROM (NOW()-created_at))::INT * 1000,
		     updated_at=NOW()
		 WHERE trace_id=$4 AND span_id=$5
		 RETURNING `+taskTraceCols,
		status, string(outJSON), errMsg, traceID, spanID)
	if err != nil {
		return nil, err
	}
	return collectOne[TaskTrace](rows)
}

// Create 直接创建完整记录。
func (s *TaskTraceStore) Create(ctx context.Context, t *TaskTrace) (*TaskTrace, error) {
	inJSON, _ := json.Marshal(t.Input)
	outJSON, _ := json.Marshal(t.Output)
	metaJSON, _ := json.Marshal(t.Metadata)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO task_traces (trace_id, span_id, parent_span, agent_id, action,
		   input, output, status, error, duration_ms, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11::jsonb, NOW(), NOW())
		 RETURNING `+taskTraceCols,
		t.TraceID, t.SpanID, t.ParentSpan, t.AgentID, t.Action,
		string(inJSON), string(outJSON), t.Status, t.Error, t.DurationMS, string(metaJSON))
	if err != nil {
		return nil, err
	}
	return collectOne[TaskTrace](rows)
}

// ListByTraceID 按 trace_id 查询 (对应 Python list_task_trace_spans)。
func (s *TaskTraceStore) ListByTraceID(ctx context.Context, traceID string) ([]TaskTrace, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+taskTraceCols+" FROM task_traces WHERE trace_id = $1 ORDER BY created_at", traceID)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskTrace](rows)
}

// List 列表查询 (对应 Python list_task_traces)。
func (s *TaskTraceStore) List(ctx context.Context, agentID, keyword string, since *time.Time, limit int) ([]TaskTrace, error) {
	q := NewQueryBuilder().Eq("agent_id", agentID)
	if since != nil {
		q.n++
		q.where = append(q.where, "created_at >= $"+string(rune('0'+q.n)))
		q.params = append(q.params, *since)
	}
	q.KeywordLike(keyword, "action", "status")
	sql, params := q.Build("SELECT "+taskTraceCols+" FROM task_traces", "created_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskTrace](rows)
}

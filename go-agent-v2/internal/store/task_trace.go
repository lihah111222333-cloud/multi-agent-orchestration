// task_trace.go — 任务追踪 CRUD (对应 Python agent_ops_store.py trace 部分)。
// 增加 StartSpan / FinishSpan 生命周期 (对应 Python start_task_trace_span / finish_task_trace_span)。
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TaskTraceStore 任务追踪存储。
type TaskTraceStore struct{ BaseStore }

// NewTaskTraceStore 创建任务追踪存储。
func NewTaskTraceStore(pool *pgxpool.Pool) *TaskTraceStore {
	return &TaskTraceStore{NewBaseStore(pool)}
}

const taskTraceCols = `id, trace_id, span_id, parent_span_id, span_name, component,
	status, input_payload, output_payload, error_text, metadata,
	started_at, finished_at, duration_ms`

// Deprecated: StartSpan 无外部调用者。
func (s *TaskTraceStore) StartSpan(ctx context.Context, t *TaskTrace) (*TaskTrace, error) {
	inJSON := mustMarshalJSON(t.Input)
	metaJSON := mustMarshalJSON(t.Metadata)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO task_traces (trace_id, span_id, parent_span_id, span_name, component,
		   input_payload, status, metadata, started_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, 'running', $7::jsonb, NOW())
		 RETURNING `+taskTraceCols,
		t.TraceID, t.SpanID, t.ParentSpanID, t.SpanName, t.Component,
		string(inJSON), string(metaJSON))
	if err != nil {
		return nil, err
	}
	return collectOne[TaskTrace](rows)
}

// Deprecated: FinishSpan 无外部调用者。
func (s *TaskTraceStore) FinishSpan(ctx context.Context, traceID, spanID, status string, output any, errText string) (*TaskTrace, error) {
	outJSON := mustMarshalJSON(output)
	rows, err := s.pool.Query(ctx,
		`UPDATE task_traces
		 SET status=$1, output_payload=$2::jsonb, error_text=$3,
		     duration_ms=EXTRACT(EPOCH FROM (NOW()-started_at))::INT * 1000,
		     finished_at=NOW()
		 WHERE trace_id=$4 AND span_id=$5
		 RETURNING `+taskTraceCols,
		status, string(outJSON), errText, traceID, spanID)
	if err != nil {
		return nil, err
	}
	return collectOne[TaskTrace](rows)
}

// Create 直接创建完整记录。
func (s *TaskTraceStore) Create(ctx context.Context, t *TaskTrace) (*TaskTrace, error) {
	inJSON := mustMarshalJSON(t.Input)
	outJSON := mustMarshalJSON(t.Output)
	metaJSON := mustMarshalJSON(t.Metadata)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO task_traces (trace_id, span_id, parent_span_id, span_name, component,
		   input_payload, output_payload, status, error_text, duration_ms, metadata, started_at, finished_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11::jsonb, NOW(), NULL)
		 RETURNING `+taskTraceCols,
		t.TraceID, t.SpanID, t.ParentSpanID, t.SpanName, t.Component,
		string(inJSON), string(outJSON), t.Status, t.ErrorText, t.DurationMS, string(metaJSON))
	if err != nil {
		return nil, err
	}
	return collectOne[TaskTrace](rows)
}

// Deprecated: ListByTraceID 无外部调用者。
func (s *TaskTraceStore) ListByTraceID(ctx context.Context, traceID string) ([]TaskTrace, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+taskTraceCols+" FROM task_traces WHERE trace_id = $1 ORDER BY started_at", traceID)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskTrace](rows)
}

// List 列表查询 (对应 Python list_task_traces)。
func (s *TaskTraceStore) List(ctx context.Context, agentID, keyword string, since *time.Time, limit int) ([]TaskTrace, error) {
	q := NewQueryBuilder().Eq("component", agentID)
	if since != nil {
		q.n++
		q.where = append(q.where, "started_at >= $"+string(rune('0'+q.n)))
		q.params = append(q.params, *since)
	}
	q.KeywordLike(keyword, "span_name", "status")
	sql, params := q.Build("SELECT "+taskTraceCols+" FROM task_traces", "started_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskTrace](rows)
}

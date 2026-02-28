package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskTraceStore struct{ BaseStore }

func NewTaskTraceStore(pool *pgxpool.Pool) *TaskTraceStore {
	return &TaskTraceStore{NewBaseStore(pool)}
}

const taskTraceCols = `id, trace_id, span_id, parent_span_id, span_name, component,
	status, input_payload, output_payload, error_text, metadata,
	started_at, finished_at, duration_ms`

func (s *TaskTraceStore) Create(ctx context.Context, t *TaskTrace) (*TaskTrace, error) {
	inJSON, outJSON, metaJSON := mustMarshalJSON(t.Input), mustMarshalJSON(t.Output), mustMarshalJSON(t.Metadata)
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

func (s *TaskTraceStore) List(ctx context.Context, agentID, keyword string, since *time.Time, limit int) ([]TaskTrace, error) {
	q := NewQueryBuilder().Eq("component", agentID)
	if since != nil {
		q.Gte("started_at", *since)
	}
	q.KeywordLike(keyword, "span_name", "status")
	sql, params := q.Build("SELECT "+taskTraceCols+" FROM task_traces", "started_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[TaskTrace](rows)
}

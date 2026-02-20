// audit_log.go — 审计日志 CRUD (对应 Python audit_log.py)。
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditLogStore 审计日志存储。
type AuditLogStore struct{ BaseStore }

// NewAuditLogStore 创建审计日志存储。
func NewAuditLogStore(pool *pgxpool.Pool) *AuditLogStore { return &AuditLogStore{NewBaseStore(pool)} }

// Append 追加审计事件。
func (s *AuditLogStore) Append(ctx context.Context, e *AuditEvent) error {
	extraJSON := mustMarshalJSON(e.Extra)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_events (ts, event_type, action, result, actor, target, detail, level, extra)
		 VALUES (NOW(), $1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		e.EventType, e.Action, e.Result, e.Actor, e.Target, e.Detail, e.Level, string(extraJSON))
	return err
}

// List 查询审计日志 (支持 event_type + action + actor + keyword 过滤)。
func (s *AuditLogStore) List(ctx context.Context, eventType, action, actor, keyword string, limit int) ([]AuditEvent, error) {
	q := NewQueryBuilder().
		Eq("event_type", eventType).
		Eq("action", action).
		Eq("actor", actor).
		KeywordLike(keyword, "event_type", "action", "result", "actor", "target", "detail")
	sql, params := q.Build(
		"SELECT ts, event_type, action, result, actor, target, detail, level, extra FROM audit_events",
		"ts DESC, id DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[AuditEvent](rows)
}

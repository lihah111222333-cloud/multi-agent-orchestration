// agent_status.go — Agent 状态存储 CRUD (对应 Python agent_status_store.py)。
// 增加输入验证 (M4) 和 status 过滤 (H12)。
package store

import (
	"context"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// AgentStatusStore Agent 状态存储。
type AgentStatusStore struct{ BaseStore }

// NewAgentStatusStore 创建。
func NewAgentStatusStore(pool *pgxpool.Pool) *AgentStatusStore {
	return &AgentStatusStore{NewBaseStore(pool)}
}

var agentIDRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var validStatuses = map[string]bool{
	"idle": true, "running": true, "stagnant": true,
	"error": true, "stopped": true, "unknown": true,
}

const asCols = "agent_id, agent_name, session_id, status, stagnant_sec, error, output_tail, created_at, updated_at"

// validateAgentID 验证 agent_id 格式 (对应 Python _normalize_agent_id)。
func validateAgentID(id string) error {
	if id == "" || !agentIDRe.MatchString(id) {
		return apperrors.Newf("validateAgentID", "agent_id 格式非法: %q", id)
	}
	return nil
}

// normalizeOutputTail 截断输出尾部 (对应 Python _normalize_output_tail, 最多 50 行)。
func normalizeOutputTail(tail any) any {
	if tail == nil {
		return []string{}
	}
	if lines, ok := tail.([]string); ok {
		if len(lines) > 50 {
			return lines[len(lines)-50:]
		}
		return lines
	}
	return tail
}

// Upsert 更新或插入 Agent 状态 (含输入验证)。
func (s *AgentStatusStore) Upsert(ctx context.Context, a *AgentStatus) (*AgentStatus, error) {
	if err := validateAgentID(a.AgentID); err != nil {
		return nil, err
	}
	if !validStatuses[a.Status] {
		a.Status = "unknown"
	}
	if a.StagnantSec < 0 {
		a.StagnantSec = 0
	}
	a.OutputTail = normalizeOutputTail(a.OutputTail)

	outputJSON := mustMarshalJSON(a.OutputTail)
	rows, err := s.pool.Query(ctx,
		`INSERT INTO agent_status (agent_id, agent_name, session_id, status, stagnant_sec, error, output_tail, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, NOW(), NOW())
		 ON CONFLICT (agent_id) DO UPDATE SET
		   agent_name=EXCLUDED.agent_name, session_id=EXCLUDED.session_id, status=EXCLUDED.status,
		   stagnant_sec=EXCLUDED.stagnant_sec, error=EXCLUDED.error, output_tail=EXCLUDED.output_tail, updated_at=NOW()
		 RETURNING `+asCols,
		a.AgentID, a.AgentName, a.SessionID, a.Status, a.StagnantSec, a.Error, string(outputJSON))
	if err != nil {
		return nil, err
	}
	return collectOne[AgentStatus](rows)
}

// Get 按 agent_id 查询。
func (s *AgentStatusStore) Get(ctx context.Context, agentID string) (*AgentStatus, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+asCols+" FROM agent_status WHERE agent_id = $1", agentID)
	if err != nil {
		return nil, err
	}
	return collectOne[AgentStatus](rows)
}

// List 查询 Agent 状态 (支持 status 过滤, 对应 Python query_agent_status)。
func (s *AgentStatusStore) List(ctx context.Context, status string) ([]AgentStatus, error) {
	q := NewQueryBuilder().Eq("status", status)
	sql, params := q.Build("SELECT "+asCols+" FROM agent_status", "updated_at DESC", 500)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[AgentStatus](rows)
}

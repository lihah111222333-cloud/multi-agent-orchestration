// agent_status.go stores agent runtime status records.
package store

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type AgentStatusStore struct{ BaseStore }

func NewAgentStatusStore(pool *pgxpool.Pool) *AgentStatusStore {
	return &AgentStatusStore{NewBaseStore(pool)}
}

var agentIDRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var validStatuses = map[string]bool{"idle": true, "running": true, "stagnant": true, "error": true, "stopped": true, "unknown": true}

const asCols = "agent_id, agent_name, session_id, status, stagnant_sec, error, output_tail, created_at, updated_at"

func validateAgentID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || !agentIDRe.MatchString(id) {
		return apperrors.Newf("validateAgentID", "agent_id 格式非法: %q", id)
	}
	return nil
}

func normalizeOutputTail(tail any) any {
	switch lines := tail.(type) {
	case nil:
		return []string{}
	case []string:
		if len(lines) > 50 {
			return lines[len(lines)-50:]
		}
		return lines
	default:
		return tail
	}
}

func (s *AgentStatusStore) Upsert(ctx context.Context, a *AgentStatus) (*AgentStatus, error) {
	if a == nil {
		return nil, apperrors.New("upsertAgentStatus", "agent status is required")
	}
	a.AgentID = strings.TrimSpace(a.AgentID)
	if err := validateAgentID(a.AgentID); err != nil {
		return nil, err
	}
	a.Status = strings.ToLower(strings.TrimSpace(a.Status))
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

func (s *AgentStatusStore) Get(ctx context.Context, agentID string) (*AgentStatus, error) {
	agentID = strings.TrimSpace(agentID)
	rows, err := s.pool.Query(ctx,
		"SELECT "+asCols+" FROM agent_status WHERE agent_id = $1", agentID)
	if err != nil {
		return nil, err
	}
	return collectOne[AgentStatus](rows)
}

func (s *AgentStatusStore) List(ctx context.Context, status string) ([]AgentStatus, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	q := NewQueryBuilder().Eq("status", status)
	sql, params := q.Build("SELECT "+asCols+" FROM agent_status", "updated_at DESC", 500)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[AgentStatus](rows)
}

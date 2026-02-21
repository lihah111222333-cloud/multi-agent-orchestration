// topology_approval.go — 拓扑审批 CRUD (对应 Python topology_approval.py)。
package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// TopologyApprovalStore 拓扑审批存储。
type TopologyApprovalStore struct{ BaseStore }

// NewTopologyApprovalStore 创建拓扑审批存储。
func NewTopologyApprovalStore(pool *pgxpool.Pool) *TopologyApprovalStore {
	return &TopologyApprovalStore{NewBaseStore(pool)}
}

// Create 创建审批请求。
func (s *TopologyApprovalStore) Create(ctx context.Context, a *TopologyApproval) (*TopologyApproval, error) {
	proposalJSON, err := json.Marshal(a.ProposalJSON)
	if err != nil {
		return nil, pkgerr.Wrap(err, "TopologyApproval.Create", "marshal proposal")
	}
	rows, err := s.pool.Query(ctx,
		`INSERT INTO topology_approvals (proposal_hash, proposal_json, status, requested_by, expires_at, created_at, updated_at)
		 VALUES ($1, $2::jsonb, 'pending', $3, $4, NOW(), NOW())
		 RETURNING id, proposal_hash, proposal_json, status, requested_by, approved_by, rejected_by, expires_at, created_at, updated_at`,
		a.ProposalHash, string(proposalJSON), a.RequestedBy, a.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return collectOne[TopologyApproval](rows)
}

// Approve 批准审批。
func (s *TopologyApprovalStore) Approve(ctx context.Context, id int, approvedBy string) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE topology_approvals SET status='approved', approved_by=$1, updated_at=NOW() WHERE id=$2 AND status='pending'",
		approvedBy, id)
	return err
}

// Reject 拒绝审批。
func (s *TopologyApprovalStore) Reject(ctx context.Context, id int, rejectedBy string) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE topology_approvals SET status='rejected', rejected_by=$1, updated_at=NOW() WHERE id=$2 AND status='pending'",
		rejectedBy, id)
	return err
}

// GetPending 查询待审批。
func (s *TopologyApprovalStore) GetPending(ctx context.Context) ([]TopologyApproval, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, proposal_hash, proposal_json, status, requested_by, approved_by, rejected_by, expires_at, created_at, updated_at
		 FROM topology_approvals WHERE status='pending' AND expires_at > NOW() ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	return collectRows[TopologyApproval](rows)
}

// Deprecated: ListRecent 无外部调用者。
func (s *TopologyApprovalStore) ListRecent(ctx context.Context, limit int) ([]TopologyApproval, error) {
	q := NewQueryBuilder()
	sql, params := q.Build(
		`SELECT id, proposal_hash, proposal_json, status, requested_by, approved_by, rejected_by, expires_at, created_at, updated_at
		 FROM topology_approvals`, "created_at DESC", limit)
	rows, err := s.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	return collectRows[TopologyApproval](rows)
}

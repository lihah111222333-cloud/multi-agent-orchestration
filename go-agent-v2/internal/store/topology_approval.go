package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type TopologyApprovalStore struct{ BaseStore }

func NewTopologyApprovalStore(pool *pgxpool.Pool) *TopologyApprovalStore {
	return &TopologyApprovalStore{NewBaseStore(pool)}
}

const topologyApprovalCols = `id, proposal_hash, proposal_json, status, requested_by,
	approved_by, rejected_by, expires_at, created_at, updated_at`

func (s *TopologyApprovalStore) Create(ctx context.Context, a *TopologyApproval) (*TopologyApproval, error) {
	proposalJSON, err := json.Marshal(a.ProposalJSON)
	if err != nil {
		return nil, pkgerr.Wrap(err, "TopologyApproval.Create", "marshal proposal")
	}
	rows, err := s.pool.Query(ctx,
		`INSERT INTO topology_approvals (proposal_hash, proposal_json, status, requested_by, expires_at, created_at, updated_at)
		 VALUES ($1, $2::jsonb, 'pending', $3, $4, NOW(), NOW())
		 RETURNING `+topologyApprovalCols,
		a.ProposalHash, string(proposalJSON), a.RequestedBy, a.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return collectOne[TopologyApproval](rows)
}

func (s *TopologyApprovalStore) Approve(ctx context.Context, id int, approvedBy string) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE topology_approvals SET status='approved', approved_by=$1, updated_at=NOW() WHERE id=$2 AND status='pending'",
		approvedBy, id)
	return err
}

func (s *TopologyApprovalStore) Reject(ctx context.Context, id int, rejectedBy string) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE topology_approvals SET status='rejected', rejected_by=$1, updated_at=NOW() WHERE id=$2 AND status='pending'",
		rejectedBy, id)
	return err
}

func (s *TopologyApprovalStore) GetPending(ctx context.Context) ([]TopologyApproval, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+topologyApprovalCols+`
		 FROM topology_approvals WHERE status='pending' AND expires_at > NOW() ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	return collectRows[TopologyApproval](rows)
}

// agent_codex_binding.go stores 1:1 agent_id <-> codex_thread_id bindings.
package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

type AgentCodexBinding struct {
	AgentID       string `json:"agent_id"`
	CodexThreadID string `json:"codex_thread_id"`
	RolloutPath   string `json:"rollout_path,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

type AgentCodexBindingStore struct{ BaseStore }

func NewAgentCodexBindingStore(pool *pgxpool.Pool) *AgentCodexBindingStore {
	return &AgentCodexBindingStore{NewBaseStore(pool)}
}

const acbCols = "agent_id, codex_thread_id, rollout_path, created_at, updated_at"

func (s *AgentCodexBindingStore) Bind(ctx context.Context, agentID, codexThreadID, rolloutPath string) error {
	agentID, codexThreadID, rolloutPath = strings.TrimSpace(agentID), strings.TrimSpace(codexThreadID), strings.TrimSpace(rolloutPath)
	if agentID == "" || codexThreadID == "" {
		return fmt.Errorf("bind requires non-empty agent_id and codex_thread_id")
	}

	existing, err := s.FindByAgentID(ctx, agentID)
	if err != nil {
		return err
	}
	if existing == nil {
		now := time.Now().Unix()
		_, err = s.pool.Exec(ctx,
			`INSERT INTO agent_codex_binding (agent_id, codex_thread_id, rollout_path, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5)`,
			agentID, codexThreadID, rolloutPath, now, now)
		return err
	}
	switch existingThreadID := strings.TrimSpace(existing.CodexThreadID); {
	case existingThreadID != codexThreadID:
		return fmt.Errorf("immutable binding violation: agent %q already bound to %q, cannot bind to %q",
			agentID, existingThreadID, codexThreadID)
	case rolloutPath == "" || rolloutPath == strings.TrimSpace(existing.RolloutPath):
		return nil
	default:
		_, err = s.pool.Exec(ctx,
			`UPDATE agent_codex_binding
		 SET rollout_path = $1, updated_at = $2
		 WHERE agent_id = $3 AND codex_thread_id = $4`,
			rolloutPath, time.Now().Unix(), agentID, codexThreadID)
		return err
	}
}

func (s *AgentCodexBindingStore) Unbind(ctx context.Context, agentID string) error {
	agentID = strings.TrimSpace(agentID)
	_, err := s.pool.Exec(ctx, "DELETE FROM agent_codex_binding WHERE agent_id = $1", agentID)
	return err
}

func (s *AgentCodexBindingStore) FindByAgentID(ctx context.Context, agentID string) (*AgentCodexBinding, error) {
	agentID = strings.TrimSpace(agentID)
	rows, err := s.pool.Query(ctx,
		"SELECT "+acbCols+" FROM agent_codex_binding WHERE agent_id = $1", agentID)
	if err != nil {
		return nil, err
	}
	return collectOne[AgentCodexBinding](rows)
}

func (s *AgentCodexBindingStore) FindBindingByAgentID(ctx context.Context, agentID string) (*agentcore.Binding, error) {
	binding, err := s.FindByAgentID(ctx, agentID)
	if err != nil || binding == nil {
		return nil, err
	}
	return &agentcore.Binding{CodexThreadID: strings.TrimSpace(binding.CodexThreadID)}, nil
}

func (s *AgentCodexBindingStore) ListAll(ctx context.Context) ([]AgentCodexBinding, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+acbCols+" FROM agent_codex_binding ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	return collectRows[AgentCodexBinding](rows)
}

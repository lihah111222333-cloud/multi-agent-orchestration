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

// AgentCodexBinding is a 1:1 binding record.
type AgentCodexBinding struct {
	AgentID       string `json:"agent_id"`
	CodexThreadID string `json:"codex_thread_id"`
	RolloutPath   string `json:"rollout_path,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

// AgentCodexBindingStore reads/writes agent_codex_binding.
type AgentCodexBindingStore struct{ BaseStore }

func NewAgentCodexBindingStore(pool *pgxpool.Pool) *AgentCodexBindingStore {
	return &AgentCodexBindingStore{NewBaseStore(pool)}
}

const acbCols = "agent_id, codex_thread_id, rollout_path, created_at, updated_at"

// Bind creates a 1:1 binding and treats codex_thread_id as immutable.
func (s *AgentCodexBindingStore) Bind(ctx context.Context, agentID, codexThreadID, rolloutPath string) error {
	agentID = strings.TrimSpace(agentID)
	codexThreadID = strings.TrimSpace(codexThreadID)
	rolloutPath = strings.TrimSpace(rolloutPath)
	if agentID == "" || codexThreadID == "" {
		return fmt.Errorf("bind requires non-empty agent_id and codex_thread_id")
	}

	existing, err := s.FindByAgentID(ctx, agentID)
	if err != nil {
		return err
	}
	if existing != nil {
		if existingThreadID := strings.TrimSpace(existing.CodexThreadID); existingThreadID != codexThreadID {
			return fmt.Errorf("immutable binding violation: agent %q already bound to %q, cannot bind to %q",
				agentID, existingThreadID, codexThreadID)
		}
		if rolloutPath == "" || rolloutPath == strings.TrimSpace(existing.RolloutPath) {
			return nil
		}
		_, err = s.pool.Exec(ctx,
			`UPDATE agent_codex_binding
			 SET rollout_path = $1, updated_at = $2
			 WHERE agent_id = $3 AND codex_thread_id = $4`,
			rolloutPath, time.Now().Unix(), agentID, codexThreadID)
		return err
	}

	now := time.Now().Unix()
	_, err = s.pool.Exec(ctx,
		`INSERT INTO agent_codex_binding (agent_id, codex_thread_id, rollout_path, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		agentID, codexThreadID, rolloutPath, now, now)
	return err
}

func (s *AgentCodexBindingStore) Unbind(ctx context.Context, agentID string) error {
	agentID = strings.TrimSpace(agentID)
	_, err := s.pool.Exec(ctx,
		"DELETE FROM agent_codex_binding WHERE agent_id = $1", agentID)
	return err
}

// FindByAgentID loads the binding for an agent_id.
func (s *AgentCodexBindingStore) FindByAgentID(ctx context.Context, agentID string) (*AgentCodexBinding, error) {
	agentID = strings.TrimSpace(agentID)
	rows, err := s.pool.Query(ctx,
		"SELECT "+acbCols+" FROM agent_codex_binding WHERE agent_id = $1", agentID)
	if err != nil {
		return nil, err
	}
	return collectOne[AgentCodexBinding](rows)
}

// FindBindingByAgentID returns the lightweight binding contract used by runtime services.
func (s *AgentCodexBindingStore) FindBindingByAgentID(ctx context.Context, agentID string) (*agentcore.Binding, error) {
	binding, err := s.FindByAgentID(ctx, agentID)
	if binding == nil || err != nil {
		return nil, err
	}
	return &agentcore.Binding{CodexThreadID: strings.TrimSpace(binding.CodexThreadID)}, nil
}

// ListAll returns all bindings.
func (s *AgentCodexBindingStore) ListAll(ctx context.Context) ([]AgentCodexBinding, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+acbCols+" FROM agent_codex_binding ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	return collectRows[AgentCodexBinding](rows)
}

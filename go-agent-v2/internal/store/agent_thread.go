package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/internal/discovery"
)

type AgentThread struct {
	ThreadID      string `json:"thread_id"`
	Prompt        string `json:"prompt"`
	Model         string `json:"model"`
	Cwd           string `json:"cwd"`
	Status        string `json:"status"`
	Port          int    `json:"port"`
	PID           int    `json:"pid"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	FinishedAt    *int64 `json:"finished_at,omitempty"`
	LastEventType string `json:"last_event_type,omitempty"`
	ErrorMessage  string `json:"error_message,omitempty"`
}

type AgentThreadStore struct{ BaseStore }

var _ discovery.Discoverer = (*AgentThreadStore)(nil)

func NewAgentThreadStore(pool *pgxpool.Pool) *AgentThreadStore {
	return &AgentThreadStore{NewBaseStore(pool)}
}

const atCols = "thread_id, prompt, model, cwd, status, port, pid, created_at, updated_at, finished_at, last_event_type, error_message"

func (s *AgentThreadStore) FindByPort(ctx context.Context, port int) (*AgentThread, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+atCols+" FROM agent_threads WHERE port = $1 AND status = 'running' ORDER BY updated_at DESC LIMIT 1",
		port)
	if err != nil {
		return nil, err
	}
	return collectOne[AgentThread](rows)
}

func (s *AgentThreadStore) ListRunning(ctx context.Context) ([]discovery.RunningAgent, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT thread_id, port, pid, status FROM agent_threads WHERE status = 'running' ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	return collectRows[discovery.RunningAgent](rows)
}

func (s *AgentThreadStore) ListRunningFull(ctx context.Context) ([]AgentThread, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+atCols+" FROM agent_threads WHERE status = 'running' ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	return collectRows[AgentThread](rows)
}

func (s *AgentThreadStore) Delete(ctx context.Context, threadID string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM agent_threads WHERE thread_id=$1", threadID)
	return err
}

func (s *AgentThreadStore) Upsert(ctx context.Context, t AgentThread) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agent_threads (thread_id, prompt, model, cwd, status, port, pid, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (thread_id) DO UPDATE SET status=$5, cwd=$4, updated_at=$9`,
		t.ThreadID, t.Prompt, t.Model, t.Cwd, t.Status, t.Port, t.PID,
		t.CreatedAt, t.UpdatedAt)
	return err
}

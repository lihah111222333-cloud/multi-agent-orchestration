// agent_thread.go — agent_threads 表 CRUD (Codex 线程注册/发现)。
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentThread codex http-api 线程注册信息。
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

// AgentThreadStore agent_threads 存储。
type AgentThreadStore struct{ BaseStore }

// NewAgentThreadStore 创建。
func NewAgentThreadStore(pool *pgxpool.Pool) *AgentThreadStore {
	return &AgentThreadStore{NewBaseStore(pool)}
}

const atCols = "thread_id, prompt, model, cwd, status, port, pid, created_at, updated_at, finished_at, last_event_type, error_message"

// Register 注册线程 (codex 启动后调用)。
func (s *AgentThreadStore) Register(ctx context.Context, t *AgentThread) error {
	now := time.Now().Unix()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.Status == "" {
		t.Status = "running"
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO agent_threads (thread_id, prompt, model, cwd, status, port, pid, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (thread_id) DO UPDATE SET
		   status=EXCLUDED.status, port=EXCLUDED.port, pid=EXCLUDED.pid, updated_at=EXCLUDED.updated_at`,
		t.ThreadID, t.Prompt, t.Model, t.Cwd, t.Status, t.Port, t.PID, t.CreatedAt, t.UpdatedAt)
	return err
}

// FindByPort 按端口查找运行中的线程。
func (s *AgentThreadStore) FindByPort(ctx context.Context, port int) (*AgentThread, error) {
	return s.findRunning(ctx, "port", port)
}

// FindByPID 按 PID 查找运行中的线程。
func (s *AgentThreadStore) FindByPID(ctx context.Context, pid int) (*AgentThread, error) {
	return s.findRunning(ctx, "pid", pid)
}

// findRunning 按指定列查找运行中的线程 (内部 DRY)。
func (s *AgentThreadStore) findRunning(ctx context.Context, col string, val any) (*AgentThread, error) {
	sql := "SELECT " + atCols + " FROM agent_threads WHERE " + col + " = $1 AND status = 'running' ORDER BY updated_at DESC LIMIT 1"
	rows, err := s.pool.Query(ctx, sql, val)
	if err != nil {
		return nil, err
	}
	return collectOne[AgentThread](rows)
}

// ListRunning 列出所有运行中的线程。
func (s *AgentThreadStore) ListRunning(ctx context.Context) ([]AgentThread, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+atCols+" FROM agent_threads WHERE status = 'running' ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	return collectRows[AgentThread](rows)
}

// UpdateStatus 更新线程状态 (参数化 SQL, 无注入风险)。
func (s *AgentThreadStore) UpdateStatus(ctx context.Context, threadID, status string, errMsg string) error {
	now := time.Now().Unix()
	if status == "stopped" || status == "error" {
		_, err := s.pool.Exec(ctx,
			`UPDATE agent_threads SET status=$1, updated_at=$2, error_message=$3, finished_at=$4 WHERE thread_id=$5`,
			status, now, errMsg, now, threadID)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE agent_threads SET status=$1, updated_at=$2, error_message=$3 WHERE thread_id=$4`,
		status, now, errMsg, threadID)
	return err
}

// UpdateLastEvent 更新最后事件类型。
func (s *AgentThreadStore) UpdateLastEvent(ctx context.Context, threadID, eventType string) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE agent_threads SET last_event_type=$1, updated_at=$2 WHERE thread_id=$3",
		eventType, time.Now().Unix(), threadID)
	return err
}

// Delete 删除线程记录。
func (s *AgentThreadStore) Delete(ctx context.Context, threadID string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM agent_threads WHERE thread_id=$1", threadID)
	return err
}

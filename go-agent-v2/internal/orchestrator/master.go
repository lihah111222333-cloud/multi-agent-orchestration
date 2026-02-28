package orchestrator

import (
	"context"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type State string

const (
	StateIdle        State = "idle"
	StateDispatching State = "dispatching"
	StateWaiting     State = "waiting"
	StateCollecting  State = "collecting"
	StateCompleted   State = "completed"
	StateError       State = "error"
)

type Master struct {
	state     State
	taskTrace *store.TaskTraceStore
	taskDAG   *store.TaskDAGStore
	taskAck   *store.TaskAckStore
	gateways  []*Gateway
}

func NewMaster(traces *store.TaskTraceStore, dag *store.TaskDAGStore, ack *store.TaskAckStore) *Master {
	return &Master{
		state:     StateIdle,
		taskTrace: traces,
		taskDAG:   dag,
		taskAck:   ack,
	}
}

func (m *Master) AddGateway(gw *Gateway) { m.gateways = append(m.gateways, gw) }

func (m *Master) Run(ctx context.Context) error {
	logger.Info("orchestrator started", "gateways", len(m.gateways))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("orchestrator shutting down")
			logger.Info("orchestrator shutdown completed", "gateways", len(m.gateways))
			return nil
		case <-ticker.C:
			if err := m.tick(); err != nil {
				logger.Error("orchestrator tick error", logger.FieldStatus, m.state, logger.FieldError, err, logger.FieldDecision, "transition_to_error")
				m.state = StateError
			}
		}
	}
}

func (m *Master) tick() error {
	if m.state == StateCompleted || m.state == StateError {
		m.state = StateIdle
	}
	return nil
}

type Gateway struct {
	ID   string
	Name string
}

func (g *Gateway) Execute(ctx context.Context, task string) (string, error) {
	logger.Info("gateway executing", logger.FieldGatewayID, g.ID, "task", task)
	// TODO: 实现 Gateway 任务分发逻辑
	return "dispatched", nil
}

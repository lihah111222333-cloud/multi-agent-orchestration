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
			logger.Info("orchestrator shutdown completed", "gateways", len(m.gateways))
			return nil
		case <-ticker.C:
			m.tick()
		}
	}
}

func (m *Master) tick() {
	if m.state == StateCompleted || m.state == StateError {
		m.state = StateIdle
	}
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

// Package orchestrator 提供主编排器 (对应 Python master.py)。
//
// 使用 Go for-select 状态机替代 LangGraph StateGraph。
package orchestrator

import (
	"context"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// State 编排状态。
type State string

const (
	StateIdle        State = "idle"
	StateDispatching State = "dispatching"
	StateWaiting     State = "waiting"
	StateCollecting  State = "collecting"
	StateCompleted   State = "completed"
	StateError       State = "error"
)

// Master 主编排器。
type Master struct {
	state     State
	taskTrace *store.TaskTraceStore
	taskDAG   *store.TaskDAGStore
	taskAck   *store.TaskAckStore
	gateways  []*Gateway
}

// NewMaster 创建主编排器。
func NewMaster(traces *store.TaskTraceStore, dag *store.TaskDAGStore, ack *store.TaskAckStore) *Master {
	return &Master{
		state:     StateIdle,
		taskTrace: traces,
		taskDAG:   dag,
		taskAck:   ack,
	}
}

// AddGateway 添加 Gateway。
func (m *Master) AddGateway(gw *Gateway) { m.gateways = append(m.gateways, gw) }

// Run 执行编排循环 (for-select 状态机)。
func (m *Master) Run(ctx context.Context) error {
	logger.Infow("orchestrator started", "gateways", len(m.gateways))

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("orchestrator shutting down")
			return nil
		case <-ticker.C:
			if err := m.tick(ctx); err != nil {
				logger.Errorw("orchestrator tick error", logger.FieldStatus, m.state, logger.FieldError, err)
				m.state = StateError
			}
		}
	}
}

// tick 单次状态转换。
func (m *Master) tick(ctx context.Context) error {
	switch m.state {
	case StateIdle:
		// 检查是否有新任务
		return nil
	case StateDispatching:
		// 向 Gateway 分发任务
		return nil
	case StateWaiting:
		// 等待 Agent ACK
		return nil
	case StateCollecting:
		// 收集执行结果
		return nil
	case StateCompleted:
		m.state = StateIdle
		return nil
	case StateError:
		m.state = StateIdle
		return nil
	}
	return nil
}

// ========================================
// Gateway — 单个 Gateway 执行器 (原 gateway.go)
// ========================================

// Gateway 单个 Gateway 执行器。
type Gateway struct {
	ID   string
	Name string
}

// Execute 执行任务分发。
func (g *Gateway) Execute(ctx context.Context, task string) (string, error) {
	logger.Infow("gateway executing", logger.FieldGatewayID, g.ID, "task", task)
	// TODO: 实现 Gateway 任务分发逻辑
	return "dispatched", nil
}

package orchestrator

import (
	"context"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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

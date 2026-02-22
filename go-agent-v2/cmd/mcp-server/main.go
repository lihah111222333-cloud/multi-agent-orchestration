// cmd/mcp-server — MCP 服务器入口。
package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/mcp"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := config.Load()
	logger.Init(cfg.LogLevel)

	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		logger.Fatal("database init failed", logger.FieldError, err)
	}
	defer pool.Close()

	stores := &mcp.Stores{
		Interaction:      store.NewInteractionStore(pool),
		TaskTrace:        store.NewTaskTraceStore(pool),
		PromptTemplate:   store.NewPromptTemplateStore(pool),
		CommandCard:      store.NewCommandCardStore(pool),
		AuditLog:         store.NewAuditLogStore(pool),
		SharedFile:       store.NewSharedFileStore(pool),
		AgentStatus:      store.NewAgentStatusStore(pool),
		TopologyApproval: store.NewTopologyApprovalStore(pool),
		DBQuery:          store.NewDBQueryStore(pool),
	}

	s := mcp.NewServer(stores)
	if err := s.Start(ctx); err != nil {
		logger.Fatal("MCP server failed", logger.FieldError, err)
	}
}

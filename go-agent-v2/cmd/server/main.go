package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/dashboard"
	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/monitor"
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
	logger.AttachDBHandler(pool)
	defer logger.ShutdownDBHandler()

	if err := database.Migrate(ctx, pool, "./migrations"); err != nil {
		logger.Fatal("migration failed", logger.FieldError, err)
	}

	stores := &dashboard.Stores{
		Interaction:      store.NewInteractionStore(pool),
		TaskTrace:        store.NewTaskTraceStore(pool),
		PromptTemplate:   store.NewPromptTemplateStore(pool),
		CommandCard:      store.NewCommandCardStore(pool),
		AuditLog:         store.NewAuditLogStore(pool),
		SystemLog:        store.NewSystemLogStore(pool),
		AILog:            store.NewAILogStore(pool),
		BusLog:           store.NewBusLogStore(pool),
		SharedFile:       store.NewSharedFileStore(pool),
		AgentStatus:      store.NewAgentStatusStore(pool),
		TopologyApproval: store.NewTopologyApprovalStore(pool),
		DBQuery:          store.NewDBQueryStore(pool),
	}

	srv := dashboard.NewServer(stores, cfg)

	patrol := monitor.NewPatrol(stores.AgentStatus, srv)
	patrol.Start(ctx)

	if err := srv.ListenAndServe(ctx, ":8080"); err != nil {
		logger.Fatal("server failed", logger.FieldError, err)
	}
}

// cmd/server — Dashboard + 编排器主入口。
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
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := config.Load()
	logger.Init(cfg.LogLevel)

	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		logger.Fatal("database init failed", logger.Any(logger.FieldError, err))
	}
	defer pool.Close()
	logger.AttachDBHandler(pool)
	defer logger.ShutdownDBHandler()

	if err := database.Migrate(ctx, pool, "./migrations"); err != nil {
		logger.Fatal("migration failed", logger.Any(logger.FieldError, err))
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

	srv := dashboard.NewServer(stores)

	// 启动巡检
	patrol := monitor.NewPatrol(stores.AgentStatus, srv.Bus())
	patrol.Start(ctx)

	port := ":8080"
	logger.Infow("dashboard starting", logger.FieldPort, port)

	util.SafeGo(func() {
		if err := srv.Engine().Run(port); err != nil {
			logger.Fatal("server failed", logger.Any(logger.FieldError, err))
		}
	})

	<-ctx.Done()
	logger.Info("shutting down")
}

package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/multi-agent/go-agent-v2/internal/apiserver"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
)

func main() {
	cfg := config.Load()
	logger.Init(cfg.LogLevel)

	mgr, err := runner.NewAgentManager(
		func(port int, id string) agentcore.Client { return codex.NewAppServerClient(port, id) },
		func(port int, id string) agentcore.Client { return codex.NewClient(port, id) },
	)
	if err != nil {
		logger.Fatal("runner manager init failed", logger.FieldError, err)
	}

	listen := flag.String("listen", "ws://127.0.0.1:4500", "WebSocket 监听地址")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if cfg.PostgresConnStr == "" {
		logger.Fatal("POSTGRES_CONNECTION_STRING is required")
	}
	dbPool, err := database.NewPool(ctx, cfg)
	if err != nil {
		logger.Fatal("postgres connect failed", logger.FieldError, err)
	}
	defer dbPool.Close()

	migrationsDir := cfg.MigrationsDir
	if migrationsDir == "" {
		migrationsDir = filepath.Join(filepath.Dir(os.Args[0]), "..", "..", "migrations")
		if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
			migrationsDir = "migrations"
		}
	}
	if err := database.Migrate(ctx, dbPool, migrationsDir); err != nil {
		if cfg.MigrationNonFatal {
			logger.Warn("migration failed (non-fatal by config)", logger.FieldError, err, logger.FieldPath, migrationsDir)
		} else {
			logger.Fatal("migration failed", logger.FieldError, err, logger.FieldPath, migrationsDir)
		}
	}

	srv := apiserver.New(apiserver.Deps{
		Manager: mgr,
		LSP:     lsp.NewManager(nil),
		Config:  cfg,
		DB:      dbPool,
	})

	mgr.SetOnEvent(func(agentID string, event agentcore.Event) {
		apiserver.AgentEventHandler(srv, agentID)(event)
	})

	cwd, _ := os.Getwd()
	apiserver.SetupLSP(srv, cwd)

	logger.Info("app-server starting", logger.FieldListen, *listen)

	if err := srv.ListenAndServe(ctx, *listen); err != nil {
		logger.Fatal("app-server failed", logger.FieldError, err)
	}
}

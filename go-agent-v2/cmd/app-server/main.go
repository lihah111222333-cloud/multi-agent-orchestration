// cmd/app-server — JSON-RPC over WebSocket 服务入口。
//
// 启动:
//
//	codex app-server --listen ws://127.0.0.1:4500
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/multi-agent/go-agent-v2/internal/apiserver"
	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func main() {
	listen := flag.String("listen", "ws://127.0.0.1:4500", "WebSocket 监听地址")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := config.Load()
	logger.Init(cfg.LogLevel)

	// Runner (Agent 进程管理)
	mgr := runner.NewAgentManager()

	// LSP Manager (延迟启动)
	lspMgr := lsp.NewManager(nil)

	// PostgreSQL (消息持久化, 必需)
	if cfg.PostgresConnStr == "" {
		logger.Fatal("POSTGRES_CONNECTION_STRING is required")
	}
	dbPool, err := database.NewPool(ctx, cfg)
	if err != nil {
		logger.Fatal("postgres connect failed", logger.Any(logger.FieldError, err))
	}
	defer dbPool.Close()

	// 自动迁移
	migrationsDir := filepath.Join(filepath.Dir(os.Args[0]), "..", "..", "migrations")
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		migrationsDir = "migrations"
	}
	if err := database.Migrate(ctx, dbPool, migrationsDir); err != nil {
		if cfg.MigrationNonFatal {
			logger.Warnw("migration failed (non-fatal by config)", logger.FieldError, err, logger.FieldPath, migrationsDir)
		} else {
			logger.Fatal("migration failed", logger.FieldError, err, logger.FieldPath, migrationsDir)
		}
	}

	// JSON-RPC Server
	srv := apiserver.New(apiserver.Deps{
		Manager: mgr,
		LSP:     lspMgr,
		Config:  cfg,
		DB:      dbPool,
	})

	// 注册 Agent 事件 → JSON-RPC Notification 转发
	mgr.SetOnEvent(func(agentID string, event codex.Event) {
		handler := srv.AgentEventHandler(agentID)
		handler(event)
	})

	// LSP 初始化: 诊断缓存 + 广播
	cwd, _ := os.Getwd()
	srv.SetupLSP(cwd)

	logger.Infow("app-server starting", logger.FieldListen, *listen)

	if err := srv.ListenAndServe(ctx, *listen); err != nil {
		logger.Fatal("app-server failed", logger.Any(logger.FieldError, err))
	}
}

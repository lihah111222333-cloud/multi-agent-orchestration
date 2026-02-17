// cmd/agent-terminal — Wails v3 原生多 Agent 终端。
//
// 统一架构:
//   - 内嵌 apiserver — 前端通过 Wails 绑定 App.CallAPI() 调用
//   - Agent 事件通过 Wails Events 推送到前端
//
// 构建:
//
//	go build -tags "production" -o agent-terminal ./cmd/agent-terminal/
package main

import (
	"bufio"
	"context"
	"embed"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/internal/apiserver"
	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed frontend/*
var assets embed.FS

// loadEnvFile 从当前目录向上搜索 .env 文件并加载到环境变量。
// 不覆盖已有的环境变量 — 只填充未设置的。
func loadEnvFile() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 5; i++ {
		envPath := filepath.Join(dir, ".env")
		f, err := os.Open(envPath)
		if err == nil {
			scanner := bufio.NewScanner(f)
			count := 0
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) != 2 {
					continue
				}
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				if _, exists := os.LookupEnv(key); !exists {
					os.Setenv(key, val)
					count++
				}
			}
			f.Close()
			slog.Info("loaded .env file", "path", envPath, "vars_set", count)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}

func main() {
	loadEnvFile()
	info := currentBuildInfo()
	slog.Info("build info",
		"version", info.Version,
		"commit", info.Commit,
		"build_time", info.BuildTime,
		"runtime", info.Runtime,
	)

	// 日志持久化: stdout + 文件
	if err := logger.InitWithFile("logs"); err != nil {
		slog.Warn("file logging unavailable", "error", err)
	}

	group := flag.String("group", "", "窗口分组名称 (显示在标题栏)")
	n := flag.Int("n", 0, "自动启动的 Agent 数量")
	debug := flag.Bool("debug", false, "调试模式: 在 :4501 启动 HTTP UI 服务, 浏览器访问")
	flag.Parse()

	title := "Agent Orchestrator"
	if *group != "" {
		title = fmt.Sprintf("Agent Orchestrator — %s", *group)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 加载配置 + 数据库
	cfg := config.Load()
	var dbPool interface{ Close() } // 用于 shutdown 关闭

	// DB 连接池 (database.NewPool 返回 *pgxpool.Pool)
	var pool *pgxpool.Pool
	if cfg.PostgresConnStr != "" {
		p, err := database.NewPool(ctx, cfg)
		if err != nil {
			slog.Warn("DB not available, dashboard pages will be empty", "error", err)
		} else {
			// 自动执行 DB 迁移
			if mErr := database.Migrate(ctx, p, "./migrations"); mErr != nil {
				slog.Warn("DB migration failed (non-fatal)", "error", mErr)
			}
			logger.AttachDBHandler(p)
			pool = p
			dbPool = p
		}
	} else {
		slog.Info("no POSTGRES_CONNECTION_STRING, dashboard pages disabled")
	}

	// ─── 内嵌 apiserver (统一工具注入 + JSON-RPC) ───
	mgr := runner.NewAgentManager()
	lspMgr := lsp.NewManager(nil)

	deps := apiserver.Deps{
		Manager:   mgr,
		LSP:       lspMgr,
		Config:    cfg,
		DB:        pool,
		SkillsDir: ".agent/skills",
	}
	appSrv := apiserver.New(deps)

	go func() {
		if err := appSrv.ListenAndServe(ctx, "127.0.0.1:4500"); err != nil {
			slog.Error("apiserver failed", "error", err)
		}
	}()

	// ─── 调试模式: HTTP 静态文件 + Wails Shim ───
	if *debug {
		startDebugServer(ctx)
	}

	// ─── Wails App ───
	appSvc := NewApp(*group, *n, appSrv, mgr)
	appSrv.SetNotifyHook(appSvc.handleBridgeNotification)

	// 统一事件分发: 仅走 apiserver 标准化通知链路。
	// codex raw event -> apiserver.Notify(method,payload) -> WebSocket/SSE + Wails bridge
	mgr.SetOnEvent(func(agentID string, event codex.Event) {
		appSrv.AgentEventHandler(agentID)(event)
	})

	app := application.New(application.Options{
		Name: "Agent Orchestrator",
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Services: []application.Service{
			application.NewService(appSvc),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: func() {
			cancel() // 停止 apiserver
			appSvc.shutdown()
			logger.ShutdownDBHandler()
			logger.ShutdownFileHandler()
			if dbPool != nil {
				dbPool.Close()
			}
		},
	})

	appSvc.wailsApp = app

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:           title,
		Width:           1440,
		Height:          900,
		MinWidth:        800,
		MinHeight:       600,
		InitialPosition: application.WindowCentered,
		BackgroundColour: application.RGBA{
			Red: 12, Green: 16, Blue: 23, Alpha: 255,
		},
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarDefault,
		},
	})

	if err := app.Run(); err != nil {
		println("Error:", err.Error())
	}
}

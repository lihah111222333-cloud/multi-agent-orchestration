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
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multi-agent/go-agent-v2/internal/apiserver"
	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/database"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed frontend/*
var assets embed.FS

//go:embed assets/appicon.png
var appIcon []byte

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
			logger.Info("loaded .env file", logger.FieldPath, envPath, logger.FieldVarsSet, count)
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
	logger.Info("build info",
		"version", info.Version,
		"commit", info.Commit,
		"build_time", info.BuildTime,
		"runtime", info.Runtime,
	)

	// 日志持久化: stdout + 文件
	if err := logger.InitWithFile("logs"); err != nil {
		logger.Warn("file logging unavailable", logger.FieldError, err)
	}

	group := flag.String("group", "", "窗口分组名称 (显示在标题栏)")
	n := flag.Int("n", 0, "自动启动的 Agent 数量")
	debug := flag.Bool("debug", false, "调试模式: 在 :4501 启动 HTTP UI 服务, 浏览器访问")
	flag.Parse()

	apiAddr := "127.0.0.1:4500"
	apiBaseURL := "http://127.0.0.1:4500"
	debugUIPort := debugPort

	title := "Agent Orchestrator"
	if *group != "" {
		title = fmt.Sprintf("Agent Orchestrator — %s", *group)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	var shutdownReason atomic.Value
	shutdownReason.Store("unknown")
	recordShutdownReason := func(reason string) {
		if strings.TrimSpace(reason) == "" {
			return
		}
		current, _ := shutdownReason.Load().(string)
		if strings.TrimSpace(current) == "" || current == "unknown" {
			shutdownReason.Store(reason)
		}
	}
	cancelWithReason := func(reason string) {
		recordShutdownReason(reason)
		cancel()
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer signal.Stop(sigCh)
	util.SafeGo(func() {
		cancelSent := false
		for sig := range sigCh {
			if sig == nil {
				continue
			}
			recordShutdownReason("os_signal:" + sig.String())
			logger.Warn("shutdown trigger: OS signal received", "signal", sig.String(), "cancel_sent", cancelSent)
			if !cancelSent {
				cancel()
				cancelSent = true
			}
		}
	})
	util.SafeGo(func() {
		<-ctx.Done()
		reason, _ := shutdownReason.Load().(string)
		logger.Warn("shutdown trigger: root context canceled", "reason", reason, "ctx_err", ctx.Err())
	})

	// 加载配置 + 数据库
	cfg := config.Load()

	// DB 连接池 (database.NewPool 返回 *pgxpool.Pool)
	var pool *pgxpool.Pool
	if cfg.PostgresConnStr != "" {
		p, err := database.NewPool(ctx, cfg)
		if err != nil {
			logger.Warn("DB not available, dashboard pages will be empty", logger.FieldError, err)
		} else {
			// 自动执行 DB 迁移
			if mErr := database.Migrate(ctx, p, "./migrations"); mErr != nil {
				logger.Warn("DB migration failed (non-fatal)", logger.FieldError, mErr)
			}
			logger.AttachDBHandler(p)
			pool = p
		}
	} else {
		logger.Info("no POSTGRES_CONNECTION_STRING, dashboard pages disabled")
	}

	// ─── 内嵌 apiserver (统一工具注入 + JSON-RPC) ───
	mgr := runner.NewAgentManager()
	runner.CleanOrphanedProcesses()
	lspMgr := lsp.NewManager(nil)

	deps := apiserver.Deps{
		Manager:   mgr,
		LSP:       lspMgr,
		Config:    cfg,
		DB:        pool,
		SkillsDir: ".agent/skills",
	}
	appSrv := apiserver.New(deps)
	setupAppServerLSPRoot(appSrv)

	util.SafeGo(func() {
		if err := appSrv.ListenAndServe(ctx, apiAddr); err != nil {
			logger.Error("apiserver failed", logger.FieldError, err)
		}
	})

	// ─── 调试模式: HTTP 静态文件 + Wails Shim ───
	// 统一事件分发: 仅走 apiserver 标准化通知链路。
	// codex raw event -> apiserver.Notify(method,payload) -> WebSocket/SSE + Wails bridge
	mgr.SetOnEvent(func(agentID string, event codex.Event) {
		appSrv.AgentEventHandler(agentID)(event)
	})

	// ─── 调试模式: 同时启动 debug HTTP 服务 + Wails 桌面窗口 ───
	if *debug {
		startDebugServer(ctx, debugUIPort, apiBaseURL)
		logger.Info("debug mode: web UI + desktop app",
			logger.FieldURL, fmt.Sprintf("http://localhost:%d", debugUIPort),
			"api_url", apiBaseURL)
	}

	// ─── Wails App ───
	appSvc := NewApp(*group, *n, appSrv, mgr)
	// 统一通过 App 桥接转发，避免 debug 模式重复 publish 同一事件。
	appSrv.SetNotifyHook(appSvc.handleBridgeNotification)
	var quitOverlayShown atomic.Bool
	var quitForceAllowed atomic.Bool

	app := application.New(application.Options{
		Name: "Agent Orchestrator",
		Icon: appIcon,
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Services: []application.Service{
			application.NewService(appSvc),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		ShouldQuit: func() bool {
			logger.Info("quit: request received",
				"force_allowed", quitForceAllowed.Load(),
				"overlay_shown", quitOverlayShown.Load(),
				"trace", callerTrace(3, 8),
			)
			if quitForceAllowed.Load() {
				logger.Info("quit: allowing shutdown")
				return true
			}
			if !quitOverlayShown.CompareAndSwap(false, true) {
				logger.Info("quit: request ignored while overlay pending")
				return false
			}
			logger.Info("quit: showing exit overlay before shutdown", "delay_ms", 320)
			if appSvc.wailsApp != nil && appSvc.wailsApp.Event != nil {
				appSvc.wailsApp.Event.Emit("app-will-quit", map[string]any{
					"delay_ms": 320,
					"at":       time.Now().UTC().Format(time.RFC3339Nano),
				})
			}
			util.SafeGo(func() {
				time.Sleep(320 * time.Millisecond)
				quitForceAllowed.Store(true)
				logger.Info("quit: grace delay elapsed, invoking Quit()")
				if appSvc.wailsApp != nil {
					appSvc.wailsApp.Quit()
				} else {
					logger.Warn("quit: wails app is nil during forced Quit()")
				}
			})
			return false
		},
		OnShutdown: func() {
			recordShutdownReason("wails_on_shutdown")
			reason, _ := shutdownReason.Load().(string)
			logger.Warn("on-shutdown: begin", "reason", reason, "active_agents", len(mgr.List()))
			cancelWithReason("wails_on_shutdown")
			appSvc.shutdown()
			logger.ShutdownDBHandler()
			logger.ShutdownFileHandler()
			if pool != nil {
				pool.Close()
			}
			logger.Warn("on-shutdown: completed", "reason", reason)
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
		logger.Error("wails app failed", logger.FieldError, err)
	}
	reason, _ := shutdownReason.Load().(string)
	logger.Warn("wails app exited", "reason", reason)
}

type lspRootSetupper interface {
	SetupLSP(rootDir string)
}

func setupAppServerLSPRoot(server lspRootSetupper) {
	if server == nil {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		logger.Warn("setup app-server lsp root failed", logger.FieldError, err)
		return
	}
	server.SetupLSP(cwd)
}

func callerTrace(skip, maxFrames int) string {
	if maxFrames <= 0 {
		return ""
	}
	pcs := make([]uintptr, maxFrames)
	n := runtime.Callers(skip, pcs)
	if n == 0 {
		return ""
	}
	frames := runtime.CallersFrames(pcs[:n])
	parts := make([]string, 0, maxFrames)
	for len(parts) < maxFrames {
		frame, more := frames.Next()
		fn := strings.TrimSpace(frame.Function)
		if fn != "" {
			if idx := strings.LastIndex(fn, "/"); idx >= 0 {
				fn = fn[idx+1:]
			}
			parts = append(parts, fmt.Sprintf("%s:%d", fn, frame.Line))
		}
		if !more {
			break
		}
	}
	return strings.Join(parts, " <- ")
}

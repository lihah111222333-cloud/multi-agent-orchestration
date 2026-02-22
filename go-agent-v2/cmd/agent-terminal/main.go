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
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/coverage"
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
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed frontend/dist/*
var assets embed.FS

//go:embed assets/appicon.png
var appIcon []byte

// frontendAssets 返回前端静态资源 FS, 去掉 "frontend/dist" 前缀。
func frontendAssets() http.FileSystem {
	sub, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		logger.Error("embed: failed to sub frontend/dist", logger.FieldError, err)
		return http.FS(assets)
	}
	return http.FS(sub)
}

// loadEnvFile 从当前目录向上搜索 .env 文件并加载到环境变量。
// 不覆盖已有的环境变量 — 只填充未设置的。
func loadEnvFile() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for range 5 {
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
					if err := os.Setenv(key, val); err != nil {
						logger.Warn("loadEnvFile: setenv failed", "key", key, logger.FieldError, err)
						continue
					}
					count++
				}
			}
			_ = f.Close()
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

	title := "Agent Orchestrator"
	if *group != "" {
		title = fmt.Sprintf("Agent Orchestrator — %s", *group)
	}

	// ─── 上下文 & 优雅关停 ───
	ctx, cancel, shutdownReason, cancelWithReason, signalCleanup := setupShutdownSignals()
	defer cancel()
	defer signalCleanup()

	// ─── 数据库 ───
	cfg := config.Load()
	// Wails 桌面 App 需要全部 JSON-RPC 方法 (config/read, model/list 等)
	cfg.DisableOffline52Methods = false
	pool := setupDatabase(ctx, cfg)

	// ─── 内嵌 apiserver ───
	appSrv, mgr := setupAppServer(ctx, cfg, pool, apiAddr)

	// ─── 调试模式 ───
	if *debug {
		startDebugServer(ctx, debugPort, apiBaseURL)
		logger.Info("debug mode: web UI + desktop app",
			logger.FieldURL, fmt.Sprintf("http://localhost:%d", debugPort),
			"api_url", apiBaseURL)
	}

	// ─── Wails App ───
	appSvc := NewApp(*group, *n, appSrv, mgr)
	appSrv.SetNotifyHook(appSvc.handleBridgeNotification)
	var quitOverlayShown atomic.Bool
	var quitForceAllowed atomic.Bool
	var coverageFlushed atomic.Bool
	flushCoverage := func(reason string) {
		if !coverageFlushed.CompareAndSwap(false, true) {
			return
		}
		flushCoverageCounters(reason)
	}
	util.SafeGo(func() {
		<-ctx.Done()
		flushCoverage("root_context_done")
	})

	app := application.New(application.Options{
		Name: "Agent Orchestrator",
		Icon: appIcon,
		Assets: application.AssetOptions{
			Handler: http.FileServer(frontendAssets()),
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
			cancelWithReason("wails_on_shutdown")
			reason, _ := shutdownReason.Load().(string)
			logger.Warn("on-shutdown: begin", "reason", reason, "active_agents", len(mgr.List()))
			appSvc.shutdown()
			flushCoverage("wails_on_shutdown")
			logger.ShutdownDBHandler()
			logger.ShutdownFileHandler()
			if pool != nil {
				pool.Close()
			}
			logger.Warn("on-shutdown: completed", "reason", reason)
		},
	})

	appSvc.wailsApp = app

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:           title,
		Width:           1440,
		Height:          900,
		MinWidth:        800,
		MinHeight:       600,
		EnableFileDrop:  true,
		InitialPosition: application.WindowCentered,
		BackgroundColour: application.RGBA{
			Red: 12, Green: 16, Blue: 23, Alpha: 255,
		},
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarDefault,
		},
	})

	mainWindow.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		if event == nil {
			return
		}
		ctx := event.Context()
		files := ctx.DroppedFiles()
		if len(files) == 0 {
			return
		}

		payload := map[string]any{
			"files": files,
		}

		targetID := ""
		if details := ctx.DropTargetDetails(); details != nil {
			targetID = strings.TrimSpace(details.ElementID)
			payload["details"] = map[string]any{
				"id":         details.ElementID,
				"classList":  details.ClassList,
				"attributes": details.Attributes,
				"x":          details.X,
				"y":          details.Y,
			}
		}

		app.Event.Emit("files-dropped", payload)
		logger.Info("wails: files dropped",
			logger.FieldCount, len(files),
			"target_id", targetID,
			"first", files[0],
		)
	})

	if err := app.Run(); err != nil {
		logger.Error("wails app failed", logger.FieldError, err)
	}
	flushCoverage("app_run_return")
	reason, _ := shutdownReason.Load().(string)
	logger.Warn("wails app exited", "reason", reason)
}

func flushCoverageCounters(reason string) {
	dir := strings.TrimSpace(os.Getenv("GOCOVERDIR"))
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("coverage: ensure dir failed", "reason", reason, logger.FieldPath, dir, logger.FieldError, err)
		return
	}
	if err := coverage.WriteCountersDir(dir); err != nil {
		logger.Warn("coverage: write counters failed", "reason", reason, logger.FieldPath, dir, logger.FieldError, err)
		return
	}
	logger.Info("coverage: counters flushed", "reason", reason, logger.FieldPath, dir)
}

// setupShutdownSignals 初始化上下文 + 优雅关停信号处理。
func setupShutdownSignals() (ctx context.Context, cancel context.CancelFunc, shutdownReason *atomic.Value, cancelWithReason func(string), cleanup func()) {
	ctx, cancel = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	shutdownReason = &atomic.Value{}
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
	cancelWithReason = func(reason string) {
		recordShutdownReason(reason)
		cancel()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
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

	cleanup = func() { signal.Stop(sigCh) }
	return ctx, cancel, shutdownReason, cancelWithReason, cleanup
}

// setupDatabase 初始化 PostgreSQL 连接池 + 自动迁移。
func setupDatabase(ctx context.Context, cfg *config.Config) *pgxpool.Pool {
	if cfg.PostgresConnStr == "" {
		logger.Info("no POSTGRES_CONNECTION_STRING, dashboard pages disabled")
		return nil
	}
	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		logger.Warn("DB not available, dashboard pages will be empty", logger.FieldError, err)
		return nil
	}
	if mErr := database.Migrate(ctx, pool, "./migrations"); mErr != nil {
		logger.Warn("DB migration failed (non-fatal)", logger.FieldError, mErr)
	}
	logger.AttachDBHandler(pool)
	return pool
}

// setupAppServer 创建 apiserver + runner manager 并启动监听。
func setupAppServer(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, addr string) (*apiserver.Server, *runner.AgentManager) {
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
		if err := appSrv.ListenAndServe(ctx, addr); err != nil {
			logger.Error("apiserver failed", logger.FieldError, err)
		}
	})

	// 统一事件分发: codex raw event -> apiserver.Notify(method,payload) -> WebSocket/SSE + Wails bridge
	mgr.SetOnEvent(func(agentID string, event codex.Event) {
		appSrv.AgentEventHandler(agentID)(event)
	})

	return appSrv, mgr
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

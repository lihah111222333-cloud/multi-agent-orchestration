// Package dashboard 提供管理面板 HTTP 服务 (对应 Python dashboard.py)。
package dashboard

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// Server Dashboard HTTP 服务。
type Server struct {
	router *gin.Engine
	stores *Stores
	bus    *EventBus
}

// Stores 聚合所有 store 依赖 (DRY: 一次注入)。
type Stores struct {
	Interaction      *store.InteractionStore
	TaskTrace        *store.TaskTraceStore
	PromptTemplate   *store.PromptTemplateStore
	CommandCard      *store.CommandCardStore
	AuditLog         *store.AuditLogStore
	SystemLog        *store.SystemLogStore
	AILog            *store.AILogStore
	BusLog           *store.BusLogStore
	SharedFile       *store.SharedFileStore
	AgentStatus      *store.AgentStatusStore
	TopologyApproval *store.TopologyApprovalStore
	DBQuery          *store.DBQueryStore
}

// NewServer 创建 Dashboard 服务。
//
// 根据 cfg.GinMode 设置运行模式 (release/debug/test),
// 并将 cfg.TrustedProxies 解析为可信代理列表。
func NewServer(stores *Stores, cfg *config.Config) *Server {
	gin.SetMode(cfg.GinMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 解析逗号分隔的可信代理 IP
	var proxies []string
	for _, p := range strings.Split(cfg.TrustedProxies, ",") {
		if t := strings.TrimSpace(p); t != "" {
			proxies = append(proxies, t)
		}
	}
	if err := r.SetTrustedProxies(proxies); err != nil {
		logger.Warn("dashboard: set trusted proxies failed", logger.FieldError, err)
	}

	s := &Server{router: r, stores: stores, bus: NewEventBus()}
	s.registerRoutes()
	return s
}

// Engine 返回 Gin 引擎。
func (s *Server) Engine() *gin.Engine { return s.router }

// Bus 返回事件总线。
func (s *Server) Bus() *EventBus { return s.bus }

// ListenAndServe 启动 HTTP 服务并支持优雅退出。
//
// ctx 取消后等待 5 秒完成活跃请求再关闭。
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 优雅关闭: 给活跃请求 5 秒完成处理
	go func() {
		<-ctx.Done()
		logger.Info("dashboard: shutdown trigger")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("dashboard: shutdown error", logger.FieldError, err)
			return
		}
		logger.Info("dashboard: shutdown completed")
	}()

	logger.Info("dashboard: listening", logger.FieldAddr, addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

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

type Server struct {
	router *gin.Engine
	stores *Stores
	bus    *EventBus
}

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

func NewServer(stores *Stores, cfg *config.Config) *Server {
	gin.SetMode(cfg.GinMode)
	r := gin.New()
	r.Use(gin.Recovery())

	proxies := make([]string, 0, strings.Count(cfg.TrustedProxies, ",")+1)
	for _, item := range strings.Split(cfg.TrustedProxies, ",") {
		if proxy := strings.TrimSpace(item); proxy != "" {
			proxies = append(proxies, proxy)
		}
	}
	if err := r.SetTrustedProxies(proxies); err != nil {
		logger.Warn("dashboard: set trusted proxies failed", logger.FieldError, err)
	}

	s := &Server{router: r, stores: stores, bus: NewEventBus()}
	s.registerRoutes()
	return s
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
	}

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

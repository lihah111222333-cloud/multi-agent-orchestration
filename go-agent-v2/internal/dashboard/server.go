// Package dashboard 提供管理面板 HTTP 服务 (对应 Python dashboard.py)。
package dashboard

import (
	"github.com/gin-gonic/gin"

	"github.com/multi-agent/go-agent-v2/internal/store"
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
func NewServer(stores *Stores) *Server {
	r := gin.Default()
	s := &Server{router: r, stores: stores, bus: NewEventBus()}
	s.registerRoutes()
	return s
}

// Engine 返回 Gin 引擎。
func (s *Server) Engine() *gin.Engine { return s.router }

// Bus 返回事件总线。
func (s *Server) Bus() *EventBus { return s.bus }

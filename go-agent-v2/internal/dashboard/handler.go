// handler.go — Dashboard REST API handlers (对应 Python gin_handler.py)。
package dashboard

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/multi-agent/go-agent-v2/internal/store"
)

// registerRoutes 注册 API 路由 (对应 Python dashboard.py do_GET/do_POST)。
func (s *Server) registerRoutes() {
	api := s.router.Group("/api")

	api.GET("/interactions", s.listInteractions)
	api.POST("/interactions", s.createInteraction)

	api.GET("/task-traces", s.listTaskTraces)

	api.GET("/prompt-templates", s.listPromptTemplates)
	api.POST("/prompt-templates", s.savePromptTemplate)
	api.POST("/prompt-templates/toggle", s.togglePromptTemplate)
	api.DELETE("/prompt-templates/:key", s.deletePromptTemplate)

	api.GET("/command-cards", s.listCommandCards)
	api.POST("/command-cards", s.saveCommandCard)
	api.DELETE("/command-cards/:key", s.deleteCommandCard)

	api.GET("/audit-log", s.listAuditLog)
	api.GET("/system-log", s.listSystemLog)
	api.GET("/ai-log", s.listAILog)
	api.GET("/bus-log", s.listBusLog)

	api.GET("/agent-status", s.listAgentStatus)

	api.GET("/shared-files", s.listSharedFiles)
	api.POST("/shared-files", s.writeSharedFile)
	api.DELETE("/shared-files/*path", s.deleteSharedFile)

	api.GET("/topology/pending", s.listPendingApprovals)
	api.POST("/topology/approve", s.approveTopology)
	api.POST("/topology/reject", s.rejectTopology)

	api.POST("/db-query", s.dbQuery)

	api.GET("/events", s.sseHandler)

	s.router.Static("/static", "./static")
	s.router.GET("/", func(c *gin.Context) { c.File("./static/index.html") })
}

// ========================================
// 辅助: 从 query 读分页参数 (DRY)
// ========================================

func queryLimit(c *gin.Context, def int) int {
	v, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(def)))
	if v < 1 {
		return def
	}
	if v > 2000 {
		return 2000
	}
	return v
}

// ========================================
// Interactions
// ========================================

func (s *Server) listInteractions(c *gin.Context) {
	items, err := s.stores.Interaction.List(c.Request.Context(),
		c.Query("thread_id"), c.Query("keyword"), queryLimit(c, 100))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

func (s *Server) createInteraction(c *gin.Context) {
	var req struct {
		ThreadID string `json:"thread_id"`
		Sender   string `json:"sender"`
		Receiver string `json:"receiver"`
		MsgType  string `json:"msg_type"`
		Payload  any    `json:"payload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid_request", err.Error())
		return
	}
	item, err := s.stores.Interaction.Create(c.Request.Context(), &store.Interaction{
		ThreadID: req.ThreadID, Sender: req.Sender, Receiver: req.Receiver,
		MsgType: req.MsgType, Payload: req.Payload,
	})
	if err != nil {
		serverError(c, err)
		return
	}
	created(c, item)
}

// ========================================
// Task Traces
// ========================================

func (s *Server) listTaskTraces(c *gin.Context) {
	items, err := s.stores.TaskTrace.List(c.Request.Context(),
		c.Query("agent_id"), c.Query("keyword"), nil, queryLimit(c, 100))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

// ========================================
// Prompt Templates
// ========================================

func (s *Server) listPromptTemplates(c *gin.Context) {
	items, err := s.stores.PromptTemplate.List(c.Request.Context(),
		c.Query("agent_key"), c.Query("keyword"), queryLimit(c, 100))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

func (s *Server) savePromptTemplate(c *gin.Context) {
	var req store.PromptTemplate
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid_request", err.Error())
		return
	}
	item, err := s.stores.PromptTemplate.Save(c.Request.Context(), &req)
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, item)
}

func (s *Server) togglePromptTemplate(c *gin.Context) {
	var req struct {
		PromptKey string `json:"prompt_key"`
		Enabled   bool   `json:"enabled"`
		UpdatedBy string `json:"updated_by"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid_request", err.Error())
		return
	}
	if err := s.stores.PromptTemplate.SetEnabled(c.Request.Context(), req.PromptKey, req.Enabled, req.UpdatedBy); err != nil {
		serverError(c, err)
		return
	}
	success(c, gin.H{"ok": true})
}

func (s *Server) deletePromptTemplate(c *gin.Context) {
	if err := s.stores.PromptTemplate.Delete(c.Request.Context(), c.Param("key")); err != nil {
		serverError(c, err)
		return
	}
	success(c, gin.H{"ok": true})
}

// ========================================
// Command Cards
// ========================================

func (s *Server) listCommandCards(c *gin.Context) {
	items, err := s.stores.CommandCard.List(c.Request.Context(), c.Query("keyword"), queryLimit(c, 100))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

func (s *Server) saveCommandCard(c *gin.Context) {
	var req store.CommandCard
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid_request", err.Error())
		return
	}
	item, err := s.stores.CommandCard.Save(c.Request.Context(), &req)
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, item)
}

func (s *Server) deleteCommandCard(c *gin.Context) {
	if err := s.stores.CommandCard.Delete(c.Request.Context(), c.Param("key")); err != nil {
		serverError(c, err)
		return
	}
	success(c, gin.H{"ok": true})
}

// ========================================
// Logs (DRY: 四种日志 handler 同一模式)
// ========================================

func (s *Server) listAuditLog(c *gin.Context) {
	items, err := s.stores.AuditLog.List(c.Request.Context(),
		c.Query("event_type"), c.Query("action"), c.Query("actor"), c.Query("keyword"), queryLimit(c, 100))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

func (s *Server) listSystemLog(c *gin.Context) {
	items, err := s.stores.SystemLog.ListV2(c.Request.Context(), store.ListParams{
		Level:     c.Query("level"),
		Logger:    c.Query("logger"),
		Source:    c.Query("source"),
		Component: c.Query("component"),
		AgentID:   c.Query("agent_id"),
		ThreadID:  c.Query("thread_id"),
		EventType: c.Query("event_type"),
		ToolName:  c.Query("tool_name"),
		Keyword:   c.Query("keyword"),
		Limit:     queryLimit(c, 100),
	})
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

func (s *Server) listAILog(c *gin.Context) {
	items, err := s.stores.AILog.Query(c.Request.Context(),
		c.Query("category"), c.Query("keyword"), queryLimit(c, 100))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

func (s *Server) listBusLog(c *gin.Context) {
	items, err := s.stores.BusLog.List(c.Request.Context(),
		c.Query("category"), c.Query("severity"), c.Query("keyword"), queryLimit(c, 100))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

// ========================================
// Agent Status
// ========================================

func (s *Server) listAgentStatus(c *gin.Context) {
	items, err := s.stores.AgentStatus.List(c.Request.Context(), c.Query("status"))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

// ========================================
// Shared Files
// ========================================

func (s *Server) listSharedFiles(c *gin.Context) {
	items, err := s.stores.SharedFile.List(c.Request.Context(), c.Query("prefix"), queryLimit(c, 200))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

func (s *Server) writeSharedFile(c *gin.Context) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Actor   string `json:"actor"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid_request", err.Error())
		return
	}
	item, err := s.stores.SharedFile.Write(c.Request.Context(), req.Path, req.Content, req.Actor)
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, item)
}

func (s *Server) deleteSharedFile(c *gin.Context) {
	deleted, err := s.stores.SharedFile.Delete(c.Request.Context(), c.Param("path"), c.Query("actor"))
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, gin.H{"deleted": deleted})
}

// ========================================
// Topology
// ========================================

func (s *Server) listPendingApprovals(c *gin.Context) {
	items, err := s.stores.TopologyApproval.GetPending(c.Request.Context())
	if err != nil {
		serverError(c, err)
		return
	}
	success(c, items)
}

func (s *Server) approveTopology(c *gin.Context) {
	var req struct {
		ID         int    `json:"id"`
		ApprovedBy string `json:"approved_by"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid_request", err.Error())
		return
	}
	if err := s.stores.TopologyApproval.Approve(c.Request.Context(), req.ID, req.ApprovedBy); err != nil {
		serverError(c, err)
		return
	}
	success(c, gin.H{"ok": true})
}

func (s *Server) rejectTopology(c *gin.Context) {
	var req struct {
		ID         int    `json:"id"`
		RejectedBy string `json:"rejected_by"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid_request", err.Error())
		return
	}
	if err := s.stores.TopologyApproval.Reject(c.Request.Context(), req.ID, req.RejectedBy); err != nil {
		serverError(c, err)
		return
	}
	success(c, gin.H{"ok": true})
}

// ========================================
// DB Query
// ========================================

func (s *Server) dbQuery(c *gin.Context) {
	var req struct {
		SQL   string `json:"sql"`
		Limit int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid_request", err.Error())
		return
	}
	rows, err := s.stores.DBQuery.Query(c.Request.Context(), req.SQL, req.Limit)
	if err != nil {
		badRequest(c, "query_error", err.Error())
		return
	}
	success(c, rows)
}

// server.go — JSON-RPC over WebSocket 服务器。
//
// 架构:
//
//	WebSocket 连接 → JSON-RPC 2.0 消息解析 → 方法分发 → 响应
//	Agent 事件 → Notification 广播给所有连接
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// Handler JSON-RPC 方法处理器。
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// connEntry WebSocket 连接 + 写锁 (gorilla/websocket 不安全并发写)。
type connEntry struct {
	ws   *websocket.Conn
	wrMu sync.Mutex // 序列化所有写操作
}

// writeMsg 线程安全地写入 WebSocket 消息。
func (c *connEntry) writeMsg(msgType int, data []byte) error {
	c.wrMu.Lock()
	defer c.wrMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteMessage(msgType, data)
}

const (
	maxConnections = 100     // 最大并发连接数
	maxMessageSize = 4 << 20 // 4MB 消息大小限制
)

// Server JSON-RPC WebSocket 服务器。
type Server struct {
	mgr     *runner.AgentManager
	lsp     *lsp.Manager
	cfg     *config.Config
	methods map[string]Handler

	// 消息持久化
	msgStore *store.AgentMessageStore

	// 资源 Store (编排工具依赖)
	dagStore    *store.TaskDAGStore
	cmdStore    *store.CommandCardStore
	promptStore *store.PromptTemplateStore
	fileStore   *store.SharedFileStore
	sysLogStore *store.SystemLogStore

	// Dashboard Store (JSON-RPC dashboard/* 方法)
	agentStatusStore *store.AgentStatusStore
	auditLogStore    *store.AuditLogStore
	aiLogStore       *store.AILogStore
	busLogStore      *store.BusLogStore
	taskAckStore     *store.TaskAckStore
	taskTraceStore   *store.TaskTraceStore
	skillSvc         *service.SkillService

	// 连接管理 (支持多 IDE 同时连接)
	mu     sync.RWMutex
	conns  map[string]*connEntry // connID → entry
	nextID atomic.Int64

	// § 二 Server → Client 请求: 服务端发起请求, 等待客户端响应
	pendingMu sync.Mutex
	pending   map[int64]chan *Response // requestID → response channel
	nextReqID atomic.Int64

	threadSeq atomic.Int64 // thread/start 唯一序号

	// LSP 诊断缓存 (uri → diagnostics)
	diagMu    sync.RWMutex
	diagCache map[string][]lsp.Diagnostic

	// 动态工具调用计数 (可观测性)
	toolCallMu    sync.Mutex
	toolCallCount map[string]int64 // toolName → count

	// Per-session 技能配置 (agentID → skills 列表)
	skillsMu    sync.RWMutex
	agentSkills map[string][]string // agentID → [“skill1”, “skill2”]

	upgrader websocket.Upgrader
}

// Deps 服务器依赖注入。
type Deps struct {
	Manager   *runner.AgentManager
	LSP       *lsp.Manager
	Config    *config.Config
	DB        *pgxpool.Pool // 必需: 消息持久化 + 资源工具
	SkillsDir string        // .agent/skills 目录路径 (可选, 默认 ".agent/skills")
}

// New 创建服务器。
func New(deps Deps) *Server {
	s := &Server{
		mgr:           deps.Manager,
		lsp:           deps.LSP,
		cfg:           deps.Config,
		methods:       make(map[string]Handler),
		conns:         make(map[string]*connEntry),
		pending:       make(map[int64]chan *Response),
		diagCache:     make(map[string][]lsp.Diagnostic),
		toolCallCount: make(map[string]int64),
		agentSkills:   make(map[string][]string),
		upgrader: websocket.Upgrader{
			CheckOrigin: checkLocalOrigin,
		},
	}
	if deps.DB != nil {
		s.msgStore = store.NewAgentMessageStore(deps.DB)
		s.dagStore = store.NewTaskDAGStore(deps.DB)
		s.cmdStore = store.NewCommandCardStore(deps.DB)
		s.promptStore = store.NewPromptTemplateStore(deps.DB)
		s.fileStore = store.NewSharedFileStore(deps.DB)
		s.sysLogStore = store.NewSystemLogStore(deps.DB)
		// Dashboard stores
		s.agentStatusStore = store.NewAgentStatusStore(deps.DB)
		s.auditLogStore = store.NewAuditLogStore(deps.DB)
		s.aiLogStore = store.NewAILogStore(deps.DB)
		s.busLogStore = store.NewBusLogStore(deps.DB)
		s.taskAckStore = store.NewTaskAckStore(deps.DB)
		s.taskTraceStore = store.NewTaskTraceStore(deps.DB)
		slog.Info("app-server: message persistence + resource tools + dashboard enabled")
	}
	// Skills service (filesystem, no DB required)
	skillsDir := deps.SkillsDir
	if skillsDir == "" {
		skillsDir = ".agent/skills"
	}
	s.skillSvc = service.NewSkillService(skillsDir)
	s.registerMethods()
	return s
}

// InvokeMethod 内部调用 JSON-RPC 方法 (不经过 WebSocket)。
//
// 用于 Wails UI 等 Go 进程内客户端直接调用后端功能。
func (s *Server) InvokeMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	handler, ok := s.methods[method]
	if !ok {
		return nil, fmt.Errorf("unknown method: %s", method)
	}
	return handler(ctx, params)
}

// checkLocalOrigin 仅允许 localhost 来源的 WebSocket 连接。
//
// 接受: 无 Origin header (本地工具), localhost, 127.0.0.1, [::1]。
func checkLocalOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // 无 Origin = 非浏览器客户端 (CLI/IDE)
	}
	origin = strings.ToLower(origin)
	for _, allowed := range []string{
		"http://localhost", "https://localhost",
		"http://127.0.0.1", "https://127.0.0.1",
		"http://[::1]", "https://[::1]",
		"wails://", // Wails 桌面应用 WebKit
	} {
		if strings.HasPrefix(origin, allowed) {
			return true
		}
	}
	slog.Warn("app-server: rejected non-local origin", "origin", origin)
	return false
}

// ListenAndServe 启动 WebSocket 服务器。
//
// addr 格式: "ws://127.0.0.1:4500" 或 "127.0.0.1:4500"。
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	// 解析地址: 去掉 ws:// 前缀
	host := strings.TrimPrefix(addr, "ws://")
	host = strings.TrimPrefix(host, "wss://")

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleUpgrade)

	srv := &http.Server{
		Addr:        host,
		Handler:     mux,
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	// 优雅关闭
	go func() {
		<-ctx.Done()
		slog.Info("app-server: shutting down")
		_ = srv.Close()
	}()

	slog.Info("app-server: listening", "addr", host)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("app-server: %w", err)
	}
	return nil
}

// Notify 向所有连接广播 JSON-RPC 通知。
func (s *Server) Notify(method string, params any) {
	notif := newNotification(method, params)
	data, err := json.Marshal(notif)
	if err != nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, entry := range s.conns {
		if err := entry.writeMsg(websocket.TextMessage, data); err != nil {
			slog.Warn("app-server: notify failed", "conn", id, "error", err)
		}
	}
}

// SendRequest 向指定连接发送 Server→Client 请求并等待响应 (§ 二)。
//
// 用于 approval 流程: requestApproval → client 审批 → 返回结果。
// 超时 5 分钟 (用户审批需要时间)。
func (s *Server) SendRequest(connID, method string, params any) (*Response, error) {
	reqID := s.nextReqID.Add(1)

	req := Request{
		JSONRPC: jsonrpcVersion,
		ID:      reqID,
		Method:  method,
	}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = raw
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// 创建 response channel
	ch := make(chan *Response, 1)
	s.pendingMu.Lock()
	s.pending[reqID] = ch
	s.pendingMu.Unlock()

	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, reqID)
		s.pendingMu.Unlock()
	}()

	// 发送到指定连接
	s.mu.RLock()
	entry, ok := s.conns[connID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("connection %s not found", connID)
	}

	if err := entry.writeMsg(websocket.TextMessage, data); err != nil {
		return nil, fmt.Errorf("write to %s: %w", connID, err)
	}

	slog.Info("app-server: sent request to client", "conn", connID, "method", method, "id", reqID)

	// 等待客户端响应 (5 分钟超时)
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("request %d timed out waiting for client response", reqID)
	}
}

// SendRequestToAll 向所有连接广播 Server→Client 请求, 返回第一个响应。
//
// 适用于只有一个 IDE 连接的场景。
func (s *Server) SendRequestToAll(method string, params any) (*Response, error) {
	s.mu.RLock()
	var firstConn string
	for id := range s.conns {
		firstConn = id
		break
	}
	s.mu.RUnlock()

	if firstConn == "" {
		return nil, fmt.Errorf("no connected clients")
	}
	return s.SendRequest(firstConn, method, params)
}

// handleClientResponse 处理客户端对 Server→Client 请求的响应。
//
// 当 readLoop 收到一条消息, 且 ID 匹配 pending 请求时, 路由到此。
func (s *Server) handleClientResponse(resp *Response) bool {
	if resp.ID == nil {
		return false
	}

	// ID 可能是 float64 (JSON number) 或 int64
	var reqID int64
	switch v := resp.ID.(type) {
	case float64:
		reqID = int64(v)
	case int64:
		reqID = v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			reqID = n
		}
	default:
		return false
	}

	s.pendingMu.Lock()
	ch, ok := s.pending[reqID]
	s.pendingMu.Unlock()

	if !ok {
		return false
	}

	ch <- resp
	return true
}

// handleUpgrade HTTP → WebSocket 升级。
func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	// 连接数限制
	s.mu.RLock()
	numConns := len(s.conns)
	s.mu.RUnlock()
	if numConns >= maxConnections {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		slog.Warn("app-server: connection rejected (max reached)", "max", maxConnections)
		return
	}

	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("app-server: upgrade failed", "error", err)
		return
	}

	// 消息大小限制
	ws.SetReadLimit(maxMessageSize)

	connID := fmt.Sprintf("conn-%d", s.nextID.Add(1))
	entry := &connEntry{ws: ws}
	s.mu.Lock()
	s.conns[connID] = entry
	s.mu.Unlock()

	slog.Info("app-server: client connected", "conn", connID, "remote", r.RemoteAddr)

	defer func() {
		s.mu.Lock()
		delete(s.conns, connID)
		s.mu.Unlock()
		_ = ws.Close()
		slog.Info("app-server: client disconnected", "conn", connID)
	}()

	s.readLoop(r.Context(), entry, connID)
}

// readLoop 持续读取 WebSocket 消息并分发。
//
// 消息分为三类:
//  1. Client→Server 请求 (有 method + id): 路由到 handleMessage
//  2. Client→Server 通知 (有 method, 无 id): 路由到 handleMessage
//  3. Client 对 Server 请求的响应 (有 id, 无 method): 路由到 handleClientResponse
func (s *Server) readLoop(ctx context.Context, entry *connEntry, connID string) {
	for {
		_, message, err := entry.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("app-server: read error", "conn", connID, "error", err)
			}
			return
		}

		// 先尝试解析为响应 (§ 二 Server→Client 请求的回复)
		var probe struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Result any    `json:"result,omitempty"`
			Error  any    `json:"error,omitempty"`
		}
		if err := json.Unmarshal(message, &probe); err == nil {
			// 有 id + 无 method + (有 result 或 error) = 客户端响应
			if probe.ID != nil && probe.Method == "" && (probe.Result != nil || probe.Error != nil) {
				var resp Response
				if err := json.Unmarshal(message, &resp); err == nil {
					if s.handleClientResponse(&resp) {
						continue
					}
				}
			}
		}

		// 正常请求/通知
		resp := s.handleMessage(ctx, message)
		if resp == nil {
			continue
		}

		data, err := json.Marshal(resp)
		if err != nil {
			slog.Error("app-server: marshal response failed", "error", err)
			continue
		}

		if err := entry.writeMsg(websocket.TextMessage, data); err != nil {
			slog.Warn("app-server: write failed", "conn", connID, "error", err)
			return
		}
	}
}

// handleMessage 解析 JSON-RPC 请求并分发到对应 handler。
func (s *Server) handleMessage(ctx context.Context, raw []byte) *Response {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return newError(nil, CodeParseError, "parse error: "+err.Error())
	}

	if req.Method == "" {
		return newError(req.ID, CodeInvalidRequest, "method is required")
	}

	handler, ok := s.methods[req.Method]
	if !ok {
		if req.ID == nil {
			slog.Warn("app-server: notification for unregistered method (dropped)",
				"method", req.Method,
				"params_len", len(req.Params),
			)
			return nil
		}
		slog.Warn("app-server: request for unregistered method",
			"method", req.Method,
			"id", req.ID,
		)
		return newError(req.ID, CodeMethodNotFound, "method not found: "+req.Method)
	}

	result, err := handler(ctx, req.Params)
	if err != nil {
		if req.ID == nil {
			slog.Warn("app-server: notification handler error (no response sent)",
				"method", req.Method,
				"error", err,
			)
			return nil
		}
		slog.Warn("app-server: request handler error",
			"method", req.Method,
			"id", req.ID,
			"error", err,
		)
		return newError(req.ID, CodeInternalError, err.Error())
	}

	// JSON-RPC 2.0: 通知 (id == nil) 不返回响应
	if req.ID == nil {
		return nil
	}

	return newResult(req.ID, result)
}

// AgentEventHandler 返回一个 codex.EventHandler，将 Agent 事件转为 JSON-RPC 通知/请求。
//
// 普通事件: 广播为通知 (无需客户端回复)。
// 审批事件: 发送 Server→Client 请求, 等待客户端回复, 回传 codex (§ 二)。
// 持久化: 异步写入 agent_messages 表 (不阻塞广播)。
func (s *Server) AgentEventHandler(agentID string) codex.EventHandler {
	return func(event codex.Event) {
		method := mapEventToMethod(event.Type)

		// 统一日志: 记录所有 codex 事件
		threadID := ""
		if proc := s.mgr.Get(agentID); proc != nil {
			threadID = proc.Client.GetThreadID()
		}
		slog.Info("codex event",
			logger.FieldSource, "codex",
			logger.FieldComponent, "event",
			logger.FieldAgentID, agentID,
			logger.FieldThreadID, threadID,
			logger.FieldEventType, event.Type,
		)

		// 异步持久化 (不阻塞通知广播)
		if s.msgStore != nil {
			go s.persistMessage(agentID, event, method)
		}

		// 构建通知参数: threadId 始终在顶层以便前端路由
		payload := map[string]any{
			"threadId": agentID,
		}

		// 从 event.Data 提取前端常用字段到顶层
		if len(event.Data) > 0 {
			var dataMap map[string]any
			if json.Unmarshal(event.Data, &dataMap) == nil {
				for _, key := range []string{
					"delta", "content", "message", "command",
					"exit_code", "reason", "name", "status",
					"file", "diff", "tool_name",
				} {
					if v, ok := dataMap[key]; ok {
						payload[key] = v
					}
				}
			}
		}

		// § 二 审批事件: 需要客户端回复 (双向请求)
		switch event.Type {
		case "exec_approval_request":
			go s.handleApprovalRequest(agentID, "item/commandExecution/requestApproval", payload)
			return
		case "file_change_approval_request":
			go s.handleApprovalRequest(agentID, "item/fileChange/requestApproval", payload)
			return
		case codex.EventDynamicToolCall:
			go s.handleDynamicToolCall(agentID, event)
			return
		}

		// 普通事件: 广播通知
		s.Notify(method, payload)
	}
}

// persistMessage 异步写入消息到 agent_messages 表。
//
// 分类规则:
//
//	agent_message_delta / agent_message → role=assistant
//	exec_*/patch_*/mcp_*/dynamic_tool_call → role=tool
//	turn_started → role=user (但 content 需从 turn/start params 提取, 此处记空)
//	其他 → role=system
func (s *Server) persistMessage(agentID string, event codex.Event, method string) {
	role := classifyEventRole(event.Type)
	content := extractEventContent(event)

	msg := &store.AgentMessage{
		AgentID:   agentID,
		Role:      role,
		EventType: event.Type,
		Method:    method,
		Content:   content,
		Metadata:  event.Data,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.msgStore.Insert(ctx, msg); err != nil {
		slog.Warn("app-server: persist message failed",
			"agent", agentID,
			"event_type", event.Type,
			"error", err,
		)
	}
}

// PersistUserMessage 持久化用户消息 (从 turn/start 调用)。
func (s *Server) PersistUserMessage(agentID, prompt string) {
	if s.msgStore == nil {
		return
	}

	msg := &store.AgentMessage{
		AgentID:   agentID,
		Role:      "user",
		EventType: "user_message",
		Method:    "turn/start",
		Content:   prompt,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.msgStore.Insert(ctx, msg); err != nil {
		slog.Warn("app-server: persist user message failed",
			"agent", agentID,
			"error", err,
		)
	}
}

// classifyEventRole 按事件类型归类消息角色。
func classifyEventRole(eventType string) string {
	switch {
	case strings.Contains(eventType, "agent_message"):
		return "assistant"
	case strings.Contains(eventType, "reasoning"):
		return "assistant"
	case strings.Contains(eventType, "exec_"), strings.Contains(eventType, "patch_"),
		strings.Contains(eventType, "mcp_"), strings.Contains(eventType, "dynamic_tool"):
		return "tool"
	case eventType == "turn_started":
		return "user"
	default:
		return "system"
	}
}

// extractEventContent 从事件数据提取文本内容。
func extractEventContent(event codex.Event) string {
	if len(event.Data) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(event.Data, &m); err != nil {
		return ""
	}
	// 优先提取常见文本字段
	for _, key := range []string{"delta", "content", "message", "command", "diff", "text"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

// handleApprovalRequest 处理审批事件: Server→Client 请求 → 等回复 → 回传 codex。
func (s *Server) handleApprovalRequest(agentID, method string, payload map[string]any) {
	resp, err := s.SendRequestToAll(method, payload)
	if err != nil {
		slog.Warn("app-server: approval request failed", "agent", agentID, "error", err)
		return
	}

	// 解析客户端的审批决定
	approved := false
	if resp.Result != nil {
		if m, ok := resp.Result.(map[string]any); ok {
			if v, ok := m["approved"]; ok {
				approved, _ = v.(bool)
			}
		}
	}

	// 回传给 codex agent
	proc := s.mgr.Get(agentID)
	if proc == nil {
		return
	}
	decision := "no"
	if approved {
		decision = "yes"
	}
	if err := proc.Client.Submit(decision, nil, nil, nil); err != nil {
		slog.Warn("app-server: relay approval to codex failed", "agent", agentID, "error", err)
	}
}

// ========================================
// LSP Dynamic Tools
// ========================================

// SetupLSP 初始化 LSP 事件转发: 诊断缓存 + 广播。
func (s *Server) SetupLSP(rootDir string) {
	if s.lsp == nil {
		return
	}
	if rootDir != "" {
		s.lsp.SetRootURI("file://" + rootDir)
	}
	s.lsp.SetDiagnosticHandler(func(uri string, diagnostics []lsp.Diagnostic) {
		s.diagMu.Lock()
		if len(diagnostics) == 0 {
			delete(s.diagCache, uri)
		} else {
			s.diagCache[uri] = diagnostics
		}
		s.diagMu.Unlock()

		// 广播诊断通知给前端
		items := make([]map[string]any, 0, len(diagnostics))
		for _, d := range diagnostics {
			items = append(items, map[string]any{
				"message":  d.Message,
				"severity": d.Severity.String(),
				"line":     d.Range.Start.Line,
				"column":   d.Range.Start.Character,
			})
		}
		s.Notify("lsp/diagnostics/published", map[string]any{
			"uri":         uri,
			"diagnostics": items,
		})
	})
}

// buildLSPDynamicTools 构建 LSP 动态工具列表 (注入 codex agent)。
func (s *Server) buildLSPDynamicTools() []codex.DynamicTool {
	if s.lsp == nil {
		return nil
	}
	return []codex.DynamicTool{
		{
			Name:        "lsp_hover",
			Description: "Get type info and documentation for a symbol at a specific position in a file via LSP hover.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
					"line":      map[string]any{"type": "number", "description": "0-indexed line number"},
					"column":    map[string]any{"type": "number", "description": "0-indexed column number"},
				},
				"required": []string{"file_path", "line", "column"},
			},
		},
		{
			Name:        "lsp_open_file",
			Description: "Open a file for LSP analysis. Triggers didOpen and starts diagnostics. Call before hover/diagnostics for accurate results.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "lsp_diagnostics",
			Description: "Get current diagnostics (errors, warnings) for a file. The file should be opened with lsp_open_file first.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Absolute or relative path to the file. Empty = all files."},
				},
			},
		},
	}
}

// handleDynamicToolCall 处理 codex 发回的动态工具调用 — 调 LSP 并回传结果。
func (s *Server) handleDynamicToolCall(agentID string, event codex.Event) {
	// codex 事件信封: {"id": "...", "msg": {DynamicToolCallParams}, "conversationId": "..."}
	// 先提取 msg 字段, 再解析工具调用参数。
	var envelope struct {
		Msg json.RawMessage `json:"msg"`
	}
	raw := event.Data
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Msg) > 0 {
		raw = envelope.Msg
	}

	var call codex.DynamicToolCallData
	if err := json.Unmarshal(raw, &call); err != nil {
		slog.Warn("app-server: bad dynamic_tool_call data", "agent", agentID, "error", err,
			"raw", string(event.Data))
		return
	}

	proc := s.mgr.Get(agentID)
	if proc == nil {
		return
	}

	// ── 可观测性: 计数 + 日志 ──
	start := time.Now()
	s.toolCallMu.Lock()
	s.toolCallCount[call.Tool]++
	count := s.toolCallCount[call.Tool]
	s.toolCallMu.Unlock()

	slog.Info("dynamic-tool: called",
		"agent", agentID,
		"tool", call.Tool,
		"call_id", call.CallID,
		"total_calls", count,
	)

	var result string

	switch call.Tool {
	case "lsp_hover":
		result = s.lspHover(call.Arguments)
	case "lsp_open_file":
		result = s.lspOpenFile(call.Arguments)
	case "lsp_diagnostics":
		result = s.lspDiagnostics(call.Arguments)
	// ── 编排工具 ──
	case "orchestration_list_agents":
		result = s.orchestrationListAgents()
	case "orchestration_send_message":
		result = s.orchestrationSendMessage(call.Arguments)
	case "orchestration_launch_agent":
		result = s.orchestrationLaunchAgent(call.Arguments)
	case "orchestration_stop_agent":
		result = s.orchestrationStopAgent(call.Arguments)
	// ── 资源工具 ──
	case "task_create_dag":
		result = s.resourceTaskCreateDAG(call.Arguments)
	case "task_get_dag":
		result = s.resourceTaskGetDAG(call.Arguments)
	case "task_update_node":
		result = s.resourceTaskUpdateNode(call.Arguments)
	case "command_list":
		result = s.resourceCommandList(call.Arguments)
	case "command_get":
		result = s.resourceCommandGet(call.Arguments)
	case "prompt_list":
		result = s.resourcePromptList(call.Arguments)
	case "prompt_get":
		result = s.resourcePromptGet(call.Arguments)
	case "shared_file_read":
		result = s.resourceSharedFileRead(call.Arguments)
	case "shared_file_write":
		result = s.resourceSharedFileWrite(call.Arguments)
	default:
		result = fmt.Sprintf("unknown tool: %s", call.Tool)
	}

	elapsed := time.Since(start)

	slog.Info("dynamic-tool: completed",
		logger.FieldSource, "codex",
		logger.FieldComponent, "tool_call",
		logger.FieldAgentID, agentID,
		logger.FieldToolName, call.Tool,
		logger.FieldDurationMS, elapsed.Milliseconds(),
		logger.FieldEventType, "dynamic_tool_call",
		"result_len", len(result),
	)

	// 广播到前端 — 让 UI 可以显示 LSP 调用
	s.Notify("dynamic-tool/called", map[string]any{
		"agent":      agentID,
		"tool":       call.Tool,
		"callId":     call.CallID,
		"totalCalls": count,
		"elapsedMs":  elapsed.Milliseconds(),
		"resultLen":  len(result),
	})

	// 回传结果: 使用 event.RequestID 发送 JSON-RPC response (codex 发的是 server request)
	if err := proc.Client.SendDynamicToolResult(call.CallID, result, event.RequestID); err != nil {
		slog.Warn("app-server: send tool result failed", "agent", agentID, "tool", call.Tool, "error", err)
	}
}

// lspHover 调用 LSP hover。
func (s *Server) lspHover(args json.RawMessage) string {
	var p struct {
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	result, err := s.lsp.Hover(p.FilePath, p.Line, p.Column)
	if err != nil {
		return "error: " + err.Error()
	}
	if result == nil {
		return "no hover info available"
	}
	return result.Contents.Value
}

// lspOpenFile 打开文件触发 LSP 分析。
func (s *Server) lspOpenFile(args json.RawMessage) string {
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	content, err := os.ReadFile(p.FilePath)
	if err != nil {
		return "error reading file: " + err.Error()
	}
	if err := s.lsp.OpenFile(p.FilePath, string(content)); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("opened %s (%d bytes)", p.FilePath, len(content))
}

// lspDiagnostics 返回文件诊断信息。
func (s *Server) lspDiagnostics(args json.RawMessage) string {
	var p struct {
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal(args, &p)

	s.diagMu.RLock()
	defer s.diagMu.RUnlock()

	if p.FilePath != "" {
		uri := p.FilePath
		if !strings.HasPrefix(uri, "file://") {
			abs, _ := filepath.Abs(uri)
			uri = "file://" + abs
		}
		diags, ok := s.diagCache[uri]
		if !ok || len(diags) == 0 {
			return "no diagnostics"
		}
		var sb strings.Builder
		for _, d := range diags {
			fmt.Fprintf(&sb, "%s:%d:%d %s\n", p.FilePath, d.Range.Start.Line+1, d.Range.Start.Character, d.Message)
		}
		return sb.String()
	}

	// 所有文件
	if len(s.diagCache) == 0 {
		return "no diagnostics"
	}
	var sb strings.Builder
	for uri, diags := range s.diagCache {
		for _, d := range diags {
			fmt.Fprintf(&sb, "%s:%d:%d %s\n", uri, d.Range.Start.Line+1, d.Range.Start.Character, d.Message)
		}
	}
	return sb.String()
}

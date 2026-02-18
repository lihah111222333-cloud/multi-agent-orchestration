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
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
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
	mgr      *runner.AgentManager
	lsp      *lsp.Manager
	cfg      *config.Config
	methods  map[string]Handler
	dynTools map[string]func(json.RawMessage) string // 动态工具注册表

	// 消息持久化
	msgStore *store.AgentMessageStore

	// 资源 Store (编排工具依赖)
	dagStore          *store.TaskDAGStore
	cmdStore          *store.CommandCardStore
	promptStore       *store.PromptTemplateStore
	fileStore         *store.SharedFileStore
	workspaceRunStore *store.WorkspaceRunStore
	sysLogStore       *store.SystemLogStore

	// Dashboard Store (JSON-RPC dashboard/* 方法)
	agentStatusStore *store.AgentStatusStore
	auditLogStore    *store.AuditLogStore
	aiLogStore       *store.AILogStore
	busLogStore      *store.BusLogStore
	taskAckStore     *store.TaskAckStore
	taskTraceStore   *store.TaskTraceStore
	skillSvc         *service.SkillService
	workspaceMgr     *service.WorkspaceManager
	prefManager      *uistate.PreferenceManager
	uiRuntime        *uistate.RuntimeManager

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

	// 文件变更跟踪 (threadId → 当前变更文件列表)
	fileChangeMu       sync.Mutex
	fileChangeByThread map[string][]string

	// Per-session 技能配置 (agentID → skills 列表)
	skillsMu    sync.RWMutex
	agentSkills map[string][]string // agentID → ["skill1", "skill2"]

	// SSE 客户端 (debug 模式浏览器事件推送)
	sseMu      sync.RWMutex
	sseClients map[chan []byte]struct{}

	// 通知钩子 (给桌面端桥接使用)
	notifyHookMu sync.RWMutex
	notifyHook   func(method string, params any)

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
		mgr:                deps.Manager,
		lsp:                deps.LSP,
		cfg:                deps.Config,
		methods:            make(map[string]Handler),
		dynTools:           make(map[string]func(json.RawMessage) string),
		conns:              make(map[string]*connEntry),
		pending:            make(map[int64]chan *Response),
		diagCache:          make(map[string][]lsp.Diagnostic),
		toolCallCount:      make(map[string]int64),
		fileChangeByThread: make(map[string][]string),
		agentSkills:        make(map[string][]string),
		sseClients:         make(map[chan []byte]struct{}),
		prefManager:        uistate.NewPreferenceManager(nil),
		uiRuntime:          uistate.NewRuntimeManager(),
		upgrader: websocket.Upgrader{
			CheckOrigin: checkLocalOrigin,
		},
	}
	if deps.DB != nil {
		s.prefManager = uistate.NewPreferenceManager(store.NewUIPreferenceStore(deps.DB))
		s.msgStore = store.NewAgentMessageStore(deps.DB)
		s.dagStore = store.NewTaskDAGStore(deps.DB)
		s.cmdStore = store.NewCommandCardStore(deps.DB)
		s.promptStore = store.NewPromptTemplateStore(deps.DB)
		s.fileStore = store.NewSharedFileStore(deps.DB)
		s.workspaceRunStore = store.NewWorkspaceRunStore(deps.DB)
		s.sysLogStore = store.NewSystemLogStore(deps.DB)
		// Dashboard stores
		s.agentStatusStore = store.NewAgentStatusStore(deps.DB)
		s.auditLogStore = store.NewAuditLogStore(deps.DB)
		s.aiLogStore = store.NewAILogStore(deps.DB)
		s.busLogStore = store.NewBusLogStore(deps.DB)
		s.taskAckStore = store.NewTaskAckStore(deps.DB)
		s.taskTraceStore = store.NewTaskTraceStore(deps.DB)

		if s.cfg != nil {
			maxFileBytes := int64(s.cfg.OrchestrationWorkspaceMaxFileBytes)
			maxTotalBytes := int64(s.cfg.OrchestrationWorkspaceMaxTotalBytes)
			workspaceMgr, mgrErr := service.NewWorkspaceManager(
				s.workspaceRunStore,
				s.cfg.OrchestrationWorkspaceRoot,
				s.cfg.OrchestrationWorkspaceMaxFiles,
				maxFileBytes,
				maxTotalBytes,
			)
			if mgrErr != nil {
				logger.Warn("app-server: workspace manager unavailable", logger.FieldError, mgrErr)
			} else {
				s.workspaceMgr = workspaceMgr
				logger.Info("app-server: workspace manager enabled", logger.FieldRoot, workspaceMgr.RootDir())
			}
		}
		logger.Info("app-server: message persistence + resource tools + dashboard enabled")
	}
	// Skills service (filesystem, no DB required)
	skillsDir := deps.SkillsDir
	if skillsDir == "" {
		skillsDir = ".agent/skills"
	}
	s.skillSvc = service.NewSkillService(skillsDir)
	s.registerMethods()
	s.registerDynamicTools()
	return s
}

// registerDynamicTools 注册所有动态工具处理函数。
//
// 新增工具只需一行: s.dynTools["tool_name"] = s.toolHandler
func (s *Server) registerDynamicTools() {
	// LSP 工具
	s.dynTools["lsp_hover"] = s.lspHover
	s.dynTools["lsp_open_file"] = s.lspOpenFile
	s.dynTools["lsp_diagnostics"] = s.lspDiagnostics

	// 编排工具
	s.dynTools["orchestration_list_agents"] = func(_ json.RawMessage) string { return s.orchestrationListAgents() }
	s.dynTools["orchestration_send_message"] = s.orchestrationSendMessage
	s.dynTools["orchestration_launch_agent"] = s.orchestrationLaunchAgent
	s.dynTools["orchestration_stop_agent"] = s.orchestrationStopAgent

	// 资源工具
	s.dynTools["task_create_dag"] = s.resourceTaskCreateDAG
	s.dynTools["task_get_dag"] = s.resourceTaskGetDAG
	s.dynTools["task_update_node"] = s.resourceTaskUpdateNode
	s.dynTools["command_list"] = s.resourceCommandList
	s.dynTools["command_get"] = s.resourceCommandGet
	s.dynTools["prompt_list"] = s.resourcePromptList
	s.dynTools["prompt_get"] = s.resourcePromptGet
	s.dynTools["shared_file_read"] = s.resourceSharedFileRead
	s.dynTools["shared_file_write"] = s.resourceSharedFileWrite
	s.dynTools["workspace_create_run"] = s.resourceWorkspaceCreateRun
	s.dynTools["workspace_get_run"] = s.resourceWorkspaceGetRun
	s.dynTools["workspace_list_runs"] = s.resourceWorkspaceListRuns
	s.dynTools["workspace_merge_run"] = s.resourceWorkspaceMergeRun
	s.dynTools["workspace_abort_run"] = s.resourceWorkspaceAbortRun
}

// InvokeMethod 内部调用 JSON-RPC 方法 (不经过 WebSocket)。
//
// 用于 Wails UI 等 Go 进程内客户端直接调用后端功能。
// 复用 dispatchRequest 统一分发逻辑 (DRY)。
func (s *Server) InvokeMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	resp := s.dispatchRequest(ctx, 1, method, params)
	if resp == nil {
		return nil, nil
	}
	if resp.Error != nil {
		return nil, pkgerr.Newf("Server.InvokeMethod", "%s (code %d)", resp.Error.Message, resp.Error.Code)
	}
	return resp.Result, nil
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
	logger.Warn("app-server: rejected non-local origin", logger.FieldOrigin, origin)
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
	mux.HandleFunc("/", s.handleUpgrade)    // WebSocket
	mux.HandleFunc("/rpc", s.handleHTTPRPC) // HTTP JSON-RPC (调试模式)
	mux.HandleFunc("/events", s.handleSSE)  // SSE 事件流 (调试模式)

	srv := &http.Server{
		Addr:        host,
		Handler:     recoveryMiddleware(corsMiddleware(mux)),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	// 优雅关闭: 给活跃连接 5 秒完成处理
	util.SafeGo(func() {
		<-ctx.Done()
		logger.Info("app-server: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("app-server: shutdown error", logger.FieldError, err)
		}
	})

	logger.Info("app-server: listening", logger.FieldAddr, host)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return pkgerr.Wrap(err, "Server.ListenAndServe", "listen")
	}
	return nil
}

// SetNotifyHook 设置 Notify 事件钩子。
//
// 用于桌面端桥接: apiserver 事件 -> Wails runtime event。
func (s *Server) SetNotifyHook(h func(method string, params any)) {
	s.notifyHookMu.Lock()
	s.notifyHook = h
	s.notifyHookMu.Unlock()
}

// Notify 向所有连接广播 JSON-RPC 通知 (WebSocket + SSE)。
func (s *Server) Notify(method string, params any) {
	s.syncUIRuntimeFromNotify(method, params)

	s.notifyHookMu.RLock()
	hook := s.notifyHook
	s.notifyHookMu.RUnlock()
	if hook != nil {
		hook(method, params)
	}

	notif := newNotification(method, params)
	data, err := json.Marshal(notif)
	if err != nil {
		return
	}

	// SSE 广播 — 将事件推给浏览器调试客户端
	s.sseMu.RLock()
	sseCount := len(s.sseClients)
	if sseCount > 0 {
		logger.Debug("sse: broadcasting", logger.FieldMethod, method, "clients", sseCount, logger.FieldDataLen, len(data))
		for ch := range s.sseClients {
			select {
			case ch <- data:
			default:
				// 客户端跟不上, 丢弃 (非关键)
				logger.Warn("sse: client channel full, dropping event")
			}
		}
	}
	s.sseMu.RUnlock()

	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, entry := range s.conns {
		if err := entry.writeMsg(websocket.TextMessage, data); err != nil {
			logger.Warn("app-server: notify failed", logger.FieldConn, id, logger.FieldError, err)
		}
	}
}

func (s *Server) syncUIRuntimeFromNotify(method string, params any) {
	if s.uiRuntime == nil {
		return
	}
	payload := util.ToMapAny(params)
	switch method {
	case "workspace/run/created", "workspace/run/aborted":
		run := util.ToMapAny(payload["run"])
		if len(run) == 0 {
			return
		}
		s.uiRuntime.UpsertWorkspaceRun(run)
	case "workspace/run/merged":
		runKey, _ := payload["runKey"].(string)
		result := util.ToMapAny(payload["result"])
		if len(result) == 0 {
			return
		}
		s.uiRuntime.ApplyWorkspaceMergeResult(runKey, result)
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
			return nil, pkgerr.Wrap(err, "Server.SendRequest", "marshal params")
		}
		req.Params = raw
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, pkgerr.Wrap(err, "Server.SendRequest", "marshal request")
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
		return nil, pkgerr.Newf("Server.SendRequest", "connection %s not found", connID)
	}

	if err := entry.writeMsg(websocket.TextMessage, data); err != nil {
		return nil, pkgerr.Wrapf(err, "Server.SendRequest", "write to %s", connID)
	}

	logger.Info("app-server: sent request to client", logger.FieldConn, connID, logger.FieldMethod, method, logger.FieldID, reqID)

	// 等待客户端响应 (5 分钟超时)
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(5 * time.Minute):
		return nil, pkgerr.Newf("Server.SendRequest", "request %d timed out waiting for client response", reqID)
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
		return nil, pkgerr.New("Server.SendRequestToAll", "no connected clients")
	}
	return s.SendRequest(firstConn, method, params)
}

// handleUpgrade HTTP → WebSocket 升级。
func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	// 连接数限制
	s.mu.RLock()
	numConns := len(s.conns)
	s.mu.RUnlock()
	if numConns >= maxConnections {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		logger.Warn("app-server: connection rejected (max reached)", logger.FieldMax, maxConnections)
		return
	}

	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("app-server: upgrade failed", logger.FieldError, err)
		return
	}

	// 消息大小限制
	ws.SetReadLimit(maxMessageSize)

	connID := fmt.Sprintf("conn-%d", s.nextID.Add(1))
	entry := &connEntry{ws: ws}
	s.mu.Lock()
	s.conns[connID] = entry
	s.mu.Unlock()

	logger.Info("app-server: client connected", logger.FieldConn, connID, logger.FieldRemote, r.RemoteAddr)

	defer func() {
		s.mu.Lock()
		delete(s.conns, connID)
		s.mu.Unlock()
		_ = ws.Close()
		logger.Info("app-server: client disconnected", logger.FieldConn, connID)
	}()

	s.readLoop(r.Context(), entry, connID)
}

// rpcEnvelope 统一信封: 一次 Unmarshal 路由所有消息类型。
//
// 所有字段使用 json.RawMessage 延迟解析, 避免任何中间分配:
//   - ID: 保留原始 JSON 字节，response 分支直接解析为 int64 (零 alloc)
//   - Params/Result/Error: 按需解析
type rpcEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// parseIntID 从 JSON 原始字节直接解析 int64，无需 json.Unmarshal。
//
// 仅处理纯整数 "123"，不处理浮点/字符串/null。
// 高并发热路径: 零分配、零反射。
func parseIntID(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	// 快速路径: 纯数字字节 (JSON integer)
	var n int64
	neg := false
	i := 0
	if raw[0] == '-' {
		neg = true
		i = 1
	}
	if i >= len(raw) {
		return 0, false
	}
	for ; i < len(raw); i++ {
		c := raw[i]
		if c < '0' || c > '9' {
			return 0, false // 非整数 (浮点/字符串/对象)
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

// rawIDtoAny 将 json.RawMessage ID 转换为 Go any 值。
//
// 用于 dispatchRequest 路径 (需要 any 类型 ID 传给 Response)。
func rawIDtoAny(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	// 优先尝试 int64 (我们自己的 ID 都是整数)
	if n, ok := parseIntID(raw); ok {
		return n
	}
	// fallback: 字符串 ID ("abc")
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		logger.Debug("app-server: rawIDtoAny unmarshal", logger.FieldError, err)
	}
	return v
}

// readLoop 持续读取 WebSocket 消息并分发。
//
// 消息分为三类:
//  1. Client→Server 请求 (有 method + id): 路由到 dispatchRequest
//  2. Client→Server 通知 (有 method, 无 id): 路由到 dispatchRequest
//  3. Client 对 Server 请求的响应 (有 id, 无 method): 直接匹配 pending map
func (s *Server) readLoop(ctx context.Context, entry *connEntry, connID string) {
	for {
		_, message, err := entry.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logger.Warn("app-server: read error", logger.FieldConn, connID, logger.FieldError, err)
			}
			return
		}

		// 单次 Unmarshal: 路由 + 延迟解析
		var env rpcEnvelope
		if err := json.Unmarshal(message, &env); err != nil {
			data, _ := json.Marshal(newError(nil, CodeParseError, "parse error: "+err.Error()))
			_ = entry.writeMsg(websocket.TextMessage, data)
			continue
		}

		// 快速路径: 客户端响应 (有 id + 无 method + 有 result/error)
		// 直接从 raw bytes 解析 int64 ID → pending map 查找, 零 alloc
		if len(env.ID) > 0 && string(env.ID) != "null" && env.Method == "" && (len(env.Result) > 0 || len(env.Error) > 0) {
			if reqID, ok := parseIntID(env.ID); ok {
				s.pendingMu.Lock()
				ch, found := s.pending[reqID]
				s.pendingMu.Unlock()

				if found {
					resp := &Response{
						JSONRPC: jsonrpcVersion,
						ID:      reqID,
					}
					if len(env.Result) > 0 {
						var result any
						if err := json.Unmarshal(env.Result, &result); err != nil {
							logger.Warn("app-server: unmarshal client response result", logger.FieldError, err)
						}
						resp.Result = result
					}
					if len(env.Error) > 0 {
						var rpcErr RPCError
						if err := json.Unmarshal(env.Error, &rpcErr); err != nil {
							logger.Warn("app-server: unmarshal client response error", logger.FieldError, err)
						}
						resp.Error = &rpcErr
					}
					select {
					case ch <- resp:
					default:
					}
					continue
				}
			}
		}

		// 正常请求/通知: 复用已解析的字段
		resp := s.handleParsedMessage(ctx, env)
		if resp == nil {
			continue
		}

		data, err := json.Marshal(resp)
		if err != nil {
			logger.Error("app-server: marshal response failed", logger.FieldError, err)
			continue
		}

		if err := entry.writeMsg(websocket.TextMessage, data); err != nil {
			logger.Warn("app-server: write failed", logger.FieldConn, connID, logger.FieldError, err)
			return
		}
	}
}

// handleParsedMessage 复用已解析的 rpcEnvelope 分发请求 (避免二次 Unmarshal)。
func (s *Server) handleParsedMessage(ctx context.Context, env rpcEnvelope) *Response {
	return s.dispatchRequest(ctx, rawIDtoAny(env.ID), env.Method, env.Params)
}

// dispatchRequest 统一的方法分发逻辑。
func (s *Server) dispatchRequest(ctx context.Context, id any, method string, params json.RawMessage) *Response {
	if method == "" {
		return newError(id, CodeInvalidRequest, "method is required")
	}

	handler, ok := s.methods[method]
	if !ok {
		if id == nil {
			logger.Warn("app-server: notification for unregistered method (dropped)",
				logger.FieldMethod, method,
				logger.FieldParamsLen, len(params),
			)
			return nil
		}
		logger.Warn("app-server: request for unregistered method",
			logger.FieldMethod, method,
			logger.FieldID, id,
		)
		return newError(id, CodeMethodNotFound, "method not found: "+method)
	}

	result, err := handler(ctx, params)
	if err != nil {
		if id == nil {
			logger.Warn("app-server: notification handler error (no response sent)",
				logger.FieldMethod, method,
				logger.FieldError, err,
			)
			return nil
		}
		logger.Warn("app-server: request handler error",
			logger.FieldMethod, method,
			logger.FieldID, id,
			logger.FieldError, err,
		)
		return newError(id, CodeInternalError, err.Error())
	}

	// JSON-RPC 2.0: 通知 (id == nil) 不返回响应
	if id == nil {
		return nil
	}

	return newResult(id, result)
}

var payloadExtractKeys = []string{
	// legacy fields
	"delta", "content", "message", "command",
	"exit_code", "reason", "name", "status",
	"file", "files", "diff", "tool_name",
	// v2 protocol fields
	"text", "summary", "args", "arguments", "output",
	"id", "type", "item_id", "callId", "call_id",
	"file_path", "path", "chunk", "stream",
}

func mergePayloadFromMap(payload map[string]any, data map[string]any) {
	if data == nil {
		return
	}

	for _, key := range payloadExtractKeys {
		v, ok := data[key]
		if !ok {
			continue
		}
		payload[key] = v
	}

	if v, ok := data["call_id"]; ok {
		if _, exists := payload["id"]; !exists {
			payload["id"] = v
		}
	}
	if v, ok := data["item_id"]; ok {
		if _, exists := payload["id"]; !exists {
			payload["id"] = v
		}
	}
	if v, ok := data["file_path"]; ok {
		if _, exists := payload["file"]; !exists {
			payload["file"] = v
		}
	}
	if v, ok := data["path"]; ok {
		if _, exists := payload["file"]; !exists {
			payload["file"] = v
		}
	}
}

// walkNestedJSON 遍历 msg/data/payload 嵌套层, 对每个解析出的 map[string]any 调用 fn。
//
// 统一处理四种嵌套类型: map[string]any / string / json.RawMessage / []byte。
// mergePayloadFields 和 extractEventContent 共用此逻辑。
func walkNestedJSON(m map[string]any, fn func(map[string]any)) {
	for _, key := range []string{"msg", "data", "payload"} {
		v, ok := m[key]
		if !ok {
			continue
		}
		switch nested := v.(type) {
		case map[string]any:
			fn(nested)
		case string:
			var nm map[string]any
			if json.Unmarshal([]byte(nested), &nm) == nil {
				fn(nm)
			}
		case json.RawMessage:
			var nm map[string]any
			if json.Unmarshal(nested, &nm) == nil {
				fn(nm)
			}
		case []byte:
			var nm map[string]any
			if json.Unmarshal(nested, &nm) == nil {
				fn(nm)
			}
		}
	}
}

func mergePayloadFields(payload map[string]any, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}

	var dataMap map[string]any
	if err := json.Unmarshal(raw, &dataMap); err != nil {
		return
	}

	mergePayloadFromMap(payload, dataMap)
	walkNestedJSON(dataMap, func(nested map[string]any) {
		mergePayloadFromMap(payload, nested)
	})
}

func normalizeFiles(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		value := strings.TrimSpace(v)
		if value == "" {
			return nil
		}
		return []string{value}
	case []string:
		return uniqueStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return uniqueStrings(out)
	default:
		return nil
	}
}

func uniqueStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		s := strings.TrimSpace(item)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func parseFilesFromPatchDelta(delta string) []string {
	if delta == "" {
		return nil
	}
	lines := strings.Split(delta, "\n")
	files := make([]string, 0, 4)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "diff --git ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 4 {
				path := strings.TrimPrefix(parts[3], "b/")
				if path != "" {
					files = append(files, path)
				}
			}
			continue
		}
		if len(trimmed) > 2 && (strings.HasPrefix(trimmed, "M ") || strings.HasPrefix(trimmed, "A ") || strings.HasPrefix(trimmed, "D ")) {
			path := strings.TrimSpace(trimmed[2:])
			if path != "" {
				files = append(files, path)
			}
		}
	}
	return uniqueStrings(files)
}

func firstNonEmpty(files []string) string {
	for _, file := range files {
		if strings.TrimSpace(file) != "" {
			return strings.TrimSpace(file)
		}
	}
	return ""
}

func toolResultSuccess(result string) bool {
	value := strings.TrimSpace(strings.ToLower(result))
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "error") ||
		strings.HasPrefix(value, "failed") ||
		strings.HasPrefix(value, "unknown tool") {
		return false
	}
	if strings.HasPrefix(value, `{"error"`) ||
		strings.Contains(value, `"error":`) {
		return false
	}
	return true
}

func (s *Server) rememberFileChanges(threadID string, files []string) {
	if threadID == "" {
		return
	}
	files = uniqueStrings(files)
	if len(files) == 0 {
		return
	}
	s.fileChangeMu.Lock()
	defer s.fileChangeMu.Unlock()
	s.fileChangeByThread[threadID] = files
}

func (s *Server) consumeRememberedFileChanges(threadID string) []string {
	if threadID == "" {
		return nil
	}
	s.fileChangeMu.Lock()
	defer s.fileChangeMu.Unlock()
	files := s.fileChangeByThread[threadID]
	delete(s.fileChangeByThread, threadID)
	return append([]string(nil), files...)
}

func (s *Server) enrichFileChangePayload(threadID, eventType, method string, payload map[string]any) {
	if payload == nil {
		return
	}
	isFileChangeEvent := strings.Contains(strings.ToLower(eventType), "filechange") ||
		strings.Contains(strings.ToLower(eventType), "patch_apply")
	isFileChangeMethod := strings.Contains(method, "fileChange")
	if !isFileChangeEvent && !isFileChangeMethod {
		return
	}

	files := normalizeFiles(payload["files"])
	if len(files) == 0 {
		files = normalizeFiles(payload["file"])
	}
	if len(files) == 0 {
		delta := ""
		if value, ok := payload["delta"].(string); ok {
			delta = value
		} else if value, ok := payload["output"].(string); ok {
			delta = value
		}
		files = parseFilesFromPatchDelta(delta)
	}

	switch method {
	case "item/fileChange/outputDelta", "item/started":
		if len(files) > 0 {
			payload["files"] = files
			payload["file"] = files[0]
			payload["type"] = "fileChange"
			s.rememberFileChanges(threadID, files)
		}
	case "item/completed":
		if len(files) == 0 {
			files = s.consumeRememberedFileChanges(threadID)
		}
		if len(files) > 0 {
			payload["files"] = files
			payload["file"] = files[0]
			payload["type"] = "fileChange"
		}
	}
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
		logger.Debug("codex event",
			logger.FieldSource, "codex",
			logger.FieldComponent, "event",
			logger.FieldAgentID, agentID,
			logger.FieldThreadID, threadID,
			logger.FieldEventType, event.Type,
		)

		// 异步持久化 (不阻塞通知广播)
		if s.msgStore != nil {
			util.SafeGo(func() { s.persistMessage(agentID, event, method) })
		}

		// 构建通知参数: threadId 始终在顶层以便前端路由
		payload := map[string]any{
			"threadId": agentID,
		}

		// 从 event.Data 提取前端常用字段到顶层 (含嵌套 msg/data/payload)。
		mergePayloadFields(payload, event.Data)
		s.enrichFileChangePayload(agentID, event.Type, method, payload)

		// Normalize event for UI
		normalized := uistate.NormalizeEvent(event.Type, method, event.Data)
		payload["uiType"] = string(normalized.UIType)
		payload["uiStatus"] = string(normalized.UIStatus)
		if normalized.Text != "" {
			payload["uiText"] = normalized.Text
		}
		if normalized.Command != "" {
			payload["uiCommand"] = normalized.Command
		}
		if len(normalized.Files) > 0 {
			payload["uiFiles"] = normalized.Files
		}
		if normalized.ExitCode != nil {
			payload["uiExitCode"] = *normalized.ExitCode
		}
		if s.uiRuntime != nil {
			s.uiRuntime.ApplyAgentEvent(agentID, normalized, payload)
		}

		// § 二 审批事件: 需要客户端回复 (双向请求)
		switch event.Type {
		case "exec_approval_request":
			util.SafeGo(func() { s.handleApprovalRequest(agentID, "item/commandExecution/requestApproval", payload) })
			return
		case "file_change_approval_request":
			util.SafeGo(func() { s.handleApprovalRequest(agentID, "item/fileChange/requestApproval", payload) })
			return
		case codex.EventDynamicToolCall:
			util.SafeGo(func() { s.handleDynamicToolCall(agentID, event) })
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
	dedupKey := store.BuildMessageDedupKey(event.Type, method, event.Data)

	msg := &store.AgentMessage{
		AgentID:   agentID,
		Role:      role,
		EventType: event.Type,
		Method:    method,
		Content:   content,
		DedupKey:  dedupKey,
		Metadata:  event.Data,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.msgStore.Insert(ctx, msg); err != nil {
		logger.Warn("app-server: persist message failed",
			logger.FieldAgentID, agentID,
			logger.FieldEventType, event.Type,
			logger.FieldError, err,
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
		logger.Warn("app-server: persist user message failed",
			logger.FieldAgentID, agentID,
			logger.FieldError, err,
		)
	}
}

func (s *Server) persistSyntheticMessage(agentID, role, eventType, method, content string, metadata any) error {
	if s.msgStore == nil {
		return nil
	}

	var raw json.RawMessage
	if metadata != nil {
		data, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		raw = data
	}

	msg := &store.AgentMessage{
		AgentID:   agentID,
		Role:      role,
		EventType: eventType,
		Method:    method,
		Content:   content,
		DedupKey:  store.BuildMessageDedupKey(eventType, method, raw),
		Metadata:  raw,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.msgStore.Insert(ctx, msg)
}

// classifyEventRole 按事件类型归类消息角色。
func classifyEventRole(eventType string) string {
	t := strings.ToLower(eventType)

	switch {
	case strings.Contains(t, "agent_message"), strings.Contains(t, "agentmessage"):
		return "assistant"
	case strings.Contains(t, "reasoning"):
		return "assistant"
	case strings.Contains(t, "exec_"), strings.Contains(t, "patch_"),
		strings.Contains(t, "mcp_"), strings.Contains(t, "dynamic_tool"),
		strings.Contains(t, "commandexecution"), strings.Contains(t, "filechange"),
		strings.Contains(t, "dynamictool"), strings.Contains(t, "tool/call"):
		return "tool"
	case t == "turn_started", t == "turn/started", t == "user_message", t == "item/usermessage":
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

	extract := func(src map[string]any) string {
		for _, key := range []string{"delta", "content", "message", "command", "diff", "text", "summary", "output"} {
			if v, ok := src[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
		return ""
	}

	if s := extract(m); s != "" {
		return s
	}

	var result string
	walkNestedJSON(m, func(nested map[string]any) {
		if result == "" {
			result = extract(nested)
		}
	})
	return result
}

// handleApprovalRequest 处理审批事件: Server→Client 请求 → 等回复 → 回传 codex。
func (s *Server) handleApprovalRequest(agentID, method string, payload map[string]any) {
	resp, err := s.SendRequestToAll(method, payload)
	if err != nil {
		logger.Warn("app-server: approval request failed", logger.FieldAgentID, agentID, logger.FieldError, err)
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
		logger.Warn("app-server: relay approval to codex failed", logger.FieldAgentID, agentID, logger.FieldError, err)
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
	statuses := s.lsp.Statuses()
	hasAvailableServer := false
	for _, st := range statuses {
		if st.Available {
			hasAvailableServer = true
			break
		}
	}
	if !hasAvailableServer {
		logger.Info("lsp dynamic tools disabled: no language server available on PATH")
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
		logger.Warn("app-server: bad dynamic_tool_call data", logger.FieldAgentID, agentID, logger.FieldError, err,
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

	logger.Info("dynamic-tool: called",
		logger.FieldAgentID, agentID,
		logger.FieldToolName, call.Tool,
		"call_id", call.CallID,
		"total_calls", count,
	)

	var result string

	if handler, ok := s.dynTools[call.Tool]; ok {
		result = handler(call.Arguments)
	} else {
		result = fmt.Sprintf("unknown tool: %s", call.Tool)
	}

	elapsed := time.Since(start)
	success := toolResultSuccess(result)

	var argMap map[string]any
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &argMap); err != nil {
			logger.Debug("app-server: unmarshal tool arguments", logger.FieldToolName, call.Tool, logger.FieldError, err)
		}
	}
	filePath := ""
	if argMap != nil {
		for _, key := range []string{"file_path", "path", "file"} {
			if v, ok := argMap[key].(string); ok && strings.TrimSpace(v) != "" {
				filePath = strings.TrimSpace(v)
				break
			}
		}
	}

	logger.Info("dynamic-tool: completed",
		logger.FieldSource, "codex",
		logger.FieldComponent, "tool_call",
		logger.FieldAgentID, agentID,
		logger.FieldToolName, call.Tool,
		logger.FieldDurationMS, elapsed.Milliseconds(),
		logger.FieldEventType, "dynamic_tool_call",
		"result_len", len(result),
		"success", success,
	)

	// 广播到前端 — 让 UI 可以显示 LSP 调用
	notifyPayload := map[string]any{
		"threadId":   agentID,
		"agent":      agentID,
		"tool":       call.Tool,
		"callId":     call.CallID,
		"arguments":  argMap,
		"file":       filePath,
		"success":    success,
		"totalCalls": count,
		"elapsedMs":  elapsed.Milliseconds(),
		"resultLen":  len(result),
	}
	if result != "" {
		if len(result) > 500 {
			notifyPayload["resultPreview"] = result[:500]
		} else {
			notifyPayload["resultPreview"] = result
		}
	}
	s.Notify("dynamic-tool/called", notifyPayload)

	// 持久化可观测事件, 便于重启后回放工具调用链路。
	if s.msgStore != nil {
		content := result
		if content == "" {
			content = call.Tool
		}
		if err := s.persistSyntheticMessage(agentID, "tool", "dynamic-tool/called", "dynamic-tool/called", content, notifyPayload); err != nil {
			logger.Warn("app-server: persist dynamic-tool event failed", logger.FieldAgentID, agentID, logger.FieldToolName, call.Tool, logger.FieldError, err)
		}
	}

	// 回传结果: 使用 event.RequestID 发送 JSON-RPC response (codex 发的是 server request)
	if err := proc.Client.SendDynamicToolResult(call.CallID, result, event.RequestID); err != nil {
		logger.Warn("app-server: send tool result failed", logger.FieldAgentID, agentID, logger.FieldToolName, call.Tool, logger.FieldError, err)
	}
}

// lspHover 调用 LSP hover。
func (s *Server) lspHover(args json.RawMessage) string {
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	if strings.TrimSpace(p.FilePath) == "" {
		return "error: file_path is required"
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
	if s.lsp == nil {
		return "error: lsp manager unavailable"
	}
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: " + err.Error()
	}
	p.FilePath = strings.TrimSpace(p.FilePath)
	if p.FilePath == "" {
		return "error: file_path is required"
	}
	content, err := os.ReadFile(p.FilePath)
	if err != nil {
		return "error: reading file: " + err.Error()
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
	if err := json.Unmarshal(args, &p); err != nil {
		return "error: unmarshal diagnostics params: " + err.Error()
	}

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

// ========================================
// HTTP JSON-RPC (调试模式)
// ========================================

// handleHTTPRPC 处理 HTTP POST /rpc 请求 (调试模式用)。
//
// 接收标准 JSON-RPC 2.0 请求, 调用 InvokeMethod, 返回 JSON-RPC 响应。
func (s *Server) handleHTTPRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, -32700, "Parse error: "+err.Error())
		return
	}

	if req.Method == "" {
		writeJSONRPCError(w, req.ID, -32600, "Invalid Request: method is required")
		return
	}

	// 如果 params 为 null, 用空对象
	params := req.Params
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage("{}")
	}

	result, err := s.InvokeMethod(r.Context(), req.Method, params)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, err.Error())
		return
	}

	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"result":  result,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// writeJSONRPCError 写 JSON-RPC 错误响应。
func writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC 错误仍返回 200
	json.NewEncoder(w).Encode(resp)
}

// recoveryMiddleware 捕获 HTTP handler panic，防止单个请求崩溃导致整个服务端退出。
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				logger.Error("http: handler panicked",
					logger.FieldMethod, r.Method,
					logger.FieldPath, r.URL.Path,
					logger.FieldRemote, r.RemoteAddr,
					logger.FieldError, rv,
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware 添加 CORS 头 (调试模式允许跨域)。
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleSSE 处理 SSE 事件流 (debug 模式浏览器实时接收 agent 事件)。
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 64)

	s.sseMu.Lock()
	s.sseClients[ch] = struct{}{}
	s.sseMu.Unlock()

	defer func() {
		s.sseMu.Lock()
		delete(s.sseClients, ch)
		s.sseMu.Unlock()
	}()

	logger.Info("sse: client connected", logger.FieldRemote, r.RemoteAddr)

	for {
		select {
		case data := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			logger.Info("sse: client disconnected", logger.FieldRemote, r.RemoteAddr)
			return
		}
	}
}

// server_conn.go — WebSocket 连接管理、RPC 消息解析与方法分发。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type wsOutbound struct {
	msgType int
	data    []byte
}

// connEntry WebSocket 连接 + 写锁 (gorilla/websocket 不安全并发写)。
type connEntry struct {
	ws        *websocket.Conn
	wrMu      sync.Mutex // 序列化所有写操作
	outbox    chan wsOutbound
	closeCh   chan struct{}
	closeOnce sync.Once
}

func newConnEntry(ws *websocket.Conn) *connEntry {
	return &connEntry{
		ws:      ws,
		outbox:  make(chan wsOutbound, connOutboxSize),
		closeCh: make(chan struct{}),
	}
}

// writeMsg 线程安全地写入 WebSocket 消息。
func (c *connEntry) writeMsg(msgType int, data []byte) error {
	c.wrMu.Lock()
	defer c.wrMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteMessage(msgType, data)
}

func (c *connEntry) enqueue(msgType int, data []byte) bool {
	select {
	case <-c.closeCh:
		return false
	default:
	}
	select {
	case c.outbox <- wsOutbound{msgType: msgType, data: data}:
		return true
	default:
		return false
	}
}

func (c *connEntry) outboxDepth() int {
	return len(c.outbox)
}

func (c *connEntry) closeNow() {
	c.closeOnce.Do(func() {
		close(c.closeCh)
		if c.ws != nil {
			_ = c.ws.Close()
		}
	})
}

func (c *connEntry) writeLoop() error {
	for {
		select {
		case <-c.closeCh:
			return nil
		case msg := <-c.outbox:
			if err := c.writeMsg(msg.msgType, msg.data); err != nil {
				return err
			}
		}
	}
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

func (s *Server) broadcastNotification(method string, params any) {
	s.notifyHookMu.RLock()
	hook := s.notifyHook
	s.notifyHookMu.RUnlock()
	if hook != nil {
		hook(method, params)
	}

	notif := newNotification(method, params)
	data, err := json.Marshal(notif)
	if err != nil {
		logger.Error("app-server: marshal notification failed", logger.FieldMethod, method, logger.FieldError, err)
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
	snapshot := make(map[string]*connEntry, len(s.conns))
	for id, entry := range s.conns {
		snapshot[id] = entry
	}
	s.mu.RUnlock()
	for id, entry := range snapshot {
		s.enqueueConnMessage(id, entry, websocket.TextMessage, data, "notify_backpressure")
	}
}

func (s *Server) enqueueConnMessage(connID string, entry *connEntry, msgType int, data []byte, reason string) bool {
	if entry == nil {
		return false
	}
	if entry.enqueue(msgType, data) {
		return true
	}
	logger.Warn("app-server: client send queue overloaded, disconnecting",
		logger.FieldConn, connID,
		"reason", strings.TrimSpace(reason),
		"outbox_depth", entry.outboxDepth(),
		"outbox_cap", connOutboxSize,
	)
	s.disconnectConn(connID)
	return false
}

func (s *Server) disconnectConn(connID string) {
	id := strings.TrimSpace(connID)
	if id == "" {
		return
	}
	s.mu.Lock()
	entry, ok := s.conns[id]
	if ok {
		delete(s.conns, id)
	}
	s.mu.Unlock()
	if ok && entry != nil {
		entry.closeNow()
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

	if !s.enqueueConnMessage(connID, entry, websocket.TextMessage, data, "server_request_backpressure") {
		return nil, pkgerr.Newf("Server.SendRequest", "connection %s overloaded; retry later", connID)
	}

	logger.Info("app-server: sent request to client", logger.FieldConn, connID, logger.FieldMethod, method, logger.FieldID, reqID)

	// 等待客户端响应 (5 分钟超时)
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
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

// ResolvePendingRequest 由 Wails 前端调用, 将审批结果注入 pending channel。
//
// 当前端通过 CallAPI("approval/respond", {requestId, approved}) 回复审批时,
// app.go 调用此方法将结果写入 handleApprovalRequest 正在等待的 channel。
//
// 参数:
//   - reqID: handleApprovalRequest 分配的请求 ID
//   - result: 审批结果 (如 {"approved": true})
//
// 返回 true 表示找到对应 pending 请求并已投递。
func (s *Server) ResolvePendingRequest(reqID int64, result map[string]any) bool {
	s.pendingMu.Lock()
	ch, ok := s.pending[reqID]
	s.pendingMu.Unlock()
	if !ok {
		logger.Warn("app-server: ResolvePendingRequest — no pending request",
			logger.FieldID, reqID)
		return false
	}
	resp := &Response{
		JSONRPC: jsonrpcVersion,
		ID:      reqID,
		Result:  result,
	}
	select {
	case ch <- resp:
		logger.Info("app-server: ResolvePendingRequest — delivered",
			logger.FieldID, reqID)
		return true
	default:
		logger.Warn("app-server: ResolvePendingRequest — channel full",
			logger.FieldID, reqID)
		return false
	}
}

// AllocPendingRequest 分配一个新的 pending 请求 ID 和等待 channel。
//
// 用于 handleApprovalRequest 的 Wails 模式: 通过 broadcastNotification 推送审批请求,
// 然后用返回的 channel 等待前端 ResolvePendingRequest 回复。
//
// 返回:
//   - reqID: 分配的请求 ID
//   - ch: 等待响应的 channel
//   - cleanup: 清理函数 (defer 调用)
func (s *Server) AllocPendingRequest() (reqID int64, ch <-chan *Response, cleanup func()) {
	id := s.nextReqID.Add(1)
	respCh := make(chan *Response, 1)
	s.pendingMu.Lock()
	s.pending[id] = respCh
	s.pendingMu.Unlock()
	cleanupFn := func() {
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
	}
	return id, respCh, cleanupFn
}

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
	entry := newConnEntry(ws)
	s.mu.Lock()
	s.conns[connID] = entry
	s.mu.Unlock()
	util.SafeGo(func() {
		if err := entry.writeLoop(); err != nil {
			logger.Warn("app-server: write loop failed", logger.FieldConn, connID, logger.FieldError, err)
			s.disconnectConn(connID)
		}
	})

	logger.Info("app-server: client connected", logger.FieldConn, connID, logger.FieldRemote, r.RemoteAddr)

	defer func() {
		s.mu.Lock()
		delete(s.conns, connID)
		s.mu.Unlock()
		entry.closeNow()
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
	defer func() {
		if r := recover(); r != nil {
			logger.Error("app-server: readLoop panicked, disconnecting",
				logger.FieldConn, connID, logger.FieldError, r)
			s.disconnectConn(connID)
		}
	}()
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
			_ = s.sendResponseViaOutbox(connID, entry, newError(nil, CodeParseError, "parse error: "+err.Error()), "parse_error_response")
			continue
		}

		// 快速路径: 客户端响应 (有 id + 无 method + 有 result/error)
		// 直接从 raw bytes 解析 int64 ID → pending map 查找, 零 alloc
		if s.handleClientResponse(env) {
			continue
		}
		if env.Method != "" && len(env.ID) > 0 && string(env.ID) != "null" && entry.outboxDepth() >= connBacklogCut {
			overloaded := newErrorData(rawIDtoAny(env.ID), CodeOverloaded, "Server overloaded; retry later.", map[string]any{
				"retry_after_ms": 500,
			})
			if !s.sendResponseViaOutbox(connID, entry, overloaded, "request_overloaded") {
				return
			}
			continue
		}

		// 正常请求/通知: 复用已解析的字段
		resp := s.handleParsedMessage(ctx, env)
		if resp == nil {
			continue
		}

		if !s.sendResponseViaOutbox(connID, entry, resp, "request_response") {
			return
		}
	}
}

func (s *Server) sendResponseViaOutbox(connID string, entry *connEntry, resp *Response, reason string) bool {
	if resp == nil {
		return true
	}
	data, err := json.Marshal(resp)
	if err != nil {
		logger.Error("app-server: marshal response failed", logger.FieldConn, connID, logger.FieldError, err)
		return false
	}
	return s.enqueueConnMessage(connID, entry, websocket.TextMessage, data, reason)
}

func (s *Server) handleClientResponse(env rpcEnvelope) bool {
	if len(env.ID) == 0 || string(env.ID) == "null" || env.Method != "" {
		return false
	}
	if len(env.Result) == 0 && len(env.Error) == 0 {
		return false
	}
	reqID, ok := parseIntID(env.ID)
	if !ok {
		return false
	}
	s.pendingMu.Lock()
	ch, found := s.pending[reqID]
	s.pendingMu.Unlock()
	if !found {
		return false
	}
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
	return true
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
		if method == "ui/code/open" {
			logger.Warn("app-server: request for unregistered method",
				logger.FieldMethod, method,
				logger.FieldID, id,
				"hint", "backend binary is outdated; rebuild agent-terminal/app-server with ui/code/open registration",
			)
		} else {
			logger.Warn("app-server: request for unregistered method",
				logger.FieldMethod, method,
				logger.FieldID, id,
			)
		}
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

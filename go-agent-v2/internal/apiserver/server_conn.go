// server_conn.go — WebSocket 连接管理、RPC 消息解析与方法分发。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

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

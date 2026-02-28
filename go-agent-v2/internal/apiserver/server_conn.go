package apiserver

import (
	"context"
	"encoding/json"
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

const (
	connWriteTimeout    = 10 * time.Second
	connPingInterval    = 20 * time.Second
	connReadIdleTimeout = 75 * time.Second
)

type connEntry struct {
	ws        *websocket.Conn
	wrMu      sync.Mutex
	outbox    chan wsOutbound
	closeCh   chan struct{}
	closeOnce sync.Once
}

func newConnEntry(ws *websocket.Conn) *connEntry {
	return &connEntry{ws: ws, outbox: make(chan wsOutbound, connOutboxSize), closeCh: make(chan struct{})}
}

func (c *connEntry) writeMsg(msgType int, data []byte) error {
	c.wrMu.Lock()
	defer c.wrMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(connWriteTimeout))
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

func (c *connEntry) outboxDepth() int { return len(c.outbox) }

func (c *connEntry) closeNow() {
	c.closeOnce.Do(func() {
		close(c.closeCh)
		if c.ws != nil { _ = c.ws.Close() }
	})
}

func (c *connEntry) writeLoop() error {
	for {
		select {
		case <-c.closeCh:
			return nil
		case msg := <-c.outbox:
			if err := c.writeMsg(msg.msgType, msg.data); err != nil { return err }
		}
	}
}

func (c *connEntry) writePing() error {
	c.wrMu.Lock()
	defer c.wrMu.Unlock()
	deadline := time.Now().Add(connWriteTimeout)
	_ = c.ws.SetWriteDeadline(deadline)
	return c.ws.WriteControl(websocket.PingMessage, []byte("ping"), deadline)
}

func checkLocalOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" { return true }
	origin = strings.ToLower(origin)
	for _, allowed := range []string{"http://localhost", "https://localhost", "http://127.0.0.1", "https://127.0.0.1", "http://[::1]", "https://[::1]", "wails://"} {
		if strings.HasPrefix(origin, allowed) { return true }
	}
	logger.Warn("app-server: rejected non-local origin", logger.FieldOrigin, origin)
	return false
}

func InvokeMethod(s *Server, ctx context.Context, method string, params json.RawMessage) (any, error) {
	if s == nil { return nil, pkgerr.New("Server.InvokeMethod", "server is nil") }
	resp := dispatchRequest(s, ctx, 1, method, params)
	if resp == nil { return nil, nil }
	if resp.Error != nil {
		return nil, pkgerr.Newf("Server.InvokeMethod", "%s (code %d)", resp.Error.Message, resp.Error.Code)
	}
	return resp.Result, nil
}

func broadcastNotification(s *Server, method string, params any) {
	if s == nil { return }
	hook := notifyHookFuncState(s)
	if hook != nil { hook(method, params) }

	notif := newNotification(method, params)
	data, err := json.Marshal(notif)
	if err != nil {
		logger.Error("app-server: marshal notification failed", logger.FieldMethod, method, logger.FieldError, err)
		return
	}

	clients := snapshotSSEClientsState(s)
	if len(clients) > 0 {
		logger.Debug("sse: broadcasting", logger.FieldMethod, method, "clients", len(clients), logger.FieldDataLen, len(data))
		for _, ch := range clients {
			select {
			case ch <- data:
			default:
				logger.Warn("sse: client channel full, dropping event")
			}
		}
	}

	snapshot := connsSnapshotState(s)
	for id, entry := range snapshot {
		enqueueConnMessage(s, id, entry, websocket.TextMessage, data, "notify_backpressure")
	}
}

func enqueueConnMessage(s *Server, connID string, entry *connEntry, msgType int, data []byte, reason string) bool {
	if s == nil || entry == nil { return false }
	if entry.enqueue(msgType, data) { return true }
	logger.Warn("app-server: client send queue overloaded, disconnecting",
		logger.FieldConn, connID,
		"reason", strings.TrimSpace(reason),
		"outbox_depth", entry.outboxDepth(),
		"outbox_cap", connOutboxSize,
	)
	disconnectConn(s, connID)
	return false
}

func disconnectConn(s *Server, connID string) {
	id := strings.TrimSpace(connID)
	if s == nil || id == "" { return }
	entry, ok := removeConnState(s, id)
	if ok && entry != nil { entry.closeNow() }
}

func sendRequest(s *Server, connID, method string, params any) (*Response, error) {
	if s == nil { return nil, pkgerr.New("Server.SendRequest", "server is nil") }
	reqID, ch, cleanupPending := allocPendingRequestState(s)
	defer cleanupPending()

	req := Request{JSONRPC: jsonrpcVersion, ID: reqID, Method: method}
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

	entry, ok := getConnState(s, connID)
	if !ok { return nil, pkgerr.Newf("Server.SendRequest", "connection %s not found", connID) }

	if !enqueueConnMessage(s, connID, entry, websocket.TextMessage, data, "server_request_backpressure") {
		return nil, pkgerr.Newf("Server.SendRequest", "connection %s overloaded; retry later", connID)
	}

	logger.Info("app-server: sent request to client", logger.FieldConn, connID, logger.FieldMethod, method, logger.FieldID, reqID)

	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return nil, pkgerr.Newf("Server.SendRequest", "request %d timed out waiting for client response", reqID)
	}
}

func sendRequestToAll(s *Server, method string, params any) (*Response, error) {
	if s == nil { return nil, pkgerr.New("Server.SendRequestToAll", "server is nil") }
	firstConn := firstConnIDState(s)
	if firstConn == "" {
		return nil, pkgerr.New("Server.SendRequestToAll", "no connected clients")
	}
	return sendRequest(s, firstConn, method, params)
}

func ResolvePendingRequest(s *Server, reqID int64, result map[string]any) bool {
	if s == nil { return false }
	resp := &Response{JSONRPC: jsonrpcVersion, ID: reqID, Result: result}
	found, delivered := deliverPendingResponseState(s, reqID, resp)
	if !found {
		logger.Warn("app-server: ResolvePendingRequest — no pending request", logger.FieldID, reqID)
		return false
	}
	if delivered {
		logger.Info("app-server: ResolvePendingRequest — delivered", logger.FieldID, reqID)
		return true
	}
	logger.Warn("app-server: ResolvePendingRequest — channel full", logger.FieldID, reqID)
	return false
}

func allocPendingRequest(s *Server) (reqID int64, ch <-chan *Response, cleanup func()) {
	if s == nil { return 0, nil, func() {} }
	return allocPendingRequestState(s)
}

func handleUpgrade(s *Server, w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.Error(w, "server not ready", http.StatusServiceUnavailable)
		return
	}
	numConns := connectionCountState(s)
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

	ws.SetReadLimit(maxMessageSize)
	_ = ws.SetReadDeadline(time.Now().Add(connReadIdleTimeout))
	ws.SetPongHandler(func(string) error {
		_ = ws.SetReadDeadline(time.Now().Add(connReadIdleTimeout))
		return nil
	})

	connID := allocConnIDState(s)
	entry := newConnEntry(ws)
	addConnState(s, connID, entry)
	util.SafeGo(func() {
		if err := entry.writeLoop(); err != nil {
			logger.Warn("app-server: write loop failed", logger.FieldConn, connID, logger.FieldError, err)
			disconnectConn(s, connID)
		}
	})
	util.SafeGo(func() { pingConnLoop(s, entry, connID) })

	logger.Info("app-server: client connected", logger.FieldConn, connID, logger.FieldRemote, r.RemoteAddr)

	defer func() {
		removeConnState(s, connID)
		entry.closeNow()
		logger.Info("app-server: client disconnected", logger.FieldConn, connID)
	}()

	readLoop(s, r.Context(), entry, connID)
}

func pingConnLoop(s *Server, entry *connEntry, connID string) {
	if s == nil || entry == nil { return }
	ticker := time.NewTicker(connPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-entry.closeCh:
			return
		case <-ticker.C:
			if err := entry.writePing(); err != nil {
				logger.Warn("app-server: ping failed, disconnecting client", logger.FieldConn, connID, logger.FieldError, err)
				disconnectConn(s, connID)
				return
			}
		}
	}
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func validIncomingJSONRPCVersion(version string) bool {
	return strings.TrimSpace(version) == jsonrpcVersion
}

func parseIntID(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" { return 0, false }
	var n int64
	neg := false
	i := 0
	if raw[0] == '-' {
		neg = true
		i = 1
	}
	if i >= len(raw) { return 0, false }
	for ; i < len(raw); i++ {
		c := raw[i]
		if c < '0' || c > '9' { return 0, false }
		n = n*10 + int64(c-'0')
	}
	if neg { n = -n }
	return n, true
}

func rawIDtoAny(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" { return nil }
	if n, ok := parseIntID(raw); ok { return n }
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		logger.Debug("app-server: rawIDtoAny unmarshal", logger.FieldError, err)
	}
	return v
}

func readLoop(s *Server, ctx context.Context, entry *connEntry, connID string) {
	if s == nil { return }
	defer func() {
		if r := recover(); r != nil {
			logger.Error("app-server: readLoop panicked, disconnecting", logger.FieldConn, connID, logger.FieldError, r)
			disconnectConn(s, connID)
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
		_ = entry.ws.SetReadDeadline(time.Now().Add(connReadIdleTimeout))

		var env rpcEnvelope
		var resp *Response
		reason := "request_response"
		if err := json.Unmarshal(message, &env); err != nil {
			_ = sendResponseViaOutbox(s, connID, entry, newError(nil, CodeParseError, "parse error: "+err.Error()), "parse_error_response")
			continue
		}
		if !validIncomingJSONRPCVersion(env.JSONRPC) {
			resp = newError(rawIDtoAny(env.ID), CodeInvalidRequest, "invalid request: jsonrpc must be \"2.0\"")
			reason = "invalid_jsonrpc_version"
		} else if handleClientResponse(s, env) {
			continue
		} else if env.Method != "" && len(env.ID) > 0 && string(env.ID) != "null" && entry.outboxDepth() >= connBacklogCut {
			resp = newErrorData(rawIDtoAny(env.ID), CodeOverloaded, "Server overloaded; retry later.", map[string]any{"retry_after_ms": 500})
			reason = "request_overloaded"
		} else {
			resp = dispatchRequest(s, ctx, rawIDtoAny(env.ID), env.Method, env.Params)
			if resp == nil { continue }
		}
		if !sendResponseViaOutbox(s, connID, entry, resp, reason) { return }
	}
}

func sendResponseViaOutbox(s *Server, connID string, entry *connEntry, resp *Response, reason string) bool {
	if s == nil { return false }
	if resp == nil { return true }
	data, err := json.Marshal(resp)
	if err != nil {
		logger.Error("app-server: marshal response failed", logger.FieldConn, connID, logger.FieldError, err)
		return false
	}
	return enqueueConnMessage(s, connID, entry, websocket.TextMessage, data, reason)
}

func handleClientResponse(s *Server, env rpcEnvelope) bool {
	if s == nil { return false }
	if len(env.ID) == 0 || string(env.ID) == "null" || env.Method != "" { return false }
	if len(env.Result) == 0 && len(env.Error) == 0 { return false }
	reqID, ok := parseIntID(env.ID)
	if !ok { return false }
	resp := &Response{JSONRPC: jsonrpcVersion, ID: reqID}
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
	found, _ := deliverPendingResponseState(s, reqID, resp)
	return found
}

func dispatchRequest(s *Server, ctx context.Context, id any, method string, params json.RawMessage) *Response {
	if s == nil { return newError(id, CodeInternalError, "server is nil") }
	if method == "" { return newError(id, CodeInvalidRequest, "method is required") }

	handler, ok := s.methods[method]
	if !ok {
		if id == nil {
			logger.Warn("app-server: notification for unregistered method (dropped)", logger.FieldMethod, method, logger.FieldParamsLen, len(params))
			return nil
		}
		logger.Warn("app-server: request for unregistered method", logger.FieldMethod, method, logger.FieldID, id)
		return newError(id, CodeMethodNotFound, "method not found: "+method)
	}

	result, err := handler(ctx, params)
	if err != nil {
		if id == nil {
			logger.Warn("app-server: notification handler error (no response sent)", logger.FieldMethod, method, logger.FieldError, err)
			return nil
		}
		logger.Warn("app-server: request handler error", logger.FieldMethod, method, logger.FieldID, id, logger.FieldError, err)
		return newError(id, CodeInternalError, err.Error())
	}
	if id == nil { return nil }
	return newResult(id, result)
}

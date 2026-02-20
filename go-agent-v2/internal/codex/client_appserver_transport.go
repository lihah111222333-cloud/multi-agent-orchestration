// client_appserver_transport.go — WebSocket 传输层: 连接、重连、RPC 通信。
package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func (c *AppServerClient) Spawn(ctx context.Context) error {
	if c.Port > 0 {
		if err := checkPortFree(c.Port); err != nil {
			return apperrors.Wrapf(err, "AppServerClient.Spawn", "port %d occupied", c.Port)
		}
	}

	listenURL := fmt.Sprintf("ws://127.0.0.1:%d", c.Port)
	// 注意: 使用 exec.Command 而非 exec.CommandContext —
	// 子进程不应随 HTTP 请求或 WebSocket 连接断开而被终止。
	// 生命周期由 AppServerClient.Shutdown()/Kill() 显式管理。
	c.Cmd = exec.Command("codex", "app-server", "--listen", listenURL)
	c.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cmd.Env = os.Environ()
	c.Cmd.Stdout = io.Discard
	c.stderrCollector = logger.NewStderrCollector(fmt.Sprintf("codex-appserver-%d", c.Port))
	c.Cmd.Stderr = c.stderrCollector

	if err := c.Cmd.Start(); err != nil {
		return apperrors.Wrap(err, "AppServerClient.Spawn", "spawn app-server")
	}

	// 等待 WebSocket 可用 (默认最多 30 秒, 同时受 ctx 控制)
	deadline := time.Now().Add(appServerStartupProbeTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			_ = c.Kill()
			return apperrors.Wrap(ctx.Err(), "AppServerClient.Spawn", "spawn cancelled")
		default:
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", c.Port), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			logger.Info("codex: app-server listening", logger.FieldAgentID, c.AgentID, logger.FieldPort, c.Port)
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	_ = c.Kill()
	return apperrors.Newf("AppServerClient.Spawn", "app-server startup timeout on port %d", c.Port)
}

// connectWS 连接 WebSocket 并启动 readLoop。
func (c *AppServerClient) connectWS() error {
	conn, err := c.dialWS(c.ctx)
	if err != nil {
		return apperrors.Wrap(err, "AppServerClient.connectWS", "ws connect")
	}
	c.replaceWSConn(conn)
	util.SafeGo(func() { c.readLoop() })
	util.SafeGo(func() { c.pingLoop(conn) })
	return nil
}

func (c *AppServerClient) dialWS(ctx context.Context) (*websocket.Conn, error) {
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d", c.Port)
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		NetDialContext:   (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, apperrors.New("AppServerClient.dialWS", "dial returned nil websocket connection")
	}
	_ = conn.SetReadDeadline(time.Now().Add(appServerReadIdleTimeout))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(appServerReadIdleTimeout))
		return nil
	})
	return conn, nil
}

func (c *AppServerClient) currentWSConn() *websocket.Conn {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	return c.ws
}

func (c *AppServerClient) replaceWSConn(conn *websocket.Conn) {
	if conn == nil {
		return
	}
	c.wsMu.Lock()
	prev := c.ws
	c.ws = conn
	c.wsMu.Unlock()
	if prev != nil && prev != conn {
		_ = prev.Close()
	}
}

func appServerReconnectDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	delay := appServerReconnectBaseDelay
	for i := 2; i < attempt; i++ {
		delay *= 2
		if delay >= appServerReconnectMaxDelay {
			return appServerReconnectMaxDelay
		}
	}
	if delay > appServerReconnectMaxDelay {
		return appServerReconnectMaxDelay
	}
	return delay
}

func (c *AppServerClient) sleepWithContext(delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.ctx.Done():
		return false
	}
}

func (c *AppServerClient) emitBackgroundEvent(message string, status string, active bool, done bool, details map[string]any) {
	c.handlerMu.RLock()
	handler := c.handler
	c.handlerMu.RUnlock()
	if handler == nil {
		return
	}
	payload := map[string]any{
		"message": strings.TrimSpace(message),
		"status":  strings.TrimSpace(status),
		"active":  active,
		"done":    done,
	}
	for key, value := range details {
		payload[key] = value
	}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("codex: emitBackgroundEvent marshal failed",
			logger.FieldAgentID, c.AgentID, logger.FieldError, err)
		return
	}
	handler(Event{Type: EventBackgroundEvent, Data: data})
}

func (c *AppServerClient) reconnectWS(trigger string, lastErr error) bool {
	trigger = strings.TrimSpace(trigger)
	activeTurnID := c.getActiveTurnID()
	maxRetries := appServerStreamMaxRetries
	if maxRetries <= 0 {
		maxRetries = 0
	}
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if c.stopped.Load() {
			return false
		}
		// 子进程已退出则无需重连 — 避免无效 dial 浪费时间。
		if !c.Running() {
			logger.Warn("codex: reconnect aborted — process exited",
				logger.FieldAgentID, c.AgentID,
				"trigger", trigger,
			)
			break
		}
		delay := appServerReconnectDelay(attempt)
		if !c.sleepWithContext(delay) {
			return false
		}
		if c.attemptSingleReconnect(trigger, activeTurnID, attempt, maxRetries) {
			return true
		}
	}

	c.handleReconnectExhausted(trigger, activeTurnID, maxRetries, lastErr)
	return false
}

// attemptSingleReconnect tries a single WebSocket reconnection.
// Returns true if reconnection succeeded.
func (c *AppServerClient) attemptSingleReconnect(trigger, activeTurnID string, attempt, maxRetries int) bool {
	c.emitBackgroundEvent(
		"Reconnecting...",
		"reconnecting",
		true,
		false,
		map[string]any{
			"phase":        "reconnect",
			"trigger":      trigger,
			"attempt":      attempt,
			"max_retries":  maxRetries,
			"activeTurnId": activeTurnID,
		},
	)

	conn, err := c.dialWS(c.ctx)
	if err != nil {
		retryErr := apperrors.Wrap(err, "AppServerClient.reconnectWS", "dial reconnect")
		willRetry := attempt < maxRetries
		reconnectMessage := fmt.Sprintf("Reconnecting... %d/%d", attempt, maxRetries)
		if !willRetry {
			reconnectMessage = fmt.Sprintf("Reconnect failed %d/%d", attempt, maxRetries)
		}
		c.emitStreamError(retryErr, "reconnect", false, willRetry, map[string]any{
			"message":      reconnectMessage,
			"attempt":      attempt,
			"max_retries":  maxRetries,
			"trigger":      trigger,
			"activeTurnId": activeTurnID,
		})
		logger.Warn("codex: ws reconnect attempt failed",
			logger.FieldAgentID, c.AgentID,
			"trigger", trigger,
			"attempt", attempt,
			"max_retries", maxRetries,
			"active_turn_id", activeTurnID,
			logger.FieldError, retryErr,
		)
		return false
	}

	c.replaceWSConn(conn)
	c.listenerEnsureNeeded.Store(true)
	c.ensureListenerIfNeededAsync("reconnect", c.call)
	util.SafeGo(func() { c.pingLoop(conn) })
	c.emitBackgroundEvent(
		"Reconnected",
		"completed",
		false,
		true,
		map[string]any{
			"phase":        "reconnect",
			"trigger":      trigger,
			"attempt":      attempt,
			"max_retries":  maxRetries,
			"activeTurnId": activeTurnID,
		},
	)
	logger.Info("codex: ws reconnected",
		logger.FieldAgentID, c.AgentID,
		"trigger", trigger,
		"attempt", attempt,
		"max_retries", maxRetries,
		"active_turn_id", activeTurnID,
	)
	return true
}

// handleReconnectExhausted emits failure events after all reconnection attempts are exhausted.
func (c *AppServerClient) handleReconnectExhausted(trigger, activeTurnID string, maxRetries int, lastErr error) {
	exhausted := map[string]any{
		"phase":       "reconnect",
		"trigger":     trigger,
		"attempt":     maxRetries,
		"max_retries": maxRetries,
	}
	if lastErr != nil {
		exhausted["last_error"] = lastErr.Error()
	}
	if activeTurnID != "" {
		exhausted["activeTurnId"] = activeTurnID
	}
	c.emitBackgroundEvent("Reconnect failed", "failed", false, true, exhausted)
	logger.Warn("codex: ws reconnect exhausted",
		logger.FieldAgentID, c.AgentID,
		"trigger", trigger,
		"max_retries", maxRetries,
		"active_turn_id", activeTurnID,
		logger.FieldError, lastErr,
	)
}

// ========================================
// JSON-RPC 请求/响应
// ========================================

// call 发送 JSON-RPC 请求并等待响应。
func (c *AppServerClient) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	pc := &pendingCall{done: make(chan struct{})}
	c.pending.Store(id, pc)
	defer c.pending.Delete(id)

	if err := c.asWriteJSON(req); err != nil {
		return nil, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-pc.done:
		return pc.result, pc.err
	case <-timer.C:
		return nil, apperrors.Newf("AppServerClient.call", "%s timeout", method)
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

// notify 发送 JSON-RPC 通知 (无需响应)。
func (c *AppServerClient) notify(method string, params any) error {
	msg := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.asWriteJSON(msg)
}

// respond 发送 JSON-RPC 响应 (回复 server request)。
func (c *AppServerClient) respond(id int64, result any) error {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	return c.asWriteJSON(resp)
}

// RespondError 向 codex 发送 JSON-RPC 错误响应 (用于 server request 失败场景)。
//
// 当 codex 发送带 id 的 server request (如 dynamic_tool_call / approval) 时,
// 我方必须回复 response; 若处理过程中遇到错误, 用此方法发 error response,
// 避免 codex turn 永久挂起。
func (c *AppServerClient) RespondError(id int64, code int, message string) error {
	resp := struct {
		JSONRPC string        `json:"jsonrpc"`
		ID      int64         `json:"id"`
		Error   *jsonRPCError `json:"error"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: message},
	}
	return c.asWriteJSON(resp)
}

// ========================================
// 协议方法
// ========================================

// Initialize 发送 initialize 请求。
//
// capabilities.experimentalApi = true 是 dynamicTools 的前提条件:
// codex 的 thread/start.dynamicTools 标记了 #[experimental],
// 不声明此 capability 会导致 dynamicTools 被静默忽略。

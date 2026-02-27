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

	backofflib "github.com/cenkalti/backoff/v4"
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
	c.replaceWSConnAndStartLoops(conn, false)
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
	_ = conn.SetReadDeadline(time.Now().Add(currentAppServerReadIdleTimeout()))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(currentAppServerReadIdleTimeout()))
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

func (c *AppServerClient) replaceWSConnAndStartLoops(conn *websocket.Conn, resetReadLoopSignal bool) {
	c.replaceWSConn(conn)
	if resetReadLoopSignal {
		c.wsDone = make(chan struct{})
	}
	util.SafeGo(func() { c.readLoop() })
	util.SafeGo(func() { c.pingLoop(conn) })
}

// appServerReconnectBackoff 创建预配置的指数退避器。
// 首次无延迟 (由调用方控制), 后续: 300ms → 600ms → 1.2s → 2.4s → 3s (capped)。
func appServerReconnectBackoff() backofflib.BackOff {
	b := backofflib.NewExponentialBackOff()
	b.InitialInterval = appServerReconnectBaseDelay
	b.MaxInterval = appServerReconnectMaxDelay
	b.MaxElapsedTime = 0 // 不自动停止，由调用方控制重试次数
	b.Multiplier = 2
	b.RandomizationFactor = 0 // 保持确定性，与原实现一致
	b.Reset()
	return b
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
	maxRetries := max(appServerStreamMaxRetries, 0)
	now := time.Now()
	if remaining, health := c.circuitRemaining(now); remaining > 0 {
		c.emitBackgroundEvent("Reconnect paused (circuit breaker)", "reconnecting", true, false, reconnectDetails(trigger, activeTurnID, map[string]any{
			"phase":               "reconnect",
			"circuit_remaining":   remaining.Milliseconds(),
			"read_failure_streak": health.ReadFailureStreak,
			"read_errors_window":  health.ReadErrorsWindow,
		}))
		logger.Warn("codex: reconnect paused by circuit breaker",
			logger.FieldAgentID, c.AgentID,
			"trigger", trigger,
			"circuit_remaining_ms", remaining.Milliseconds(),
			"read_failure_streak", health.ReadFailureStreak,
			"read_errors_window", health.ReadErrorsWindow,
		)
		if !c.sleepWithContext(remaining) {
			return false
		}
	}
	if c.shouldPreferRespawn(time.Now()) {
		if c.recoverByRespawn(trigger, activeTurnID, "read_error_burst", lastErr) {
			return true
		}
	}

	bo := appServerReconnectBackoff()
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
		var delay time.Duration
		if attempt > 1 {
			delay = bo.NextBackOff()
			if delay == backofflib.Stop {
				delay = appServerReconnectMaxDelay
			}
		}
		if !c.sleepWithContext(delay) {
			return false
		}
		if c.attemptSingleReconnect(trigger, activeTurnID, attempt, maxRetries) {
			return true
		}
	}

	if c.recoverByRespawn(trigger, activeTurnID, "reconnect_exhausted", lastErr) {
		return true
	}
	c.handleReconnectExhausted(trigger, activeTurnID, maxRetries, lastErr)
	return false
}

// attemptSingleReconnect tries a single WebSocket reconnection.
// Returns true if reconnection succeeded.
func (c *AppServerClient) attemptSingleReconnect(trigger, activeTurnID string, attempt, maxRetries int) bool {
	c.noteReconnectAttempt()
	c.emitBackgroundEvent(
		"Reconnecting...",
		"reconnecting",
		true,
		false,
		reconnectDetails(trigger, activeTurnID, map[string]any{
			"phase":       "reconnect",
			"attempt":     attempt,
			"max_retries": maxRetries,
		}),
	)

	conn, err := c.dialWS(c.ctx)
	if err != nil {
		retryErr := apperrors.Wrap(err, "AppServerClient.reconnectWS", "dial reconnect")
		health := c.noteReconnectFailure(time.Now())
		willRetry := attempt < maxRetries
		reconnectMessage := fmt.Sprintf("Reconnecting... %d/%d", attempt, maxRetries)
		if !willRetry {
			reconnectMessage = fmt.Sprintf("Reconnect failed %d/%d", attempt, maxRetries)
		}
		details := reconnectDetails(trigger, activeTurnID, map[string]any{
			"message":     reconnectMessage,
			"attempt":     attempt,
			"max_retries": maxRetries,
		})
		c.emitStreamError(retryErr, "reconnect", false, willRetry, mergeDetails(details, health.asDetailsMap()))
		logger.Warn("codex: ws reconnect attempt failed",
			logger.FieldAgentID, c.AgentID,
			"trigger", trigger,
			"attempt", attempt,
			"max_retries", maxRetries,
			"reconnect_failure_streak", health.ReconnectFailureStreak,
			"circuit_open", health.CircuitOpen,
			"circuit_remaining_ms", health.CircuitRemainingMs,
			logger.FieldTurnID, activeTurnID,
			logger.FieldError, retryErr,
		)
		return false
	}

	c.replaceWSConn(conn)
	c.listenerEnsureNeeded.Store(true)
	c.ensureListenerIfNeededAsync(c.call)
	util.SafeGo(func() { c.pingLoop(conn) })
	health := c.noteReconnectSuccess(time.Now())
	c.emitBackgroundEvent(
		"Reconnected",
		"completed",
		false,
		true,
		reconnectDetails(trigger, activeTurnID, map[string]any{
			"phase":                     "reconnect",
			"attempt":                   attempt,
			"max_retries":               maxRetries,
			"total_reconnect_successes": health.TotalReconnectSuccess,
		}),
	)
	// 显式清除残留 stream error 状态 —
	// 防止快速重连周期中 streamErrorText 永久卡住。
	c.emitReconnectResolved()
	logger.Info("codex: ws reconnected",
		logger.FieldAgentID, c.AgentID,
		"trigger", trigger,
		"attempt", attempt,
		"max_retries", maxRetries,
		"reconnect_failure_streak", health.ReconnectFailureStreak,
		"total_reconnect_successes", health.TotalReconnectSuccess,
		logger.FieldTurnID, activeTurnID,
	)
	return true
}

func (c *AppServerClient) restartProcessAndReconnect() error {
	if err := c.Kill(); err != nil {
		logger.Warn("codex: restart recovery kill failed",
			logger.FieldAgentID, c.AgentID,
			logger.FieldError, err,
		)
	}
	if c.stderrCollector != nil {
		_ = c.stderrCollector.Close()
		c.stderrCollector = nil
	}

	spawnCtx, cancel := context.WithTimeout(c.ctx, appServerStartupProbeTimeout)
	defer cancel()
	if err := c.Spawn(spawnCtx); err != nil {
		return apperrors.Wrap(err, "AppServerClient.restartProcessAndReconnect", "spawn")
	}
	conn, err := c.dialWS(c.ctx)
	if err != nil {
		return apperrors.Wrap(err, "AppServerClient.restartProcessAndReconnect", "dial ws")
	}
	// 重置 wsDone 以支持新的 readLoop 生命周期信号。
	c.replaceWSConnAndStartLoops(conn, true)
	return nil
}

func (c *AppServerClient) recoverByRespawn(trigger, activeTurnID, reason string, lastErr error) bool {
	if !c.respawnRecoverInFlight.CompareAndSwap(false, true) {
		logger.Debug("codex: respawn recovery skipped (already in-flight)",
			logger.FieldAgentID, c.AgentID,
			"trigger", trigger,
			"reason", strings.TrimSpace(reason),
		)
		return false
	}
	defer c.respawnRecoverInFlight.Store(false)

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "reconnect_recovery"
	}
	c.emitBackgroundEvent("Recovering connection (restart app-server)...", "reconnecting", true, false, reconnectDetails(trigger, activeTurnID, map[string]any{
		"phase":  "reconnect",
		"reason": reason,
	}))
	logger.Warn("codex: reconnect escalation to process restart",
		logger.FieldAgentID, c.AgentID,
		"trigger", trigger,
		"reason", reason,
		logger.FieldTurnID, activeTurnID,
		logger.FieldError, lastErr,
	)
	threadID := strings.TrimSpace(c.ThreadID)

	if err := c.restartProcessAndReconnect(); err != nil {
		health := c.noteRespawnResult(time.Now(), false)
		c.emitStreamError(err, "reconnect", false, false, reconnectDetails(trigger, activeTurnID, map[string]any{
			"message":                "Reconnect recovery failed",
			"reason":                 reason,
			"total_respawn_failures": health.TotalRespawnFailure,
		}))
		logger.Warn("codex: process restart recovery failed",
			logger.FieldAgentID, c.AgentID,
			"trigger", trigger,
			"reason", reason,
			"total_respawn_failures", health.TotalRespawnFailure,
			logger.FieldError, err,
		)
		return false
	}

	health := c.noteRespawnResult(time.Now(), true)
	c.listenerEnsureNeeded.Store(threadID != "")
	util.SafeGo(func() {
		if err := c.Initialize(); err != nil {
			logger.Warn("codex: restart recovery initialize failed",
				logger.FieldAgentID, c.AgentID,
				"trigger", trigger,
				"reason", reason,
				logger.FieldError, err,
			)
			return
		}
		if threadID == "" {
			return
		}
		if err := c.ResumeThread(ResumeThreadRequest{ThreadID: threadID}); err != nil {
			logger.Warn("codex: restart recovery resume failed",
				logger.FieldAgentID, c.AgentID,
				logger.FieldThreadID, threadID,
				"trigger", trigger,
				"reason", reason,
				logger.FieldError, err,
			)
			return
		}
		logger.Info("codex: restart recovery initialize/resume completed",
			logger.FieldAgentID, c.AgentID,
			logger.FieldThreadID, threadID,
			"trigger", trigger,
			"reason", reason,
		)
	})
	c.emitBackgroundEvent("Reconnected after restart", "completed", false, true, reconnectDetails(trigger, activeTurnID, map[string]any{
		"phase":                   "reconnect",
		"reason":                  reason,
		"total_respawn_successes": health.TotalRespawnSuccess,
	}))
	logger.Info("codex: reconnected via process restart",
		logger.FieldAgentID, c.AgentID,
		"trigger", trigger,
		"reason", reason,
		"total_respawn_successes", health.TotalRespawnSuccess,
		logger.FieldTurnID, activeTurnID,
	)
	return true
}

// RecoverConnection forces a process restart recovery flow for manual UI recovery.
func (c *AppServerClient) RecoverConnection(reason string) error {
	switch {
	case c == nil:
		return apperrors.New("AppServerClient.RecoverConnection", "client is nil")
	case c.stopped.Load():
		return apperrors.New("AppServerClient.RecoverConnection", "client is stopped")
	case c.respawnRecoverInFlight.Load():
		return apperrors.New("AppServerClient.RecoverConnection", "recovery already in progress")
	}
	recoveryReason := strings.TrimSpace(reason)
	if recoveryReason == "" {
		recoveryReason = "manual_recover"
	}
	if ok := c.recoverByRespawn("manual", c.getActiveTurnID(), recoveryReason, nil); !ok {
		return apperrors.New("AppServerClient.RecoverConnection", "manual recovery failed")
	}
	return nil
}

// emitReconnectResolved 在重连成功后显式清除残留的 stream error 状态。
//
// 快速重连周期中 (read_error→reconnect→read_error→reconnect), handleReadLoopReadError
// 可能在 background_event(done=true) 之后再次设置 streamErrorText。
// 此方法通过 handler callback 直接发送一个无害事件来触发 UI 层清除逻辑。
func (c *AppServerClient) emitReconnectResolved() {
	c.handlerMu.RLock()
	handler := c.handler
	c.handlerMu.RUnlock()
	if handler == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"message":  "Reconnected",
		"resolved": true,
		"done":     true,
		"active":   false,
	})
	handler(Event{Type: EventBackgroundEvent, Data: payload})
}

// handleReconnectExhausted emits failure events after all reconnection attempts are exhausted.
func (c *AppServerClient) handleReconnectExhausted(trigger, activeTurnID string, maxRetries int, lastErr error) {
	_, health := c.circuitRemaining(time.Now())
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
	exhausted = mergeDetails(exhausted, health.asDetailsMap())
	c.emitBackgroundEvent("Reconnect failed", "failed", false, true, exhausted)
	logger.Warn("codex: ws reconnect exhausted",
		logger.FieldAgentID, c.AgentID,
		"trigger", trigger,
		"max_retries", maxRetries,
		"reconnect_failure_streak", health.ReconnectFailureStreak,
		"circuit_open", health.CircuitOpen,
		"circuit_remaining_ms", health.CircuitRemainingMs,
		logger.FieldTurnID, activeTurnID,
		logger.FieldError, lastErr,
	)
}

// ========================================
// JSON-RPC 请求/响应
// ========================================

// call 发送 JSON-RPC 请求并等待响应。
func (c *AppServerClient) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	rpcID := newJSONRPCIntID(c.nextID.Add(1))
	pc := &pendingCall{done: make(chan struct{})}
	c.pending.Store(rpcID.pendingKey(), pc)
	defer c.pending.Delete(rpcID.pendingKey())

	if err := c.asWriteJSON(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      rpcID,
		Method:  method,
		Params:  params,
	}); err != nil {
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
	return c.asWriteJSON(jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

// respond 发送 JSON-RPC 响应 (回复 server request)。
func (c *AppServerClient) respond(id int64, result any) error {
	return c.respondWithID(newJSONRPCIntID(id), result)
}

func (c *AppServerClient) writeRPCResponse(id jsonRPCID, result any, rpcErr *jsonRPCError) error {
	return c.asWriteJSON(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	})
}

func (c *AppServerClient) respondWithID(id jsonRPCID, result any) error {
	return c.writeRPCResponse(id, result, nil)
}

// RespondError 向 codex 发送 JSON-RPC 错误响应 (用于 server request 失败场景)。
func (c *AppServerClient) RespondError(id int64, code int, message string) error {
	return c.respondErrorWithID(newJSONRPCIntID(id), code, message)
}

func (c *AppServerClient) respondErrorWithID(id jsonRPCID, code int, message string) error {
	return c.writeRPCResponse(id, nil, &jsonRPCError{Code: code, Message: message})
}

// ========================================
// 协议方法
// ========================================

// Initialize 发送 initialize 请求。
//
// capabilities.experimentalApi = true 是 dynamicTools 的前提条件:
// codex 的 thread/start.dynamicTools 标记了 #[experimental],
// 不声明此 capability 会导致 dynamicTools 被静默忽略。

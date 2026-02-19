// client_appserver.go — codex app-server JSON-RPC 传输层。
//
// codex app-server 使用 JSON-RPC 2.0 (WebSocket):
//   - Client → Server: {jsonrpc,id,method,params} (请求) 或 {jsonrpc,method,params} (通知)
//   - Server → Client: {jsonrpc,id,result} (响应) 或 {jsonrpc,method,params} (通知)
//
// 关键方法:
//   - initialize → 获取 server capabilities
//   - thread/start → 创建 thread (支持 dynamicTools)
//   - turn/start → 发送 prompt
//   - dynamic_tool_result → 回传工具结果
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// ========================================
// JSON-RPC 2.0 信封
// ========================================

// jsonRPCRequest JSON-RPC 2.0 请求。
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCNotification JSON-RPC 2.0 通知 (无 id)。
type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCMessage JSON-RPC 通用消息 (用于读取解析)。
type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"` // nil = 通知
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError JSON-RPC 错误。
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// jsonRPCResponse JSON-RPC 2.0 响应 (用于回复 server request)。
type jsonRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Result  any    `json:"result,omitempty"`
}

// pendingCall 等待响应的 JSON-RPC 调用。
type pendingCall struct {
	result json.RawMessage
	err    error
	done   chan struct{}
	once   sync.Once
}

func (p *pendingCall) resolve(result json.RawMessage, err error) {
	p.once.Do(func() {
		p.result = result
		p.err = err
		close(p.done)
	})
}

// ========================================
// App-Server 专用 Client
// ========================================

// AppServerClient codex app-server JSON-RPC 客户端。
//
// 替代 http-api REST 客户端, 支持 dynamicTools 注入。
type AppServerClient struct {
	Port     int
	Cmd      *exec.Cmd
	ThreadID string
	AgentID  string // 所属 Agent 标识, 用于日志关联

	// ========================================
	// 锁职责说明
	// ========================================
	// wsMu:      保护 ws (WebSocket 读写序列化)
	// handlerMu: 保护 handler (事件回调注册/读取)
	// 两者独立, 不存在嵌套获取关系。
	// ========================================

	ws              *websocket.Conn
	wsMu            sync.Mutex
	wsDone          chan struct{}
	handler         EventHandler
	handlerMu       sync.RWMutex
	stopped         atomic.Bool
	ctx             context.Context
	cancel          context.CancelFunc
	stderrCollector *logger.StderrCollector

	// JSON-RPC request tracking
	nextID  atomic.Int64
	pending sync.Map // id → *pendingCall

	// 活跃 turn 跟踪: turn/started 存入, turn_complete/idle/error 清空。
	activeTurnID atomic.Value // string

	// listener 兜底标记: 仅在连接重连后需要在下次 turn/start 前执行 thread/resume 确保订阅。
	listenerEnsureNeeded atomic.Bool
	// listener ensure 并发保护: 避免重连和 submit 同时触发重复 ensure。
	listenerEnsureInFlight atomic.Bool

	// legacy mirror 丢弃计数: 用于采样日志输出。
	legacyMirrorDropCount atomic.Int64
}

const appServerStartupProbeTimeout = 30 * time.Second
const appServerWriteTimeout = 10 * time.Second
const appServerPingInterval = 25 * time.Second
const appServerInterruptTimeout = 30 * time.Second
const appServerListenerEnsureTimeout = 10 * time.Second
const appServerReconnectBaseDelay = 300 * time.Millisecond
const appServerReconnectMaxDelay = 3 * time.Second
const defaultAppServerReadIdleTimeout = 600 * time.Second
const defaultAppServerStreamMaxRetries = 5
const maxAppServerStreamMaxRetries = 100

var appServerReadIdleTimeout = appServerReadIdleTimeoutFromEnv()
var appServerStreamMaxRetries = appServerStreamMaxRetriesFromEnv()

func appServerReadIdleTimeoutFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GO_AGENT_APP_SERVER_STREAM_IDLE_TIMEOUT_MS"))
	if raw == "" {
		return defaultAppServerReadIdleTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		logger.Warn("codex: invalid GO_AGENT_APP_SERVER_STREAM_IDLE_TIMEOUT_MS, using default",
			"value", raw,
			"default_ms", defaultAppServerReadIdleTimeout.Milliseconds(),
		)
		return defaultAppServerReadIdleTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func appServerStreamMaxRetriesFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("GO_AGENT_APP_SERVER_STREAM_MAX_RETRIES"))
	if raw == "" {
		return defaultAppServerStreamMaxRetries
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		logger.Warn("codex: invalid GO_AGENT_APP_SERVER_STREAM_MAX_RETRIES, using default",
			"value", raw,
			"default", defaultAppServerStreamMaxRetries,
		)
		return defaultAppServerStreamMaxRetries
	}
	if value > maxAppServerStreamMaxRetries {
		return maxAppServerStreamMaxRetries
	}
	return value
}

// NewAppServerClient 创建 app-server 客户端。
func NewAppServerClient(port int, agentID string) *AppServerClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &AppServerClient{
		Port:    port,
		AgentID: agentID,
		ctx:     ctx,
		cancel:  cancel,
		wsDone:  make(chan struct{}),
	}
}

// GetPort 返回端口号。
func (c *AppServerClient) GetPort() int { return c.Port }

// GetThreadID 返回当前 thread ID。
func (c *AppServerClient) GetThreadID() string { return c.ThreadID }

// GetActiveTurnID 返回当前活跃 turn ID。
func (c *AppServerClient) GetActiveTurnID() string { return c.getActiveTurnID() }

// SetEventHandler 注册事件回调。
func (c *AppServerClient) SetEventHandler(h EventHandler) {
	c.handlerMu.Lock()
	c.handler = h
	c.handlerMu.Unlock()
}

// ========================================
// 进程管理
// ========================================

// Spawn 启动 codex app-server --listen ws://IP:PORT。
//
// 子进程的生命周期独立于调用者 ctx — 用 Shutdown()/Kill() 管理。
// ctx 仅用于启动超时控制。
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
			continue
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
	return false
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
func (c *AppServerClient) Initialize() error {
	logger.Info("codex: Initialize()",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		"experimentalApi", true,
	)
	result, err := c.call("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "go-agent-v2",
			"version": "1.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}, 10*time.Second)
	if err != nil {
		logger.Error("codex: Initialize() FAILED", logger.FieldAgentID, c.AgentID, logger.FieldPort, c.Port, logger.FieldError, err)
		return err
	}
	logger.Info("codex: Initialize() OK",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		"server_caps", string(result),
	)
	return nil
}

// asThreadStartParams thread/start 参数 (app-server JSON-RPC)。
type asThreadStartParams struct {
	Cwd          string        `json:"cwd,omitempty"`
	Model        string        `json:"model,omitempty"`
	DynamicTools []DynamicTool `json:"dynamicTools,omitempty"` // camelCase as required by app-server
}

// ThreadStart 创建 thread (app-server JSON-RPC)。
func (c *AppServerClient) ThreadStart(cwd, model string, dynamicTools []DynamicTool) (string, error) {
	toolNames := make([]string, len(dynamicTools))
	for i, t := range dynamicTools {
		toolNames[i] = t.Name
	}
	logger.Info("codex: thread/start",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		logger.FieldCwd, cwd,
		"model", model,
		"dynamic_tools_count", len(dynamicTools),
		"dynamic_tools", toolNames,
	)

	result, err := c.call("thread/start", asThreadStartParams{
		Cwd:          cwd,
		Model:        model,
		DynamicTools: dynamicTools,
	}, 30*time.Second)
	if err != nil {
		logger.Error("codex: thread/start FAILED", logger.FieldAgentID, c.AgentID, logger.FieldPort, c.Port, logger.FieldError, err)
		return "", apperrors.Wrap(err, "AppServerClient.ThreadStart", "thread/start")
	}

	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		logger.Error("codex: thread/start decode FAILED", logger.FieldAgentID, c.AgentID, logger.FieldPort, c.Port, logger.FieldRaw, string(result), logger.FieldError, err)
		return "", apperrors.Wrapf(err, "AppServerClient.ThreadStart", "thread/start decode (raw: %s)", result)
	}
	if resp.Thread.ID == "" {
		logger.Error("codex: thread/start returned empty thread ID", logger.FieldAgentID, c.AgentID, logger.FieldPort, c.Port, logger.FieldRaw, string(result))
		return "", apperrors.Newf("AppServerClient.ThreadStart", "thread/start returned empty thread ID (raw: %s)", result)
	}
	c.ThreadID = resp.Thread.ID
	c.listenerEnsureNeeded.Store(false)
	logger.Info("codex: thread/start OK",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		logger.FieldThreadID, c.ThreadID,
		"dynamic_tools", len(dynamicTools),
	)
	return c.ThreadID, nil
}

// asTurnStartInput turn/start 输入项。
type asTurnStartInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
}

func isRemoteImageURL(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(value, "http://"),
		strings.HasPrefix(value, "https://"),
		strings.HasPrefix(value, "data:image/"):
		return true
	default:
		return false
	}
}

func mentionNameFromPath(path string) string {
	base := strings.TrimSpace(filepath.Base(strings.TrimSpace(path)))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "file"
	}
	return base
}

func buildTurnStartInputs(prompt string, images, files []string) []asTurnStartInput {
	inputs := make([]asTurnStartInput, 0, 1+len(images)+len(files))
	trimmedPrompt := strings.TrimSpace(prompt)
	if trimmedPrompt != "" || (len(images) == 0 && len(files) == 0) {
		inputs = append(inputs, asTurnStartInput{Type: "text", Text: prompt})
	}

	for _, raw := range images {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if isRemoteImageURL(path) {
			inputs = append(inputs, asTurnStartInput{Type: "image", URL: path})
			continue
		}
		inputs = append(inputs, asTurnStartInput{Type: "localImage", Path: path})
	}

	for _, raw := range files {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		inputs = append(inputs, asTurnStartInput{
			Type: "mention",
			Name: mentionNameFromPath(path),
			Path: path,
		})
	}

	if len(inputs) == 0 {
		inputs = append(inputs, asTurnStartInput{Type: "text", Text: prompt})
	}
	return inputs
}

func ensureListenerViaThreadResume(
	threadID string,
	rpcCall func(method string, params any, timeout time.Duration) (json.RawMessage, error),
) (string, error) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", apperrors.New("ensureListenerViaThreadResume", "thread id is required")
	}
	if rpcCall == nil {
		return "", apperrors.New("ensureListenerViaThreadResume", "rpc call func is nil")
	}

	result, err := rpcCall("thread/resume", asThreadResumeParams{
		ThreadID: id,
	}, appServerListenerEnsureTimeout)
	if err != nil {
		return "", apperrors.Wrap(err, "ensureListenerViaThreadResume", "thread/resume")
	}

	resolvedID, err := parseThreadResumeResult(result, id)
	if err != nil {
		return "", err
	}
	return resolvedID, nil
}

func (c *AppServerClient) ensureListenerIfNeeded(
	trigger string,
	rpcCall func(method string, params any, timeout time.Duration) (json.RawMessage, error),
) {
	if c == nil || !c.listenerEnsureNeeded.Load() {
		return
	}
	threadID := strings.TrimSpace(c.ThreadID)
	if threadID == "" {
		return
	}
	if !c.listenerEnsureInFlight.CompareAndSwap(false, true) {
		return
	}
	defer c.listenerEnsureInFlight.Store(false)

	callFn := rpcCall
	if callFn == nil {
		callFn = c.call
	}
	resolvedID, err := ensureListenerViaThreadResume(threadID, callFn)
	if err != nil {
		if isMethodNotFoundRPCError(err) || isInvalidParamsRPCError(err) {
			c.listenerEnsureNeeded.Store(false)
			logger.Debug("codex: listener ensure unsupported, disable",
				logger.FieldAgentID, c.AgentID,
				logger.FieldThreadID, threadID,
				"trigger", strings.TrimSpace(trigger),
				logger.FieldError, err,
			)
			return
		}
		logger.Warn("codex: listener ensure failed, keep pending",
			logger.FieldAgentID, c.AgentID,
			logger.FieldThreadID, threadID,
			"trigger", strings.TrimSpace(trigger),
			logger.FieldError, err,
		)
		return
	}

	if strings.EqualFold(strings.TrimSpace(c.ThreadID), threadID) {
		c.ThreadID = resolvedID
	}
	c.listenerEnsureNeeded.Store(false)
}

func (c *AppServerClient) ensureListenerIfNeededAsync(
	trigger string,
	rpcCall func(method string, params any, timeout time.Duration) (json.RawMessage, error),
) {
	util.SafeGo(func() {
		c.ensureListenerIfNeeded(trigger, rpcCall)
	})
}

// Submit 发送用户 prompt (app-server JSON-RPC turn/start)。
func (c *AppServerClient) Submit(prompt string, images, files []string, outputSchema json.RawMessage) error {
	c.ensureListenerIfNeeded("turn/start", c.call)

	inputs := buildTurnStartInputs(prompt, images, files)

	params := map[string]any{
		"threadId": strings.TrimSpace(c.ThreadID),
		"input":    inputs,
	}
	if len(outputSchema) > 0 {
		params["outputSchema"] = json.RawMessage(outputSchema)
	}

	result, err := c.call("turn/start", params, 10*time.Second)
	if err != nil {
		return err
	}
	if turnID := extractTurnIDFromEventData(result); turnID != "" {
		c.setActiveTurnID(turnID)
		logger.Debug("codex: active turn set from turn/start response",
			logger.FieldAgentID, c.AgentID,
			logger.FieldThreadID, c.ThreadID,
			"turn_id", turnID,
		)
	} else {
		logger.Warn("codex: turn/start response missing turn id",
			logger.FieldAgentID, c.AgentID,
			logger.FieldThreadID, c.ThreadID,
			logger.FieldRaw, truncateBytes(result, 200),
		)
	}
	return nil
}

// SendCommand 发送斜杠命令 (通知, 无需响应)。
func (c *AppServerClient) SendCommand(cmd, args string) error {
	trimmedCmd := strings.TrimSpace(cmd)
	if trimmedCmd == CmdInterrupt {
		threadID := strings.TrimSpace(c.ThreadID)
		if threadID == "" {
			return apperrors.New("AppServerClient.SendCommand", "interrupt requires active thread id")
		}
		turnID := strings.TrimSpace(c.getActiveTurnID())
		tryTurnInterrupt := func(turnScope string) error {
			params := map[string]any{
				"threadId": threadID,
			}
			if turnScope == "with_turn_id" {
				params["turnId"] = turnID
			}
			_, err := c.call("turn/interrupt", params, appServerInterruptTimeout)
			return err
		}

		if turnID != "" {
			err := tryTurnInterrupt("with_turn_id")
			if err == nil {
				logger.Info("codex: turn/interrupt OK",
					logger.FieldAgentID, c.AgentID,
					logger.FieldThreadID, threadID,
					"turn_id", turnID,
				)
				return nil
			}
			if isInterruptTurnIDMismatchError(err) {
				logger.Warn("codex: turn/interrupt turn_id mismatch, retry thread-scoped interrupt",
					logger.FieldAgentID, c.AgentID,
					logger.FieldThreadID, threadID,
					"turn_id", turnID,
					logger.FieldError, err,
				)
				if retryErr := tryTurnInterrupt("thread_scoped"); retryErr == nil {
					logger.Info("codex: turn/interrupt thread-scoped retry OK",
						logger.FieldAgentID, c.AgentID,
						logger.FieldThreadID, threadID,
						"turn_id", turnID,
					)
					return nil
				} else {
					err = retryErr
				}
			}
			if !isMethodNotFoundRPCError(err) && !isInvalidParamsRPCError(err) {
				logger.Warn("codex: turn/interrupt FAILED, fallback to interruptConversation",
					logger.FieldAgentID, c.AgentID,
					logger.FieldThreadID, threadID,
					"turn_id", turnID,
					logger.FieldError, err,
				)
			} else {
				logger.Warn("codex: turn/interrupt unsupported, fallback to interruptConversation",
					logger.FieldAgentID, c.AgentID,
					logger.FieldThreadID, threadID,
					"turn_id", turnID,
					logger.FieldError, err,
				)
			}
		} else {
			logger.Warn("codex: missing active turn id, trying thread-scoped turn/interrupt",
				logger.FieldAgentID, c.AgentID,
				logger.FieldThreadID, threadID,
			)
			err := tryTurnInterrupt("thread_scoped")
			if err == nil {
				logger.Info("codex: turn/interrupt thread-scoped OK",
					logger.FieldAgentID, c.AgentID,
					logger.FieldThreadID, threadID,
				)
				return nil
			}
			if !isMethodNotFoundRPCError(err) && !isInvalidParamsRPCError(err) {
				logger.Warn("codex: turn/interrupt thread-scoped FAILED, fallback to interruptConversation",
					logger.FieldAgentID, c.AgentID,
					logger.FieldThreadID, threadID,
					logger.FieldError, err,
				)
			} else {
				logger.Warn("codex: turn/interrupt thread-scoped unsupported, fallback to interruptConversation",
					logger.FieldAgentID, c.AgentID,
					logger.FieldThreadID, threadID,
					logger.FieldError, err,
				)
			}
		}

		_, err := c.call("interruptConversation", map[string]any{
			"conversationId": threadID,
		}, appServerInterruptTimeout)
		if err == nil {
			logger.Info("codex: interruptConversation OK",
				logger.FieldAgentID, c.AgentID,
				logger.FieldThreadID, threadID,
			)
			return nil
		}
		if !isMethodNotFoundRPCError(err) {
			logger.Warn("codex: interruptConversation FAILED",
				logger.FieldAgentID, c.AgentID,
				logger.FieldThreadID, threadID,
				logger.FieldError, err,
			)
			return err
		}
		logger.Warn("codex: interruptConversation unsupported, fallback to slash command",
			logger.FieldAgentID, c.AgentID,
			logger.FieldThreadID, threadID,
			logger.FieldError, err,
		)
	}
	threadID := strings.TrimSpace(c.ThreadID)
	command := strings.TrimSpace(cmd)
	logger.Info("codex: command notify sending",
		logger.FieldAgentID, c.AgentID,
		logger.FieldThreadID, threadID,
		logger.FieldCommand, command,
		"args_len", len(strings.TrimSpace(args)),
	)
	if err := c.notify("command", map[string]any{
		"threadId": c.ThreadID,
		"command":  cmd,
		"args":     args,
	}); err != nil {
		logger.Warn("codex: command notify failed",
			logger.FieldAgentID, c.AgentID,
			logger.FieldThreadID, threadID,
			logger.FieldCommand, command,
			logger.FieldError, err,
		)
		return err
	}
	logger.Info("codex: command notify sent",
		logger.FieldAgentID, c.AgentID,
		logger.FieldThreadID, threadID,
		logger.FieldCommand, command,
	)
	return nil
}

func isMethodNotFoundRPCError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "method not found") || strings.Contains(text, "code -32601")
}

func isInvalidParamsRPCError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "invalid params") || strings.Contains(text, "code -32602")
}

func isInterruptTurnIDMismatchError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "turn not found") ||
		strings.Contains(text, "unknown turn") ||
		strings.Contains(text, "invalid turn") ||
		(strings.Contains(text, "turn id") && strings.Contains(text, "mismatch")) ||
		(strings.Contains(text, "turn_id") && strings.Contains(text, "mismatch"))
}

// SendDynamicToolResult 回传动态工具执行结果。
//
// codex 发动态工具调用是 Server Request (带 id), 需要回 JSON-RPC response。
// 响应体: {"contentItems": [{"type": "inputText", "text": "..."}], "success": true}
func (c *AppServerClient) SendDynamicToolResult(callID, output string, requestID *int64) error {
	result := DynamicToolCallResponse{
		ContentItems: []DynamicToolContentItem{{
			Type: "inputText",
			Text: output,
		}},
		Success: true,
	}

	if requestID != nil {
		// 正常路径: 回复 codex 的 server request
		return c.respond(*requestID, result)
	}

	// 兜底: 无 requestID 时用 notification (不应发生)
	logger.Warn("codex: SendDynamicToolResult without requestID, falling back to notification",
		logger.FieldAgentID, c.AgentID, logger.FieldCallID, callID)
	params := map[string]any{
		"threadId": c.ThreadID,
		"callId":   callID,
		// 兼容不同版本字段命名
		"toolCallId":   callID,
		"tool_call_id": callID,
		"output":       output,
		// 兼容可能期望响应体结构的实现
		"result":       result,
		"contentItems": result.ContentItems,
		"success":      true,
	}
	return c.notify("dynamic_tool_result", params)
}

// ListThreads 返回线程列表 (app-server 模式下只有当前线程)。
func (c *AppServerClient) ListThreads() ([]ThreadInfo, error) {
	if c.ThreadID == "" {
		return nil, nil
	}
	return []ThreadInfo{{ThreadID: c.ThreadID}}, nil
}

type asThreadResumeParams struct {
	ThreadID string `json:"threadId"`
	Path     string `json:"path,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
}

func parseThreadResumeResult(raw json.RawMessage, fallbackID string) (string, error) {
	fallback := strings.TrimSpace(fallbackID)
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw)) == "null" {
		if fallback == "" {
			return "", apperrors.New("parseThreadResumeResult", "thread/resume returned empty response without fallback thread ID")
		}
		return fallback, nil
	}

	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", apperrors.Wrap(err, "parseThreadResumeResult", "thread/resume decode")
	}
	if id := strings.TrimSpace(resp.Thread.ID); id != "" {
		return id, nil
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", apperrors.New("parseThreadResumeResult", "thread/resume returned empty thread ID")
}

// ResumeThread 恢复会话 (app-server JSON-RPC thread/resume)。
func (c *AppServerClient) ResumeThread(req ResumeThreadRequest) error {
	id := strings.TrimSpace(req.ThreadID)
	if id == "" {
		return apperrors.New("AppServerClient.ResumeThread", "thread/resume requires thread ID")
	}
	path := strings.TrimSpace(req.Path)
	cwd := strings.TrimSpace(req.Cwd)
	logger.Info("[DIAG] ResumeThread: calling thread/resume",
		logger.FieldAgentID, c.AgentID,
		"request_thread_id", id,
		"request_path", path,
		"request_cwd", cwd,
		"current_thread_id", c.ThreadID,
	)
	result, err := c.call("thread/resume", asThreadResumeParams{
		ThreadID: id,
		Path:     path,
		Cwd:      cwd,
	}, 30*time.Second)
	if err != nil {
		logger.Warn("[DIAG] ResumeThread: thread/resume RPC failed",
			logger.FieldAgentID, c.AgentID,
			"request_thread_id", id,
			logger.FieldError, err,
		)
		return apperrors.Wrap(err, "AppServerClient.ResumeThread", "thread/resume")
	}
	logger.Info("[DIAG] ResumeThread: raw response",
		logger.FieldAgentID, c.AgentID,
		"request_thread_id", id,
		"raw_response_len", len(result),
		"raw_response_preview", truncStr(string(result), 500),
	)
	resolvedID, err := parseThreadResumeResult(result, id)
	if err != nil {
		logger.Warn("[DIAG] ResumeThread: parseThreadResumeResult failed",
			logger.FieldAgentID, c.AgentID,
			"request_thread_id", id,
			logger.FieldError, err,
		)
		return err
	}
	idMatch := "RESUMED_SAME_ID"
	if resolvedID != id {
		idMatch = "FORKED_NEW_ID"
	}
	logger.Info("[DIAG] ResumeThread: success",
		logger.FieldAgentID, c.AgentID,
		"request_thread_id", id,
		"resolved_thread_id", resolvedID,
		"id_match", idMatch,
	)
	c.ThreadID = resolvedID
	c.listenerEnsureNeeded.Store(false)
	return nil
}

// ForkThread 分叉会话 (app-server 模式暂不支持)。
func (c *AppServerClient) ForkThread(_ ForkThreadRequest) (*ForkThreadResponse, error) {
	return nil, apperrors.New("AppServerClient.ForkThread", "fork not supported in app-server mode")
}

// ========================================
// readLoop — 读取 JSON-RPC 消息
// ========================================

// readLoop 持续读取 WebSocket JSON-RPC 消息。
//
// 消息类型:
//   - Response (id != nil): 交给 pending call
//   - Notification (id == nil): 转为 Event, 交给 handler
func (c *AppServerClient) readLoop() {
	defer func() {
		c.wsMu.Lock()
		if c.ws != nil {
			_ = c.ws.Close()
		}
		c.wsMu.Unlock()
		c.failPendingCalls(apperrors.New("AppServerClient.readLoop", "connection closed"))

		select {
		case <-c.wsDone:
		default:
			close(c.wsDone)
		}
	}()

	for !c.stopped.Load() {
		conn := c.currentWSConn()
		if conn == nil {
			if c.stopped.Load() {
				return
			}
			if !c.reconnectWS("ws_missing", apperrors.New("AppServerClient.readLoop", "ws not connected")) {
				return
			}
			continue
		}
		_, message, err := conn.ReadMessage()
		if err == nil {
			// 收到有效消息 = 连接活跃, 重置 idle deadline。
			// 注意: 必须用循环内的 conn 局部变量, 不能用 c.currentWSConn(),
			// 因为 reconnect 后 c.ws 已指向新 conn。
			_ = conn.SetReadDeadline(time.Now().Add(appServerReadIdleTimeout))
		}
		if err != nil {
			readErr := apperrors.Wrap(err, "AppServerClient.readLoop", "read message")
			c.failPendingCalls(readErr)
			if !c.stopped.Load() {
				willRetry := appServerStreamMaxRetries > 0
				reconnectingMessage := "Reconnecting..."
				if !willRetry {
					reconnectingMessage = "Stream disconnected"
				}
				c.emitStreamError(readErr, "read", isIdleTimeoutError(err), willRetry, map[string]any{
					"message":     reconnectingMessage,
					"attempt":     0,
					"max_retries": appServerStreamMaxRetries,
					"trigger":     "read_error",
				})
			}
			if c.stopped.Load() && isShutdownReadError(err) {
				logger.Debug("codex: readLoop read failed (shutdown)",
					logger.FieldAgentID, c.AgentID,
					logger.FieldError, readErr,
				)
			} else {
				logger.Warn("codex: readLoop read failed",
					logger.FieldAgentID, c.AgentID,
					"active_turn_id", c.getActiveTurnID(),
					"idle_timeout", isIdleTimeoutError(err),
					logger.FieldError, readErr,
				)
			}
			if c.stopped.Load() {
				return
			}
			if c.reconnectWS("read_error", readErr) {
				continue
			}
			return
		}

		var msg jsonRPCMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Warn("codex: readLoop unparseable JSON-RPC message",
				logger.FieldAgentID, c.AgentID,
				logger.FieldError, err,
				"raw_len", len(message),
				"raw_prefix", truncateBytes(message, 200),
			)
			continue
		}
		if dropped, preview, convID := shouldDropLegacyMirrorNotification(msg); dropped {
			seq := c.legacyMirrorDropCount.Add(1)
			if shouldLogLegacyMirrorDrop(seq) {
				logger.Info("codex: dropped legacy mirror stream notification (sampled)",
					logger.FieldAgentID, c.AgentID,
					logger.FieldMethod, msg.Method,
					"conversation_id", convID,
					"preview", preview,
					"drop_count", seq,
				)
			} else {
				logger.Debug("codex: dropped legacy mirror stream notification",
					logger.FieldAgentID, c.AgentID,
					logger.FieldMethod, msg.Method,
					"conversation_id", convID,
					"preview", preview,
				)
			}
			continue
		}

		// Response: 交给 pending call
		if c.handleRPCResponse(msg) {
			continue
		}

		if c.handleRPCEvent(msg) {
			return
		}
	}
}

func (c *AppServerClient) pingLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(appServerPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.wsDone:
			return
		case <-ticker.C:
			c.wsMu.Lock()
			if c.ws != conn {
				c.wsMu.Unlock()
				return
			}
			err := c.ws.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(appServerWriteTimeout))
			if err != nil {
				_ = c.ws.Close()
				c.ws = nil
				c.wsMu.Unlock()
				return
			}
			c.wsMu.Unlock()
		}
	}
}

func (c *AppServerClient) handleRPCResponse(msg jsonRPCMessage) bool {
	if msg.ID == nil || msg.Method != "" {
		return false
	}
	value, ok := c.pending.Load(*msg.ID)
	if !ok {
		logger.Warn("codex: orphan RPC response (no pending call)",
			logger.FieldAgentID, c.AgentID,
			logger.FieldID, *msg.ID,
			"result_len", len(msg.Result),
		)
		return true
	}
	pc := value.(*pendingCall)
	if msg.Error != nil {
		pc.resolve(nil, apperrors.Newf("AppServerClient.readLoop", "rpc error: %s (code %d)", msg.Error.Message, msg.Error.Code))
		logger.Warn("codex: RPC error response",
			logger.FieldAgentID, c.AgentID,
			logger.FieldID, *msg.ID,
			"code", msg.Error.Code,
			"message", msg.Error.Message,
		)
	} else {
		pc.resolve(msg.Result, nil)
	}
	return true
}

func (c *AppServerClient) handleRPCEvent(msg jsonRPCMessage) bool {
	event := c.jsonRPCToEvent(msg)
	// DenyFunc: 审批事件 proc==nil 时自动拒绝, 闭包捕获当前 client。
	event.DenyFunc = func() error { return c.Submit("no", nil, nil, nil) }
	if event.Type == "" {
		logger.Warn("codex: readLoop skipped message with empty event type",
			logger.FieldAgentID, c.AgentID,
			logger.FieldMethod, msg.Method,
			"has_id", msg.ID != nil,
			logger.FieldParamsLen, len(msg.Params),
		)
		return false
	}
	if msg.ID != nil && msg.Method != "" {
		event.RequestID = msg.ID
		reqID := *msg.ID
		event.RespondFunc = func(code int, message string) error {
			return c.RespondError(reqID, code, message)
		}
		logger.Debug("codex: server request received",
			logger.FieldAgentID, c.AgentID,
			logger.FieldID, *msg.ID,
			logger.FieldMethod, msg.Method,
			logger.FieldEventType, event.Type,
		)
	}
	// 跟踪活跃 turn 生命周期
	c.trackTurnLifecycle(event, msg.Method)

	c.handlerMu.RLock()
	handler := c.handler
	c.handlerMu.RUnlock()
	if handler == nil {
		logger.Warn("codex: readLoop dropping event (no handler registered)",
			logger.FieldAgentID, c.AgentID,
			logger.FieldEventType, event.Type,
			logger.FieldMethod, msg.Method,
		)
		return false
	}
	handler(event)
	return event.Type == EventShutdownComplete
}

// trackTurnLifecycle 从事件中提取并维护当前活跃 turnId。
func (c *AppServerClient) trackTurnLifecycle(event Event, method string) {
	method = strings.TrimSpace(method)
	activeTurnID := c.getActiveTurnID()

	switch event.Type {
	case EventTurnStarted:
		if turnID := extractTurnIDFromEventData(event.Data); turnID != "" {
			c.setActiveTurnID(turnID)
			logger.Debug("codex: active turn set",
				logger.FieldAgentID, c.AgentID,
				"turn_id", turnID,
			)
			return
		}
		logger.Warn("codex: turn started event missing turn id",
			logger.FieldAgentID, c.AgentID,
			logger.FieldMethod, method,
			logger.FieldEventType, event.Type,
			logger.FieldDataLen, len(event.Data),
			"active_turn_id_before", activeTurnID,
		)
	case EventTurnComplete, "turn_aborted", EventIdle, EventError, EventShutdownComplete:
		if activeTurnID != "" {
			c.clearActiveTurnID()
			logger.Debug("codex: active turn cleared",
				logger.FieldAgentID, c.AgentID,
				logger.FieldEventType, event.Type,
				logger.FieldMethod, method,
				"prev_turn_id", activeTurnID,
			)
		}
	case EventStreamError:
		if streamErrorWillRetry(event.Data) {
			return
		}
		if activeTurnID != "" {
			c.clearActiveTurnID()
			logger.Debug("codex: active turn cleared by non-retryable stream error",
				logger.FieldAgentID, c.AgentID,
				logger.FieldEventType, event.Type,
				logger.FieldMethod, method,
				"prev_turn_id", activeTurnID,
			)
		}
	default:
		if activeTurnID != "" && isTurnTailProgressEvent(event.Type, method) {
			logger.Info("codex: active turn observed progress event without terminal yet",
				logger.FieldAgentID, c.AgentID,
				"active_turn_id", activeTurnID,
				logger.FieldEventType, event.Type,
				logger.FieldMethod, method,
				logger.FieldDataLen, len(event.Data),
			)
		}
	}
}

func isTurnTailProgressEvent(eventType, method string) bool {
	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))

	switch methodKey {
	case "turn/diff/updated", "turn/plan/updated", "codex/event/turn_diff", "codex/event/plan_delta", "codex/event/plan_update":
		return true
	}
	switch eventKey {
	case EventTurnDiff, EventPlanDelta, EventPlanUpdate:
		return true
	default:
		return false
	}
}

func extractTurnIDFromEventData(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return extractTurnIDFromPayload(payload)
}

func extractTurnIDFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if id := trimmedStringValue(payload["turnId"]); id != "" {
		return id
	}
	if id := trimmedStringValue(payload["turn_id"]); id != "" {
		return id
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if id := trimmedStringValue(turn["id"]); id != "" {
			return id
		}
		if id := trimmedStringValue(turn["turnId"]); id != "" {
			return id
		}
	}
	for _, key := range []string{"msg", "data", "payload"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if id := extractTurnIDFromPayload(nested); id != "" {
			return id
		}
	}
	return ""
}

func trimmedStringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func (c *AppServerClient) getActiveTurnID() string {
	v := c.activeTurnID.Load()
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func (c *AppServerClient) setActiveTurnID(id string) {
	c.activeTurnID.Store(id)
}

func (c *AppServerClient) clearActiveTurnID() {
	c.activeTurnID.Store("")
}

// jsonRPCToEvent 将 JSON-RPC notification 转为 codex Event。
//
// app-server 通知方法映射:
//
//	"agent/event/agent_message_content_delta" → "agent_message_delta"
//	"agent/event/turn_completed" → "turn_complete"
//	"agent/event/dynamic_tool_call" → "dynamic_tool_call"
//	"agent/event/mcp_startup_complete" → "mcp_startup_complete"
//	etc.
//
// methodToEventMap 包级 var — 零分配热路径查找。
//
// JSON-RPC notification method → codex Event type。
var methodToEventMap = map[string]string{
	// v2 server notifications
	"error":                                     EventError,
	"thread/started":                            EventSessionConfigured,
	"thread/name/updated":                       EventThreadNameUpdated,
	"thread/tokenUsage/updated":                 EventTokenCount,
	"turn/started":                              EventTurnStarted,
	"turn/completed":                            EventTurnComplete,
	"turn/aborted":                              "turn_aborted",
	"turn/diff/updated":                         EventTurnDiff,
	"turn/plan/updated":                         EventPlanUpdate,
	"item/started":                              "item/started",
	"item/completed":                            "item/completed",
	"rawResponseItem/completed":                 "rawResponseItem/completed",
	"item/agentMessage/delta":                   EventAgentMessageDelta,
	"item/plan/delta":                           EventPlanDelta,
	"item/commandExecution/outputDelta":         EventExecCommandOutputDelta,
	"item/commandExecution/terminalInteraction": "item/commandExecution/terminalInteraction",
	"item/fileChange/outputDelta":               "item/fileChange/outputDelta",
	"item/mcpToolCall/progress":                 "item/mcpToolCall/progress",
	"mcpServer/oauthLogin/completed":            "mcpServer/oauthLogin/completed",
	"account/updated":                           "account/updated",
	"account/rateLimits/updated":                "account/rateLimits/updated",
	"app/list/updated":                          "app/list/updated",
	"item/reasoning/summaryTextDelta":           EventAgentReasoningDelta,
	"item/reasoning/summaryPartAdded":           EventAgentReasoningSectionBreak,
	"item/reasoning/textDelta":                  EventAgentReasoningRawDelta,
	"thread/compacted":                          EventContextCompacted,
	"deprecationNotice":                         "deprecationNotice",
	"configWarning":                             EventWarning,
	"fuzzyFileSearch/sessionUpdated":            "fuzzyFileSearch/sessionUpdated",
	"fuzzyFileSearch/sessionCompleted":          "fuzzyFileSearch/sessionCompleted",
	"windows/worldWritableWarning":              EventWarning,
	"account/login/completed":                   "account/login/completed",
	"authStatusChange":                          "authStatusChange",
	"loginChatGptComplete":                      "loginChatGptComplete",
	"sessionConfigured":                         EventSessionConfigured,

	// v2 server requests
	"item/commandExecution/requestApproval": EventExecApprovalRequest,
	"item/fileChange/requestApproval":       "item/fileChange/requestApproval",
	"item/tool/requestUserInput":            "item/tool/requestUserInput",
	"item/tool/call":                        EventDynamicToolCall,
	"account/chatgptAuthTokens/refresh":     "account/chatgptAuthTokens/refresh",
	"applyPatchApproval":                    "applyPatchApproval",
	"execCommandApproval":                   EventExecApprovalRequest,

	// Agent 输出
	"agent/event/agent_message_content_delta":   EventAgentMessageDelta,
	"agent/event/agent_message_delta":           EventAgentMessageDelta,
	"agent/event/agent_message":                 EventAgentMessage,
	"agent/event/agent_reasoning":               EventAgentReasoning,
	"agent/event/agent_reasoning_raw":           EventAgentReasoningRaw,
	"agent/event/agent_reasoning_raw_delta":     EventAgentReasoningRawDelta,
	"agent/event/agent_reasoning_section_break": EventAgentReasoningSectionBreak,
	"agent/event/agent_reasoning_delta":         EventAgentReasoningDelta,
	"agent/event/agent_message_completed":       EventAgentMessageCompleted,

	// 生命周期
	"agent/event/turn_started":         EventTurnStarted,
	"agent/event/turn_completed":       EventTurnComplete,
	"agent/event/turn_aborted":         "turn_aborted",
	"agent/event/session_configured":   EventSessionConfigured,
	"agent/event/mcp_startup_complete": EventMCPStartupComplete,
	"agent/event/mcp_startup_update":   "agent/event/mcp_startup_update",
	"agent/event/shutdown_complete":    EventShutdownComplete,
	"agent/event/error":                EventError,
	"agent/event/stream_error":         EventStreamError,
	"agent/event/warning":              EventWarning,

	// 命令执行
	"agent/event/exec_approval_request":     EventExecApprovalRequest,
	"agent/event/exec_command_begin":        EventExecCommandBegin,
	"agent/event/exec_command_end":          EventExecCommandEnd,
	"agent/event/exec_command_output_delta": EventExecCommandOutputDelta,

	// 代码修改
	"agent/event/patch_apply_begin": EventPatchApplyBegin,
	"agent/event/patch_apply_end":   EventPatchApplyEnd,

	// MCP
	"agent/event/mcp_tool_call_begin":     EventMCPToolCallBegin,
	"agent/event/mcp_tool_call_end":       EventMCPToolCallEnd,
	"agent/event/mcp_list_tools_response": EventMCPListToolsResponse,
	"agent/event/list_skills_response":    EventListSkillsResponse,

	// Dynamic Tools
	// 注意:
	//   - `item/tool/call` 才是 v2 正式 Server Request（需 JSON-RPC response）。
	//   - `codex/event/dynamic_tool_call_request` 是 raw event 通知副本，不应驱动工具回传。
	//     否则会出现“处理了通知副本但未响应真实 request”，导致 turn 卡住。
	//
	// 兼容保留:
	//   - agent/event/dynamic_tool_call
	//   - codex/event/dynamic_tool_call
	"agent/event/dynamic_tool_call": EventDynamicToolCall,
	"codex/event/dynamic_tool_call": EventDynamicToolCall,

	// Collab
	"agent/event/collab_agent_spawn_begin":       EventCollabAgentSpawnBegin,
	"agent/event/collab_agent_spawn_end":         EventCollabAgentSpawnEnd,
	"agent/event/collab_agent_interaction_begin": EventCollabAgentInteractionBegin,
	"agent/event/collab_agent_interaction_end":   EventCollabAgentInteractionEnd,

	// legacy codex/event/*
	"codex/event/task_started": EventTurnStarted,
	// ⚠️ DO NOT DELETE / DO NOT MODIFY — 以下注释和行为是刻意设计。
	// `codex/event/task_complete` 故意不映射为 EventTurnComplete, 因为 v2 协议会单独发送 turn/completed。
	// 若映射则会导致 turn_complete 被双重触发 (一次来自此映射, 一次来自 turn/completed)。
	// runner 层通过 uistate.NormalizeEvent 独立处理 last_agent_message 提取, 不依赖此映射。
	"codex/event/session_configured":             EventSessionConfigured,
	"codex/event/agent_message":                  EventAgentMessage,
	"codex/event/agent_message_delta":            EventAgentMessageDelta,
	"codex/event/agent_message_content_delta":    EventAgentMessageDelta,
	"codex/event/agent_message_completed":        EventAgentMessageCompleted,
	"codex/event/agent_reasoning":                EventAgentReasoning,
	"codex/event/agent_reasoning_delta":          EventAgentReasoningDelta,
	"codex/event/agent_reasoning_raw":            EventAgentReasoningRaw,
	"codex/event/agent_reasoning_raw_delta":      EventAgentReasoningRawDelta,
	"codex/event/agent_reasoning_section_break":  EventAgentReasoningSectionBreak,
	"codex/event/reasoning_content_delta":        EventAgentReasoningDelta,
	"codex/event/exec_approval_request":          EventExecApprovalRequest,
	"codex/event/exec_command_begin":             EventExecCommandBegin,
	"codex/event/exec_command_end":               EventExecCommandEnd,
	"codex/event/exec_command_output_delta":      EventExecCommandOutputDelta,
	"codex/event/patch_apply_begin":              EventPatchApplyBegin,
	"codex/event/patch_apply_end":                EventPatchApplyEnd,
	"codex/event/mcp_tool_call_begin":            EventMCPToolCallBegin,
	"codex/event/mcp_tool_call_end":              EventMCPToolCallEnd,
	"codex/event/mcp_list_tools_response":        EventMCPListToolsResponse,
	"codex/event/list_skills_response":           EventListSkillsResponse,
	"codex/event/mcp_startup_complete":           EventMCPStartupComplete,
	"codex/event/mcp_startup_update":             "codex/event/mcp_startup_update",
	"codex/event/token_count":                    EventTokenCount,
	"codex/event/context_compacted":              EventContextCompacted,
	"codex/event/thread_name_updated":            EventThreadNameUpdated,
	"codex/event/thread_rolled_back":             EventThreadRolledBack,
	"codex/event/plan_delta":                     EventPlanDelta,
	"codex/event/plan_update":                    EventPlanUpdate,
	"codex/event/collab_agent_spawn_begin":       EventCollabAgentSpawnBegin,
	"codex/event/collab_agent_spawn_end":         EventCollabAgentSpawnEnd,
	"codex/event/collab_agent_interaction_begin": EventCollabAgentInteractionBegin,
	"codex/event/collab_agent_interaction_end":   EventCollabAgentInteractionEnd,
	"codex/event/item_started":                   "item/started",
	"codex/event/item_completed":                 "item/completed",
	"codex/event/raw_response_item":              "rawResponseItem/completed",
	"codex/event/error":                          EventError,
	"codex/event/stream_error":                   EventStreamError,
	"codex/event/warning":                        EventWarning,
	"codex/event/shutdown_complete":              EventShutdownComplete,
}

var mappedMethodPrefixes = [...]string{
	"thread/",
	"turn/",
	"item/",
	"account/",
	"app/",
	"mcpServer/",
	"fuzzyFileSearch/",
	"rawResponseItem/",
	"windows/",
	"codex/event/",
	"agent/event/",
}

func mapMethodToEventType(method string) (string, bool) {
	if eventType, ok := methodToEventMap[method]; ok {
		return eventType, true
	}

	for _, prefix := range mappedMethodPrefixes {
		if strings.HasPrefix(method, prefix) {
			return method, true
		}
	}

	return "", false
}

func (c *AppServerClient) jsonRPCToEvent(msg jsonRPCMessage) Event {
	eventType, ok := mapMethodToEventType(msg.Method)
	if !ok {
		// 未知方法 → 用 method 名作为 type (兼容) + 警告日志
		eventType = msg.Method
		logger.Warn("codex: unmapped JSON-RPC method → using raw method as event type",
			logger.FieldAgentID, c.AgentID,
			logger.FieldMethod, msg.Method,
			logger.FieldParamsLen, len(msg.Params),
		)
	}
	normalizedParams := msg.Params
	if strings.EqualFold(strings.TrimSpace(msg.Method), "error") {
		normalizedParams = normalizeErrorNotificationPayload(msg.Params)
		if streamErrorWillRetry(normalizedParams) {
			eventType = EventStreamError
		}
	}

	return Event{Type: eventType, Data: normalizedParams}
}

func normalizeErrorNotificationPayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw
	}
	if payload == nil {
		return raw
	}

	if errObj, ok := payload["error"].(map[string]any); ok && errObj != nil {
		if _, exists := payload["message"]; !exists {
			if msg := strings.TrimSpace(trimmedStringValue(errObj["message"])); msg != "" {
				payload["message"] = msg
			}
		}
		if _, exists := payload["additional_details"]; !exists {
			if details := strings.TrimSpace(trimmedStringValue(errObj["additionalDetails"])); details != "" {
				payload["additional_details"] = details
			} else if details := strings.TrimSpace(trimmedStringValue(errObj["additional_details"])); details != "" {
				payload["additional_details"] = details
			}
		}
	}

	if _, exists := payload["willRetry"]; !exists {
		if value, ok := payload["will_retry"]; ok {
			payload["willRetry"] = value
		}
	}
	if _, exists := payload["will_retry"]; !exists {
		if value, ok := payload["willRetry"]; ok {
			payload["will_retry"] = value
		}
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return normalized
}

// ========================================
// 辅助
// ========================================

// truncateBytes 截断 []byte 用于日志展示, 避免超长。
func truncateBytes(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}

// isShutdownReadError 判断 readLoop 错误是否由正常关闭触发。
// shutdown 引起的 "use of closed network connection" 不需要 WARN, 降级为 DEBUG。
func isShutdownReadError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

// legacyMirrorDropLogSampleInterval 每 N 次 legacy mirror 丢弃打一次 INFO 采样日志。
const legacyMirrorDropLogSampleInterval int64 = 100

// shouldLogLegacyMirrorDrop 决定第 seq 次 legacy mirror 丢弃是否应打 INFO 日志。
// 策略: 第 1 次 + 每 100 次。其余降级为 DEBUG。
func shouldLogLegacyMirrorDrop(seq int64) bool {
	return seq == 1 || seq%legacyMirrorDropLogSampleInterval == 0
}

var legacyMirrorStreamMethods = map[string]struct{}{
	"agent/event/agent_message_delta":         {},
	"agent/event/agent_message_content_delta": {},
	"codex/event/agent_message_delta":         {},
	"codex/event/agent_message_content_delta": {},
	"agent/event/agent_reasoning_delta":       {},
	"agent/event/agent_reasoning_raw_delta":   {},
	"codex/event/agent_reasoning_delta":       {},
	"codex/event/agent_reasoning_raw_delta":   {},
	"codex/event/reasoning_content_delta":     {},
	"agent/event/exec_command_output_delta":   {},
	"codex/event/exec_command_output_delta":   {},
	"codex/event/plan_delta":                  {},
}

func shouldDropLegacyMirrorNotification(msg jsonRPCMessage) (bool, string, string) {
	if msg.ID != nil {
		return false, "", ""
	}

	var payload map[string]any
	if len(msg.Params) == 0 || json.Unmarshal(msg.Params, &payload) != nil {
		return false, "", ""
	}
	if payload == nil {
		return false, "", ""
	}

	conversationID, hasConversationID := payload["conversationId"].(string)
	if !hasConversationID || strings.TrimSpace(conversationID) == "" {
		return false, "", ""
	}

	msgObj, ok := payload["msg"].(map[string]any)
	if !ok {
		return false, "", ""
	}
	preview := extractLegacyMirrorPreview(msgObj)
	if preview == "" {
		return false, "", ""
	}

	if !isLegacyMirrorEnvelope(msg.Method, payload) {
		return false, "", ""
	}
	return true, preview, conversationID
}

func isLegacyMirrorEnvelope(method string, payload map[string]any) bool {
	if _, ok := legacyMirrorStreamMethods[method]; ok {
		return true
	}

	if _, ok := payload["threadId"]; ok {
		return false
	}
	if _, ok := payload["turnId"]; ok {
		return false
	}
	if _, ok := payload["itemId"]; ok {
		return false
	}
	if _, ok := payload["outputIndex"]; ok {
		return false
	}
	if _, ok := payload["contentIndex"]; ok {
		return false
	}
	_, hasLegacyID := payload["id"]
	return hasLegacyID
}

func extractLegacyMirrorPreview(msgObj map[string]any) string {
	for _, key := range []string{"delta", "text", "content", "output", "message"} {
		value, ok := msgObj[key].(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		return truncateString(trimmed, 80)
	}
	return ""
}

func truncateString(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "...(truncated)"
}

// asWriteJSON 线程安全写入 WebSocket JSON。
func (c *AppServerClient) asWriteJSON(v any) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.ws == nil {
		err := apperrors.New("AppServerClient.asWriteJSON", "ws not connected")
		c.failPendingCalls(err)
		return err
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(appServerWriteTimeout))
	if err := c.ws.WriteJSON(v); err != nil {
		writeErr := apperrors.Wrap(err, "AppServerClient.asWriteJSON", "ws write")
		_ = c.ws.Close()
		c.ws = nil
		c.failPendingCalls(writeErr)
		return writeErr
	}
	return nil
}

func (c *AppServerClient) failPendingCalls(err error) {
	if err == nil {
		err = apperrors.New("AppServerClient.failPendingCalls", "connection unavailable")
	}
	c.pending.Range(func(_, value any) bool {
		if call, ok := value.(*pendingCall); ok {
			call.resolve(nil, err)
		}
		return true
	})
}

func (c *AppServerClient) emitStreamError(err error, phase string, idleTimeout bool, willRetry bool, details map[string]any) {
	if err == nil {
		return
	}
	c.handlerMu.RLock()
	handler := c.handler
	c.handlerMu.RUnlock()
	if handler == nil {
		return
	}

	message := strings.TrimSpace(err.Error())
	payload := map[string]any{
		"message":     message,
		"phase":       strings.TrimSpace(phase),
		"recoverable": willRetry,
		"willRetry":   willRetry,
		"will_retry":  willRetry,
	}
	if details != nil {
		for key, value := range details {
			payload[key] = value
		}
		if override := strings.TrimSpace(trimmedStringValue(details["message"])); override != "" {
			payload["message"] = override
			if message != "" && !strings.EqualFold(message, override) {
				payload["additional_details"] = message
			}
		}
	}
	if c.AgentID != "" {
		payload["agentId"] = c.AgentID
	}
	if c.Port > 0 {
		payload["port"] = c.Port
	}
	if activeTurnID := c.getActiveTurnID(); activeTurnID != "" {
		payload["activeTurnId"] = activeTurnID
	}
	if idleTimeout {
		payload["reason"] = "idle_timeout"
	}
	data, _ := json.Marshal(payload)
	handler(Event{Type: EventStreamError, Data: data})
}

func streamErrorWillRetry(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return false
	}
	if value, ok := extractBoolValue(payload, "willRetry", "will_retry", "recoverable"); ok {
		return value
	}
	return false
}

func extractBoolValue(payload map[string]any, keys ...string) (bool, bool) {
	if payload == nil {
		return false, false
	}
	for _, key := range keys {
		value, exists := payload[key]
		if !exists {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "y":
				return true, true
			case "false", "0", "no", "n":
				return false, true
			}
		}
	}
	return false, false
}

func isIdleTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "i/o timeout") || strings.Contains(text, "read timeout")
}

// SpawnAndConnect 一键启动: spawn → ws connect → initialize → thread/start。
func (c *AppServerClient) SpawnAndConnect(ctx context.Context, prompt, cwd, model string, dynamicTools []DynamicTool) error {
	if err := c.Spawn(ctx); err != nil {
		return err
	}

	if err := c.connectWS(); err != nil {
		_ = c.Kill()
		return err
	}

	if err := c.Initialize(); err != nil {
		_ = c.Kill()
		return apperrors.Wrap(err, "AppServerClient.SpawnAndConnect", "initialize")
	}

	threadID, err := c.ThreadStart(cwd, model, dynamicTools)
	if err != nil {
		_ = c.Kill()
		return err
	}

	logger.Info("codex: app-server thread started",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		logger.FieldThreadID, threadID,
		"dynamic_tools", len(dynamicTools),
	)
	return nil
}

// Shutdown 优雅关闭。
func (c *AppServerClient) Shutdown() error {
	if c.stopped.Swap(true) {
		return nil
	}
	c.cancel()

	// 尝试发送 shutdown 通知 (best-effort)
	if err := c.notify("shutdown", nil); err != nil {
		logger.Debug("codex: shutdown notify failed (best-effort)",
			logger.FieldAgentID, c.AgentID, logger.FieldError, err)
	}

	// 主动关闭 ws 以立即中断 readLoop 的 ReadMessage 阻塞,
	// 避免等待 75s 的 read idle deadline。
	c.wsMu.Lock()
	if c.ws != nil {
		// 先发 WebSocket Close Frame, 给对端时间优雅关闭。
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown")
		_ = c.ws.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
		_ = c.ws.Close()
	}
	c.wsMu.Unlock()

	select {
	case <-c.wsDone:
	case <-time.After(3 * time.Second):
	}

	if err := c.Kill(); err != nil {
		return err
	}

	// stderrCollector.Close() 必须在 Kill() 之后:
	// Close() 等待 scan goroutine 退出, 而 scan 阻塞在 pipe read 上。
	// 只有子进程退出后, OS 关闭 pipe 的写端, scan 才能读到 EOF 并退出。
	if c.stderrCollector != nil {
		_ = c.stderrCollector.Close()
	}
	return nil
}

// Kill 强制终止子进程。
func (c *AppServerClient) Kill() error {
	if c.Cmd == nil || c.Cmd.Process == nil {
		return nil
	}
	// 尝试杀掉整个进程组 (Setpgid=true 时 pgid == pid)。
	// 回退: 如果进程组 kill 失败, 直接 kill 进程本身。
	pid := c.Cmd.Process.Pid
	killErr := syscall.Kill(-pid, syscall.SIGKILL)
	if killErr != nil {
		killErr = c.Cmd.Process.Kill()
	}
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return killErr
	}
	// Cmd.Wait 可能因 pipe-copying goroutine 未退出而阻塞, 加超时保护。
	waitDone := make(chan error, 1)
	go func() { waitDone <- c.Cmd.Wait() }()
	select {
	case waitErr := <-waitDone:
		if waitErr == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return nil
		}
		waitMsg := waitErr.Error()
		if strings.Contains(waitMsg, "Wait was already called") || strings.Contains(waitMsg, "no child processes") {
			return nil
		}
		return waitErr
	case <-time.After(5 * time.Second):
		logger.Warn("codex: Kill() Cmd.Wait timed out after 5s, abandoning",
			logger.FieldAgentID, c.AgentID,
			"pid", c.Cmd.Process.Pid,
		)
		return nil
	}
}

// Running 返回是否在运行。
func (c *AppServerClient) Running() bool {
	return !c.stopped.Load() && c.Cmd != nil && c.Cmd.ProcessState == nil
}

// truncStr 截断字符串用于日志输出。
func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

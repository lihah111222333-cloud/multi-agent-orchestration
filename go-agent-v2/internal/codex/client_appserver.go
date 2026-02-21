// client_appserver.go — JSON-RPC client 结构体定义 & 配置常量。
package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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

const (
	appServerStartupProbeTimeout     = 30 * time.Second
	appServerWriteTimeout            = 10 * time.Second
	appServerPingInterval            = 25 * time.Second
	appServerInterruptTimeout        = 30 * time.Second
	appServerListenerEnsureTimeout   = 10 * time.Second
	appServerReconnectBaseDelay      = 300 * time.Millisecond
	appServerReconnectMaxDelay       = 3 * time.Second
	defaultAppServerReadIdleTimeout  = 600 * time.Second
	defaultAppServerStreamMaxRetries = 5
	maxAppServerStreamMaxRetries     = 100
)

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

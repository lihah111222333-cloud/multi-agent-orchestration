package codex

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type jsonRPCRequest struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      jsonRPCID `json:"id"`
	Method  string    `json:"method"`
	Params  any       `json:"params,omitempty"`
}

type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *jsonRPCID      `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      jsonRPCID     `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

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

type AppServerClient struct {
	Port                                         int
	Cmd                                          *exec.Cmd
	ThreadID                                     string
	AgentID                                      string
	ApprovalPolicy                               string
	ws                                           *websocket.Conn
	wsMu                                         sync.Mutex
	wsDone                                       chan struct{}
	handler                                      EventHandler
	handlerMu                                    sync.RWMutex
	stopped                                      atomic.Bool
	ctx                                          context.Context
	cancel                                       context.CancelFunc
	stderrCollector                              *logger.StderrCollector
	nextID                                       atomic.Int64
	pending                                      sync.Map
	activeTurnID                                 atomic.Value
	listenerEnsureNeeded, listenerEnsureInFlight atomic.Bool
	legacyMirrorDropCount                        atomic.Int64
	healthMu                                     sync.Mutex
	health                                       appServerConnectionHealth
	respawnRecoverInFlight, readLoopRunning      atomic.Bool
	streamErrorRecoveryMu                        sync.Mutex
	streamErrorRecoveryTimer                     *time.Timer
}

const (
	appServerStartupProbeTimeout        = 30 * time.Second
	appServerWriteTimeout               = 10 * time.Second
	appServerPingInterval               = 25 * time.Second
	appServerInterruptTimeout           = 30 * time.Second
	appServerListenerEnsureTimeout      = 10 * time.Second
	appServerReconnectBaseDelay         = 300 * time.Millisecond
	appServerReconnectMaxDelay          = 3 * time.Second
	defaultAppServerReadIdleTimeout     = 600 * time.Second
	defaultAppServerStreamMaxRetries    = 5
	streamErrorRecoveryTimeout          = 60 * time.Second
	maxAppServerStreamMaxRetries        = 100
	appServerCircuitBreakerWindow       = 30 * time.Second
	appServerCircuitBreakerCooldown     = 8 * time.Second
	appServerCircuitBreakerThreshold    = 4
	appServerRespawnEscalationWindow    = 20 * time.Second
	appServerRespawnEscalationThreshold = 3
)

var (
	appServerReadIdleTimeoutMs atomic.Int64
	appServerStreamMaxRetries  = appServerStreamMaxRetriesFromEnv()
)

func init() { setAppServerReadIdleTimeout(appServerReadIdleTimeoutFromEnv()) }

func appServerReadIdleTimeoutFromEnv() time.Duration {
	raw, ms, err := parseEnvInt("GO_AGENT_APP_SERVER_STREAM_IDLE_TIMEOUT_MS")
	if raw == "" {
		return defaultAppServerReadIdleTimeout
	}
	if err != nil || ms <= 0 {
		logger.Warn("codex: invalid GO_AGENT_APP_SERVER_STREAM_IDLE_TIMEOUT_MS, using default",
			"value", raw,
			"default_ms", defaultAppServerReadIdleTimeout.Milliseconds(),
		)
		return defaultAppServerReadIdleTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func currentAppServerReadIdleTimeout() time.Duration {
	ms := appServerReadIdleTimeoutMs.Load()
	if ms <= 0 {
		return defaultAppServerReadIdleTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func setAppServerReadIdleTimeout(timeout time.Duration) {
	if timeout > 0 {
		appServerReadIdleTimeoutMs.Store(timeout.Milliseconds())
	}
}

func SetAppServerReadIdleTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	setAppServerReadIdleTimeout(timeout)
	logger.Info("codex: stream read idle timeout updated",
		"timeout_ms", timeout.Milliseconds(),
	)
}

func GetAppServerReadIdleTimeout() time.Duration { return currentAppServerReadIdleTimeout() }

func appServerStreamMaxRetriesFromEnv() int {
	raw, value, err := parseEnvInt("GO_AGENT_APP_SERVER_STREAM_MAX_RETRIES")
	if raw == "" {
		return defaultAppServerStreamMaxRetries
	}
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

func (c *AppServerClient) GetPort() int { return c.Port }

func (c *AppServerClient) GetThreadID() string { return c.ThreadID }

func (c *AppServerClient) SetApprovalPolicy(policy string) { c.ApprovalPolicy = policy }

func (c *AppServerClient) GetActiveTurnID() string { return c.getActiveTurnID() }

func (c *AppServerClient) SetEventHandler(h EventHandler) {
	c.handlerMu.Lock()
	c.handler = h
	c.handlerMu.Unlock()
}

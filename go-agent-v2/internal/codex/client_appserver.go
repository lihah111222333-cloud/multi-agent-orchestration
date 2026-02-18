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
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

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
}

const appServerStartupProbeTimeout = 30 * time.Second

// NewAppServerClient 创建 app-server 客户端。
func NewAppServerClient(port int) *AppServerClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &AppServerClient{
		Port:   port,
		ctx:    ctx,
		cancel: cancel,
		wsDone: make(chan struct{}),
	}
}

// GetPort 返回端口号。
func (c *AppServerClient) GetPort() int { return c.Port }

// GetThreadID 返回当前 thread ID。
func (c *AppServerClient) GetThreadID() string { return c.ThreadID }

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
			return fmt.Errorf("codex: port %d occupied: %w", c.Port, err)
		}
	}

	listenURL := fmt.Sprintf("ws://127.0.0.1:%d", c.Port)
	// 注意: 使用 exec.Command 而非 exec.CommandContext —
	// 子进程不应随 HTTP 请求或 WebSocket 连接断开而被终止。
	// 生命周期由 AppServerClient.Shutdown()/Kill() 显式管理。
	c.Cmd = exec.Command("codex", "app-server", "--listen", listenURL)
	c.Cmd.Env = os.Environ()
	c.Cmd.Stdout = io.Discard
	c.stderrCollector = logger.NewStderrCollector(fmt.Sprintf("codex-appserver-%d", c.Port))
	c.Cmd.Stderr = c.stderrCollector

	if err := c.Cmd.Start(); err != nil {
		return fmt.Errorf("codex: spawn app-server failed: %w", err)
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
			return fmt.Errorf("codex: spawn cancelled: %w", ctx.Err())
		default:
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", c.Port), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			logger.Info("codex: app-server listening", logger.FieldPort, c.Port)
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	_ = c.Kill()
	return fmt.Errorf("codex: app-server startup timeout on port %d", c.Port)
}

// connectWS 连接 WebSocket 并启动 readLoop。
func (c *AppServerClient) connectWS() error {
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d", c.Port)
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		NetDialContext:   (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	}

	conn, _, err := dialer.DialContext(c.ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("codex: app-server ws connect: %w", err)
	}
	c.ws = conn
	util.SafeGo(func() { c.readLoop() })
	return nil
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

	select {
	case <-pc.done:
		return pc.result, pc.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("codex: %s timeout", method)
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
		logger.Error("codex: Initialize() FAILED", logger.FieldPort, c.Port, logger.FieldError, err)
		return err
	}
	logger.Info("codex: Initialize() OK",
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
		logger.Error("codex: thread/start FAILED", logger.FieldPort, c.Port, logger.FieldError, err)
		return "", fmt.Errorf("codex: thread/start: %w", err)
	}

	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		logger.Error("codex: thread/start decode FAILED", logger.FieldPort, c.Port, logger.FieldRaw, string(result), logger.FieldError, err)
		return "", fmt.Errorf("codex: thread/start decode: %w (raw: %s)", err, result)
	}
	if resp.Thread.ID == "" {
		logger.Error("codex: thread/start returned empty thread ID", logger.FieldPort, c.Port, logger.FieldRaw, string(result))
		return "", fmt.Errorf("codex: thread/start returned empty thread ID (raw: %s)", result)
	}
	c.ThreadID = resp.Thread.ID
	logger.Info("codex: thread/start OK",
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
}

// Submit 发送用户 prompt (app-server JSON-RPC turn/start)。
func (c *AppServerClient) Submit(prompt string, images, files []string, outputSchema json.RawMessage) error {
	inputs := []asTurnStartInput{{Type: "text", Text: prompt}}

	params := map[string]any{
		"threadId": c.ThreadID,
		"input":    inputs,
	}
	if len(outputSchema) > 0 {
		params["outputSchema"] = json.RawMessage(outputSchema)
	}

	_, err := c.call("turn/start", params, 10*time.Second)
	return err
}

// SendCommand 发送斜杠命令 (通知, 无需响应)。
func (c *AppServerClient) SendCommand(cmd, args string) error {
	return c.notify("command", map[string]any{
		"threadId": c.ThreadID,
		"command":  cmd,
		"args":     args,
	})
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
		logger.FieldCallID, callID)
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

// ResumeThread 恢复会话 (app-server 模式暂不支持)。
func (c *AppServerClient) ResumeThread(_ ResumeThreadRequest) error {
	return fmt.Errorf("codex: resume not supported in app-server mode")
}

// ForkThread 分叉会话 (app-server 模式暂不支持)。
func (c *AppServerClient) ForkThread(_ ForkThreadRequest) (*ForkThreadResponse, error) {
	return nil, fmt.Errorf("codex: fork not supported in app-server mode")
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

		select {
		case <-c.wsDone:
		default:
			close(c.wsDone)
		}
	}()

	for !c.stopped.Load() {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			if !c.stopped.Load() {
				c.handlerMu.RLock()
				h := c.handler
				c.handlerMu.RUnlock()
				if h != nil {
					errData, _ := json.Marshal(ErrorData{Message: err.Error()})
					h(Event{Type: EventError, Data: errData})
				}
			}
			return
		}

		var msg jsonRPCMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Warn("codex: readLoop unparseable JSON-RPC message",
				logger.FieldError, err,
				"raw_len", len(message),
				"raw_prefix", truncateBytes(message, 200),
			)
			continue
		}

		// Response: 交给 pending call
		if msg.ID != nil && msg.Method == "" {
			if val, ok := c.pending.Load(*msg.ID); ok {
				pc := val.(*pendingCall)
				if msg.Error != nil {
					pc.err = fmt.Errorf("codex rpc: %s (code %d)", msg.Error.Message, msg.Error.Code)
					logger.Warn("codex: RPC error response",
						logger.FieldID, *msg.ID,
						"code", msg.Error.Code,
						"message", msg.Error.Message,
					)
				} else {
					pc.result = msg.Result
				}
				close(pc.done)
			} else {
				logger.Warn("codex: orphan RPC response (no pending call)",
					logger.FieldID, *msg.ID,
					"result_len", len(msg.Result),
				)
			}
			continue
		}

		// Server Request 或 Notification: 转为 Event, 交给 handler
		event := c.jsonRPCToEvent(msg)
		if event.Type == "" {
			logger.Warn("codex: readLoop skipped message with empty event type",
				logger.FieldMethod, msg.Method,
				"has_id", msg.ID != nil,
				logger.FieldParamsLen, len(msg.Params),
			)
			continue
		}

		// 如果是 server request, 携带 requestID 让 handler 回复
		if msg.ID != nil && msg.Method != "" {
			event.RequestID = msg.ID
			logger.Debug("codex: server request received",
				logger.FieldID, *msg.ID,
				logger.FieldMethod, msg.Method,
				logger.FieldEventType, event.Type,
			)
		}

		c.handlerMu.RLock()
		h := c.handler
		c.handlerMu.RUnlock()
		if h == nil {
			logger.Warn("codex: readLoop dropping event (no handler registered)",
				logger.FieldEventType, event.Type,
				logger.FieldMethod, msg.Method,
			)
			continue
		}
		h(event)

		if event.Type == EventError || event.Type == EventShutdownComplete {
			return
		}
	}
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
	"agent/event/session_configured":   EventSessionConfigured,
	"agent/event/mcp_startup_complete": EventMCPStartupComplete,
	"agent/event/mcp_startup_update":   "agent/event/mcp_startup_update",
	"agent/event/shutdown_complete":    EventShutdownComplete,
	"agent/event/error":                EventError,
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
	// 注意: `codex/event/task_complete` 与 `turn/completed` 语义重复。
	// 这里故意不折叠为 EventTurnComplete，保持 raw method，避免重复 turn/completed。
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
			logger.FieldMethod, msg.Method,
			logger.FieldParamsLen, len(msg.Params),
		)
	}

	return Event{Type: eventType, Data: msg.Params}
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

// asWriteJSON 线程安全写入 WebSocket JSON。
func (c *AppServerClient) asWriteJSON(v any) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.ws == nil {
		return fmt.Errorf("codex: ws not connected")
	}
	return c.ws.WriteJSON(v)
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
		return fmt.Errorf("codex: initialize: %w", err)
	}

	threadID, err := c.ThreadStart(cwd, model, dynamicTools)
	if err != nil {
		_ = c.Kill()
		return err
	}

	logger.Info("codex: app-server thread started",
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
	if c.stderrCollector != nil {
		_ = c.stderrCollector.Close()
	}
	c.cancel()

	// 尝试发送 shutdown
	_ = c.notify("shutdown", nil)

	select {
	case <-c.wsDone:
	case <-time.After(3 * time.Second):
	}

	return c.Kill()
}

// Kill 强制终止子进程。
func (c *AppServerClient) Kill() error {
	if c.Cmd != nil && c.Cmd.Process != nil {
		return c.Cmd.Process.Kill()
	}
	return nil
}

// Running 返回是否在运行。
func (c *AppServerClient) Running() bool {
	return !c.stopped.Load() && c.Cmd != nil && c.Cmd.ProcessState == nil
}

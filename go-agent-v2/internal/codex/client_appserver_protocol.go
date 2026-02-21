// client_appserver_protocol.go — 应用层协议方法: Initialize, ThreadStart, Submit, SendCommand 等。
package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

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
	Instructions string        `json:"instructions,omitempty"`
	DynamicTools []DynamicTool `json:"dynamicTools,omitempty"` // camelCase as required by app-server
}

// ThreadStart 创建 thread (app-server JSON-RPC)。
func (c *AppServerClient) ThreadStart(cwd, model, instructions string, dynamicTools []DynamicTool) (string, error) {
	toolNames := make([]string, len(dynamicTools))
	for i, t := range dynamicTools {
		toolNames[i] = t.Name
	}
	logger.Info("codex: thread/start",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		logger.FieldCwd, cwd,
		"model", model,
		"has_instructions", instructions != "",
		"dynamic_tools_count", len(dynamicTools),
		"dynamic_tools", toolNames,
	)

	result, err := c.call("thread/start", asThreadStartParams{
		Cwd:          cwd,
		Model:        model,
		Instructions: instructions,
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

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
	_ = result
	return nil
}

type asThreadStartParams struct {
	Cwd          string        `json:"cwd,omitempty"`
	Model        string        `json:"model,omitempty"`
	Instructions string        `json:"instructions,omitempty"`
	DynamicTools []DynamicTool `json:"dynamicTools,omitempty"` // camelCase as required by app-server
}

func (c *AppServerClient) ThreadStart(cwd, model, instructions string, dynamicTools []DynamicTool) (string, error) {
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
	return c.ThreadID, nil
}

type asTurnStartInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
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
		if util.IsRemoteImageURL(path) {
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

func isNotInitializedRPCError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not initialized")
}

func ensureListenerWithAutoInitialize(
	threadID string,
	rpcCall func(method string, params any, timeout time.Duration) (json.RawMessage, error),
	initializeFn func() error,
) (resolvedID string, retriedAfterInit bool, err error) {
	resolvedID, err = ensureListenerViaThreadResume(threadID, rpcCall)
	if err == nil {
		return resolvedID, false, nil
	}
	if !isNotInitializedRPCError(err) || initializeFn == nil {
		return "", false, err
	}
	if initErr := initializeFn(); initErr != nil {
		return "", true, apperrors.Wrap(initErr, "ensureListenerWithAutoInitialize", "initialize")
	}
	resolvedID, err = ensureListenerViaThreadResume(threadID, rpcCall)
	if err != nil {
		return "", true, err
	}
	return resolvedID, true, nil
}

func callWithNotInitializedRecovery(
	rpcCall func(method string, params any, timeout time.Duration) (json.RawMessage, error),
	initializeFn func() error,
	method string,
	params any,
	timeout time.Duration,
) (json.RawMessage, bool, error) {
	if rpcCall == nil {
		return nil, false, apperrors.New("callWithNotInitializedRecovery", "rpc call func is nil")
	}
	result, err := rpcCall(method, params, timeout)
	if err == nil || !isNotInitializedRPCError(err) {
		return result, false, err
	}
	if initializeFn == nil {
		return nil, false, err
	}
	if initErr := initializeFn(); initErr != nil {
		return nil, false, apperrors.Wrap(initErr, "callWithNotInitializedRecovery", "initialize")
	}
	retryResult, retryErr := rpcCall(method, params, timeout)
	if retryErr != nil {
		return nil, false, retryErr
	}
	return retryResult, true, nil
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
	resolvedID, _, err := ensureListenerWithAutoInitialize(threadID, callFn, c.Initialize)
	if err != nil {
		if isMethodNotFoundRPCError(err) || isInvalidParamsRPCError(err) {
			c.listenerEnsureNeeded.Store(false)
			return
		}
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

func (c *AppServerClient) Submit(prompt string, images, files []string, outputSchema json.RawMessage) error {
	c.ensureListenerIfNeeded("turn/start", c.call)

	inputs := buildTurnStartInputs(prompt, images, files)
	threadID := strings.TrimSpace(c.ThreadID)

	params := map[string]any{
		"threadId": threadID,
		"input":    inputs,
	}
	if len(outputSchema) > 0 {
		params["outputSchema"] = json.RawMessage(outputSchema)
	}

	result, recovered, err := callWithNotInitializedRecovery(c.call, c.Initialize, "turn/start", params, 10*time.Second)
	if err != nil {
		return err
	}
	_ = recovered
	if turnID := extractTurnIDFromEventData(result); turnID != "" {
		c.setActiveTurnID(turnID)
	}
	return nil
}

func (c *AppServerClient) SendCommand(cmd, args string) error {
	if strings.TrimSpace(cmd) == CmdInterrupt {
		handled, err := c.tryInterruptCommand()
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
	}
	return c.sendCommandNotification(cmd, args)
}

func (c *AppServerClient) tryInterruptCommand() (bool, error) {
	threadID := strings.TrimSpace(c.ThreadID)
	if threadID == "" {
		return false, apperrors.New("AppServerClient.SendCommand", "interrupt requires active thread id")
	}
	turnID := strings.TrimSpace(c.getActiveTurnID())

	if turnID != "" {
		err := c.callTurnInterrupt(threadID, turnID, "with_turn_id")
		if err == nil {
			return true, nil
		}
		if isInterruptTurnIDMismatchError(err) && c.callTurnInterrupt(threadID, turnID, "thread_scoped") == nil {
			return true, nil
		}
	} else {
		if c.callTurnInterrupt(threadID, "", "thread_scoped") == nil {
			return true, nil
		}
	}

	if c.callInterruptConversation(threadID) == nil {
		return true, nil
	}

	return false, nil
}

func (c *AppServerClient) callTurnInterrupt(threadID, turnID, turnScope string) error {
	_, _, err := callWithNotInitializedRecovery(
		c.call,
		c.Initialize,
		"turn/interrupt",
		buildTurnInterruptParams(threadID, turnID, turnScope),
		appServerInterruptTimeout,
	)
	return err
}

func (c *AppServerClient) callInterruptConversation(threadID string) error {
	_, _, err := callWithNotInitializedRecovery(
		c.call,
		c.Initialize,
		"interruptConversation",
		map[string]any{"conversationId": threadID},
		appServerInterruptTimeout,
	)
	return err
}

func (c *AppServerClient) sendCommandNotification(cmd, args string) error {
	if err := c.notify("command", map[string]any{"threadId": c.ThreadID, "command": cmd, "args": args}); err != nil {
		return err
	}
	return nil
}

func rpcErrorContains(err error, parts ...string) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, part := range parts {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(part))) {
			return true
		}
	}
	return false
}

func rpcErrorContainsAll(err error, parts ...string) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, part := range parts {
		if !strings.Contains(text, strings.ToLower(strings.TrimSpace(part))) {
			return false
		}
	}
	return true
}

func isMethodNotFoundRPCError(err error) bool {
	return rpcErrorContains(err, "method not found", "code -32601")
}

func isInvalidParamsRPCError(err error) bool {
	return rpcErrorContains(err, "invalid params", "code -32602")
}

func isRPCTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, " timeout") || strings.HasSuffix(text, "timeout")
}

func buildTurnInterruptParams(threadID, turnID, turnScope string) map[string]any {
	interruptTurnID := strings.TrimSpace(turnID)
	if strings.EqualFold(strings.TrimSpace(turnScope), "thread_scoped") {
		interruptTurnID = ""
	}
	return map[string]any{
		"threadId": strings.TrimSpace(threadID),
		"turnId":   interruptTurnID,
	}
}

func isInterruptTurnIDMismatchError(err error) bool {
	return rpcErrorContains(err, "turn not found", "unknown turn", "invalid turn") ||
		rpcErrorContainsAll(err, "turn id", "mismatch") ||
		rpcErrorContainsAll(err, "turn_id", "mismatch")
}

func (c *AppServerClient) SendDynamicToolResult(callID, output string, requestID *int64) error {
	result := DynamicToolCallResponse{
		ContentItems: []DynamicToolContentItem{{
			Type: "inputText",
			Text: output,
		}},
		Success: true,
	}

	if requestID != nil {
		return c.respond(*requestID, result)
	}

	logger.Warn("codex: SendDynamicToolResult without requestID, falling back to notification",
		logger.FieldAgentID, c.AgentID, logger.FieldCallID, callID)
	params := map[string]any{
		"threadId":     c.ThreadID,
		"callId":       callID,
		"toolCallId":   callID,
		"tool_call_id": callID,
		"output":       output,
		"result":       result,
		"contentItems": result.ContentItems,
		"success":      true,
	}
	return c.notify("dynamic_tool_result", params)
}

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

func (c *AppServerClient) ResumeThread(req ResumeThreadRequest) error {
	id := strings.TrimSpace(req.ThreadID)
	if id == "" {
		return apperrors.New("AppServerClient.ResumeThread", "thread/resume requires thread ID")
	}
	result, err := c.call("thread/resume", asThreadResumeParams{
		ThreadID: id,
		Path:     strings.TrimSpace(req.Path),
		Cwd:      strings.TrimSpace(req.Cwd),
	}, 30*time.Second)
	if err != nil {
		return apperrors.Wrap(err, "AppServerClient.ResumeThread", "thread/resume")
	}
	resolvedID, err := parseThreadResumeResult(result, id)
	if err != nil {
		return err
	}
	c.ThreadID = resolvedID
	c.listenerEnsureNeeded.Store(false)
	return nil
}

func (c *AppServerClient) ForkThread(_ ForkThreadRequest) (*ForkThreadResponse, error) {
	return nil, apperrors.New("AppServerClient.ForkThread", "fork not supported in app-server mode")
}

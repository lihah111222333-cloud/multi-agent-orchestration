package codex

import (
	"encoding/json"
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
	Cwd            string        `json:"cwd,omitempty"`
	Model          string        `json:"model,omitempty"`
	Instructions   string        `json:"instructions,omitempty"`
	DynamicTools   []DynamicTool `json:"dynamicTools,omitempty"` // camelCase as required by app-server
	ApprovalPolicy string        `json:"approvalPolicy,omitempty"`
}

func (c *AppServerClient) ThreadStart(cwd, model, instructions string, dynamicTools []DynamicTool) (string, error) {
	policy := strings.TrimSpace(c.ApprovalPolicy)
	logger.Info("codex: thread/start with approval policy",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		"approval_policy", policy,
	)
	result, err := c.call("thread/start", asThreadStartParams{
		Cwd:            cwd,
		Model:          model,
		Instructions:   instructions,
		DynamicTools:   dynamicTools,
		ApprovalPolicy: policy,
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

func (c *AppServerClient) ensureListenerIfNeeded(
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
	rpcCall func(method string, params any, timeout time.Duration) (json.RawMessage, error),
) {
	util.SafeGo(func() {
		c.ensureListenerIfNeeded(rpcCall)
	})
}

func (c *AppServerClient) Submit(prompt string, images, files []string, outputSchema json.RawMessage) error {
	c.ensureListenerIfNeeded(c.call)

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
	return c.notify("command", map[string]any{"threadId": c.ThreadID, "command": cmd, "args": args})
}

func (c *AppServerClient) tryInterruptCommand() (bool, error) {
	threadID := strings.TrimSpace(c.ThreadID)
	if threadID == "" {
		return false, apperrors.New("AppServerClient.SendCommand", "interrupt requires active thread id")
	}
	turnID := strings.TrimSpace(c.getActiveTurnID())
	callTurnInterrupt := func(turnID, turnScope string) error {
		return c.callWithInitializeRecovery(
			"turn/interrupt",
			buildTurnInterruptParams(threadID, turnID, turnScope),
			appServerInterruptTimeout,
		)
	}

	if turnID != "" {
		err := callTurnInterrupt(turnID, "with_turn_id")
		if err == nil {
			return true, nil
		}
		if isInterruptTurnIDMismatchError(err) && callTurnInterrupt(turnID, "thread_scoped") == nil {
			return true, nil
		}
	} else {
		if callTurnInterrupt("", "thread_scoped") == nil {
			return true, nil
		}
	}

	if c.callWithInitializeRecovery(
		"interruptConversation",
		map[string]any{"conversationId": threadID},
		appServerInterruptTimeout,
	) == nil {
		return true, nil
	}

	return false, nil
}

func (c *AppServerClient) callWithInitializeRecovery(method string, params any, timeout time.Duration) error {
	_, _, err := callWithNotInitializedRecovery(
		c.call,
		c.Initialize,
		method,
		params,
		timeout,
	)
	return err
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

func (c *AppServerClient) ResumeThread(req ResumeThreadRequest) error {
	id := strings.TrimSpace(req.ThreadID)
	if id == "" {
		return apperrors.New("AppServerClient.ResumeThread", "thread/resume requires thread ID")
	}
	resolvedID, err := callThreadResume("AppServerClient.ResumeThread", c.call, asThreadResumeParams{
		ThreadID: id,
		Path:     strings.TrimSpace(req.Path),
		Cwd:      strings.TrimSpace(req.Cwd),
	}, 30*time.Second)
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

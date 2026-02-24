package tooladapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// DynamicToolCall carries dispatch input and runtime context.
type DynamicToolCall struct {
	AgentID    string
	Tool       string
	CallID     string
	RequestID  *int64
	Arguments  json.RawMessage
	Ctx        context.Context
	TotalCalls *int64
}

// Dispatch resolves runtime handler and executes it with assembled ToolCallContext.
func Dispatch(call DynamicToolCall, deps Providers) (string, error) {
	toolName := strings.TrimSpace(call.Tool)
	if toolName == "" {
		return "", fmt.Errorf("tool name is required")
	}

	var count int64
	if deps.Counter != nil {
		count = deps.Counter.IncrementToolCall(toolName)
	}
	if call.TotalCalls != nil {
		*call.TotalCalls = count
	}

	logger.Info("dynamic-tool: called",
		logger.FieldAgentID, call.AgentID,
		logger.FieldToolName, toolName,
		"call_id", call.CallID,
		"total_calls", count,
	)

	if deps.Lookup == nil {
		return "", fmt.Errorf("runtime tool lookup is not configured")
	}
	handler, ok := deps.Lookup.LookupRuntimeTool(toolName)
	if !ok || handler == nil {
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}

	callCtx := BuildToolCallContext(call)
	if isCodeRunTool(toolName) && deps.CodeRunTracker != nil {
		resolvedCallID := resolveCodeRunCallID(callCtx.CallID, callCtx.RequestID)
		execCtx, execCancel := context.WithCancel(callCtx.Ctx)
		runKey := deps.CodeRunTracker.RegisterCodeRunCancel(callCtx.AgentID, resolvedCallID, execCancel)
		defer func() {
			deps.CodeRunTracker.UnregisterCodeRunCancel(callCtx.AgentID, runKey)
			execCancel()
		}()

		call.CallID = resolvedCallID
		call.Ctx = execCtx
		callCtx = BuildToolCallContext(call)
	}

	return handler(callCtx, call.Arguments), nil
}

func isCodeRunTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "code_run", "code_run_test":
		return true
	default:
		return false
	}
}

func resolveCodeRunCallID(callID string, requestID *int64) string {
	trimmed := strings.TrimSpace(callID)
	if trimmed != "" {
		return trimmed
	}
	if requestID != nil {
		return fmt.Sprintf("req-%d", *requestID)
	}
	return ""
}

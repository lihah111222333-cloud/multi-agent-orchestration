package tooladapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type DynamicToolCall struct {
	AgentID    string
	Tool       string
	CallID     string
	RequestID  *int64
	Arguments  json.RawMessage
	Ctx        context.Context
	TotalCalls *int64
}

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
		return "", fmt.Errorf("UNKNOWN_TOOL: unknown tool: %s", toolName)
	}

	callCtx := BuildToolCallContext(call)
	if isCodeRunTool(toolName) && deps.CodeRunTracker != nil {
		callCtx.CallID = util.ResolveCodeRunCallID(callCtx.CallID, callCtx.RequestID)
		execCtx, execCancel := context.WithCancel(callCtx.Ctx)
		callCtx.Ctx = execCtx
		runKey := deps.CodeRunTracker.RegisterCodeRunCancel(callCtx.AgentID, callCtx.CallID, execCancel)
		defer func() {
			deps.CodeRunTracker.UnregisterCodeRunCancel(callCtx.AgentID, runKey)
			execCancel()
		}()
	}

	return handler(callCtx, call.Arguments), nil
}

func isCodeRunTool(name string) bool {
	name = strings.TrimSpace(name)
	return name == "code_run" || name == "code_run_test"
}

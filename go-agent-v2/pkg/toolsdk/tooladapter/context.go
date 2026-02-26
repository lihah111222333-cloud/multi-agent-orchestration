package tooladapter

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/tools"
)

// BuildToolCallContext converts a dynamic tool call envelope into tools.ToolCallContext.
func BuildToolCallContext(call DynamicToolCall) tools.ToolCallContext {
	callCtx := call.Ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	return tools.ToolCallContext{
		AgentID:   call.AgentID,
		CallID:    call.CallID,
		RequestID: call.RequestID,
		Ctx:       callCtx,
	}
}

package tooladapter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
)

const lspToolCallMetaKey = "__tool_call_meta"

func withLSPToolCallMeta(args json.RawMessage, callCtx tools.ToolCallContext) json.RawMessage {
	payload, ok := decodeJSONMap(args)
	if !ok {
		payload = make(map[string]any)
	}

	meta := make(map[string]any)
	if agentID := strings.TrimSpace(callCtx.AgentID); agentID != "" {
		meta["agent_id"] = agentID
	}
	if callID := strings.TrimSpace(callCtx.CallID); callID != "" {
		meta["call_id"] = callID
	}
	if callCtx.RequestID != nil {
		meta["request_id"] = *callCtx.RequestID
	}
	if threadID := extractThreadIDFromToolArgs(payload); threadID != "" {
		meta["thread_id"] = threadID
	}
	if len(meta) == 0 {
		return args
	}

	payload[lspToolCallMetaKey] = meta
	marshaled, err := json.Marshal(payload)
	if err != nil {
		return args
	}
	return marshaled
}

func decodeJSONMap(args json.RawMessage) (map[string]any, bool) {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(args, &payload); err != nil || payload == nil {
		return nil, false
	}
	return payload, true
}

func extractThreadIDFromToolArgs(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	for _, key := range []string{"thread_id", "threadId"} {
		if value := strings.TrimSpace(fmt.Sprint(payload[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	for _, key := range []string{"payload", "data", "msg", "context"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if value := extractThreadIDFromToolArgs(nested); value != "" {
			return value
		}
	}
	return ""
}

package apiserver

import "encoding/json"

var payloadExtractKeys = []string{
	// legacy fields
	"delta", "content", "message", "command",
	"cmd", "command_display", "commandDisplay", "displayCommand",
	"exit_code", "reason", "name", "status",
	"file", "files", "diff", "tool_name",
	"item", "process",
	"turn", "last_agent_message", "lastAgentMessage",
	// v2 protocol fields
	"text", "summary", "args", "arguments", "output",
	"id", "type", "item_id", "callId", "call_id",
	"file_path", "path", "chunk", "stream",
	"plan", "explanation",
	"phase", "recoverable", "willRetry", "will_retry",
	"additional_details", "additionalDetails",
	"threadId", "thread_id", "turnId", "turn_id",
	"activeTurnId", "active_turn_id", "attempt", "max_retries",
	"error",
	// token usage fields (keep nested shapes for runtime parser)
	"tokenUsage", "token_usage", "usage", "info",
	"total_tokens", "totalTokens", "used_tokens", "usedTokens",
	"input_tokens", "inputTokens", "output_tokens", "outputTokens",
	"context_window_tokens", "contextWindowTokens",
	"model_context_window", "modelContextWindow",
}

func parseMapAny(raw any) map[string]any {
	switch value := raw.(type) {
	case map[string]any:
		return value
	case string:
		var out map[string]any
		if json.Unmarshal([]byte(value), &out) == nil {
			return out
		}
	case json.RawMessage:
		var out map[string]any
		if json.Unmarshal(value, &out) == nil {
			return out
		}
	case []byte:
		var out map[string]any
		if json.Unmarshal(value, &out) == nil {
			return out
		}
	}
	return nil
}

func mergePayloadFromMap(payload map[string]any, data map[string]any) {
	if data == nil {
		return
	}

	for _, key := range payloadExtractKeys {
		v, ok := data[key]
		if !ok {
			continue
		}
		payload[key] = v
	}

	if v, ok := data["call_id"]; ok {
		if _, exists := payload["id"]; !exists {
			payload["id"] = v
		}
	}
	if v, ok := data["item_id"]; ok {
		if _, exists := payload["id"]; !exists {
			payload["id"] = v
		}
	}
	if v, ok := data["file_path"]; ok {
		if _, exists := payload["file"]; !exists {
			payload["file"] = v
		}
	}
	if v, ok := data["path"]; ok {
		if _, exists := payload["file"]; !exists {
			payload["file"] = v
		}
	}
	if errObj, ok := data["error"].(map[string]any); ok && errObj != nil {
		if _, exists := payload["message"]; !exists {
			if msg, ok := errObj["message"]; ok {
				payload["message"] = msg
			}
		}
		if _, exists := payload["additional_details"]; !exists {
			if details, ok := errObj["additional_details"]; ok {
				payload["additional_details"] = details
			} else if details, ok := errObj["additionalDetails"]; ok {
				payload["additional_details"] = details
			}
		}
	}
	if item := parseMapAny(data["item"]); item != nil {
		mergePayloadFromMap(payload, item)
	}
}

// walkNestedJSON 遍历 msg/data/payload 嵌套层, 对每个解析出的 map[string]any 调用 fn。
//
// 统一处理四种嵌套类型: map[string]any / string / json.RawMessage / []byte。
// mergePayloadFields 使用此逻辑。
func walkNestedJSON(m map[string]any, fn func(map[string]any)) {
	for _, key := range []string{"msg", "data", "payload"} {
		v, ok := m[key]
		if !ok {
			continue
		}
		switch nested := v.(type) {
		case map[string]any:
			fn(nested)
		case string:
			var nm map[string]any
			if json.Unmarshal([]byte(nested), &nm) == nil {
				fn(nm)
			}
		case json.RawMessage:
			var nm map[string]any
			if json.Unmarshal(nested, &nm) == nil {
				fn(nm)
			}
		case []byte:
			var nm map[string]any
			if json.Unmarshal(nested, &nm) == nil {
				fn(nm)
			}
		}
	}
}

func mergePayloadFields(payload map[string]any, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}

	var dataMap map[string]any
	if err := json.Unmarshal(raw, &dataMap); err != nil {
		return
	}

	mergePayloadFromMap(payload, dataMap)
	walkNestedJSON(dataMap, func(nested map[string]any) {
		mergePayloadFromMap(payload, nested)
	})
}

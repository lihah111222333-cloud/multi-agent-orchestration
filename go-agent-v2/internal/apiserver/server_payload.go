package apiserver

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// uiStateThrottleEntry 全局节流状态。
type uiStateThrottleEntry struct {
	lastEmit time.Time      // 上次实际发送时间
	timer    *time.Timer    // trailing timer (保证最终一致)
	pending  map[string]any // 最新 payload (合并)
}

// SetNotifyHook 设置 Notify 事件钩子。
//
// 用于桌面端桥接: apiserver 事件 -> Wails runtime event。
func SetNotifyHook(s *Server, h func(method string, params any)) {
	if s == nil {
		return
	}
	setNotifyHookState(s, h)
}

// Notify 向所有连接广播 JSON-RPC 通知 (WebSocket + SSE)。
func Notify(s *Server, method string, params any) {
	if s == nil {
		return
	}
	notify(s, method, params)
}

func notify(s *Server, method string, params any) {
	payload := util.ToMapAny(params)
	syncUIRuntimeFromNotifyPayload(s, method, payload)
	broadcastNotification(s, method, payload)

	if shouldEmitUIStateChanged(method, payload) {
		statePayload := map[string]any{"source": method}
		if tid, _ := payload["threadId"].(string); tid != "" {
			statePayload["threadId"] = tid
		}
		if aid, _ := payload["agent_id"].(string); aid != "" {
			statePayload["agent_id"] = aid
		}
		throttledUIStateChanged(s, statePayload)
	}
}

func shouldEmitUIStateChanged(method string, payload map[string]any) bool {
	if method == "" || method == "ui/state/changed" {
		return false
	}
	if strings.HasPrefix(method, "workspace/run/") {
		return true
	}
	threadID, _ := payload["threadId"].(string)
	if strings.TrimSpace(threadID) != "" {
		return true
	}
	agentID, _ := payload["agent_id"].(string)
	return strings.TrimSpace(agentID) != ""
}

// throttledUIStateChanged 全局节流发送 ui/state/changed。
//
// 使用全局统一节流 (不再 per-thread): 多 agent 并行时也只发一条,
// 前端只需要一个信号触发 syncRuntimeState 拉取最新快照。
func throttledUIStateChanged(s *Server, payload map[string]any) {
	if s == nil {
		return
	}
	key := "_global"
	now := time.Now()
	interval := time.Duration(uiStateThrottleMs) * time.Millisecond
	pending, emitNow := stageUIStateChangedState(s,
		key,
		payload,
		now,
		interval,
		func() { flushUIStateChanged(s, key) },
	)
	if !emitNow {
		return
	}
	broadcastNotification(s, "ui/state/changed", pending)
}

// flushUIStateChanged trailing timer 回调: 发送最后一个 pending payload。
func flushUIStateChanged(s *Server, key string) {
	if s == nil {
		return
	}
	pending, ok := flushUIStateChangedState(s, key, time.Now())
	if !ok {
		return
	}
	broadcastNotification(s, "ui/state/changed", pending)
}

func syncUIRuntimeFromNotifyPayload(s *Server, method string, payload map[string]any) {
	if s == nil {
		return
	}
	if s.uiRuntime == nil {
		return
	}
	switch method {
	case "workspace/run/created", "workspace/run/aborted":
		run := util.ToMapAny(payload["run"])
		if len(run) == 0 {
			return
		}
		s.uiRuntime.UpsertWorkspaceRun(run)
	case "workspace/run/merged":
		runKey, _ := payload["runKey"].(string)
		result := util.ToMapAny(payload["result"])
		if len(result) == 0 {
			return
		}
		s.uiRuntime.ApplyWorkspaceMergeResult(runKey, result)
	}
	if shouldReplayThreadNotifyToUIRuntime(method, payload) {
		threadID, _ := payload["threadId"].(string)
		normalized := uistate.NormalizeEventFromPayload(method, method, payload)
		s.uiRuntime.ApplyAgentEvent(strings.TrimSpace(threadID), normalized, payload)
	}
}

func shouldReplayThreadNotifyToUIRuntime(method string, payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if _, hasUIType := payload["uiType"]; hasUIType {
		return false
	}
	threadID, _ := payload["threadId"].(string)
	if strings.TrimSpace(threadID) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "turn/completed", "turn/aborted", "error", "codex/event/stream_error":
		return true
	default:
		return false
	}
}

var payloadExtractKeys = []string{

	"delta", "content", "message", "command",
	"cmd", "command_display", "commandDisplay", "displayCommand",
	"exit_code", "reason", "name", "status",
	"file", "files", "diff", "tool_name",
	"item", "process",
	"turn", "last_agent_message", "lastAgentMessage",

	"text", "summary", "args", "arguments", "output",
	"id", "type", "item_id", "callId", "call_id",
	"file_path", "path", "chunk", "stream",
	"plan", "explanation",
	"phase", "recoverable", "willRetry", "will_retry",
	"additional_details", "additionalDetails",
	"threadId", "thread_id", "turnId", "turn_id",
	"activeTurnId", "active_turn_id", "attempt", "max_retries",
	"error",

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

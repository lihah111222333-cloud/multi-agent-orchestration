package apiserver

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type uiStateThrottleEntry struct {
	lastEmit time.Time
	timer    *time.Timer
	pending  map[string]any
}

func SetNotifyHook(s *Server, h func(method string, params any)) {
	if s != nil {
		setNotifyHookState(s, h)
	}
}

func Notify(s *Server, method string, params any) {
	if s != nil {
		notify(s, method, params)
	}
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
	agentID, _ := payload["agent_id"].(string)
	return strings.TrimSpace(threadID) != "" || strings.TrimSpace(agentID) != ""
}

func throttledUIStateChanged(s *Server, payload map[string]any) {
	if s == nil {
		return
	}
	now := time.Now()
	interval := time.Duration(uiStateThrottleMs) * time.Millisecond
	pending, emitNow := stageUIStateChangedState(s,
		"_global",
		payload,
		now,
		interval,
		func() { flushUIStateChanged(s, "_global") },
	)
	if !emitNow {
		return
	}
	broadcastNotification(s, "ui/state/changed", pending)
}

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
	if s == nil || s.uiRuntime == nil {
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
	if value, ok := raw.(map[string]any); ok {
		return value
	}
	var data []byte
	switch value := raw.(type) {
	case string:
		data = []byte(value)
	case json.RawMessage:
		data = value
	case []byte:
		data = value
	default:
		return nil
	}
	var out map[string]any
	if json.Unmarshal(data, &out) == nil {
		return out
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

	for _, alias := range [][2]string{
		{"call_id", "id"},
		{"item_id", "id"},
		{"file_path", "file"},
		{"path", "file"},
	} {
		if _, exists := payload[alias[1]]; exists {
			continue
		}
		if value, ok := data[alias[0]]; ok {
			payload[alias[1]] = value
		}
	}
	if errObj, ok := data["error"].(map[string]any); ok && errObj != nil {
		if _, exists := payload["message"]; !exists {
			if msg, ok := errObj["message"]; ok {
				payload["message"] = msg
			}
		}
		if _, exists := payload["additional_details"]; !exists {
			for _, key := range []string{"additional_details", "additionalDetails"} {
				if details, ok := errObj[key]; ok {
					payload["additional_details"] = details
					break
				}
			}
		}
	}
	if item := parseMapAny(data["item"]); item != nil {
		mergePayloadFromMap(payload, item)
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
	for _, key := range []string{"msg", "data", "payload"} {
		if nested := parseMapAny(dataMap[key]); nested != nil {
			mergePayloadFromMap(payload, nested)
		}
	}
}

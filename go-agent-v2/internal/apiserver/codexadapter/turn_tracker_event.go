package codexadapter

import (
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// ThreadStatusTerminalFromPayload parses thread/status/changed payload terminal status.
func ThreadStatusTerminalFromPayload(payload map[string]any) (status string, reason string, terminal bool) {
	if payload == nil {
		return "", "", false
	}

	statusType := ""
	switch raw := payload["status"].(type) {
	case string:
		statusType = strings.ToLower(strings.TrimSpace(raw))
	case map[string]any:
		statusType = strings.ToLower(strings.TrimSpace(ExtractTrackedString(raw, "type")))
	}

	if statusType == "" {
		return "", "", false
	}

	switch statusType {
	case "idle":
		return "completed", "thread_status_idle", true
	case "systemerror", "system_error", "error":
		return "failed", "thread_status_system_error", true
	case "notloaded", "not_loaded":
		return "failed", "thread_status_not_loaded", true
	default:
		return "", "", false
	}
}

// TrackedTurnTerminalFromEvent maps incoming event to tracked turn terminal state.
func TrackedTurnTerminalFromEvent(eventType, method string, payload map[string]any) (string, string, string, bool, bool) {
	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))

	switch {
	case eventKey == "turn_aborted",
		methodKey == "turn/aborted":
		reason := ExtractTrackedTurnReason(payload)
		if reason == "" {
			reason = "turn_aborted"
		}
		return ExtractTrackedTurnID(payload), "interrupted", reason, true, false
	case methodKey == "turn/completed",
		eventKey == "turn_complete",
		eventKey == "turn/completed",
		eventKey == "idle",
		eventKey == "codex/event/task_complete",
		methodKey == "codex/event/task_complete":
		status := ExtractTrackedTurnStatus(payload)
		if status == "" {
			status = "completed"
		}
		reason := ExtractTrackedTurnReason(payload)
		if reason == "" {
			reason = "turn_complete"
		}
		return ExtractTrackedTurnID(payload), status, reason, true, false
	case eventKey == "stream_error",
		eventKey == "error",
		methodKey == "error",
		methodKey == "codex/event/stream_error":
		retryable, known := ExtractTrackedRetryable(payload)
		if known && retryable {
			return "", "", "", false, false
		}
		// willRetry 缺失 (known=false) -> 不视为 terminal, codex 会自行处理。
		// 只有明确 willRetry=false 时才终止 turn。
		if !known {
			return "", "", "", false, false
		}
		reason := ExtractTrackedTurnReason(payload)
		if reason == "" {
			reason = util.FirstNonEmpty(
				ExtractTrackedString(payload, "phase"),
				eventKey,
				methodKey,
				"stream_error",
			)
		}
		return ExtractTrackedTurnID(payload), "failed", reason, true, true
	case methodKey == "thread/status/changed",
		eventKey == "thread/status/changed":
		status, reason, ok := ThreadStatusTerminalFromPayload(payload)
		if !ok {
			return "", "", "", false, false
		}
		return ExtractTrackedTurnID(payload), status, reason, true, true
	default:
		return "", "", "", false, false
	}
}

// ExtractTrackedRetryable reads retryability hint from event payload.
func ExtractTrackedRetryable(payload map[string]any) (bool, bool) {
	if payload == nil {
		return false, false
	}
	for _, key := range []string{"willRetry", "will_retry", "recoverable"} {
		value, exists := payload[key]
		if !exists {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "y":
				return true, true
			case "false", "0", "no", "n":
				return false, true
			}
		}
	}
	return false, false
}

// ExtractTrackedTurnID reads turn id from payload.
func ExtractTrackedTurnID(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if id := ExtractTrackedString(turn, "id", "turnId", "turn_id"); id != "" {
			return id
		}
	}
	return ExtractTrackedString(payload, "turnId", "turn_id", "id")
}

// ExtractTrackedTurnStatus reads turn status from payload.
func ExtractTrackedTurnStatus(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if status := ExtractTrackedString(turn, "status", "state"); status != "" {
			return status
		}
	}
	return ExtractTrackedString(payload, "status", "state")
}

// ExtractTrackedTurnReason reads turn reason from payload.
func ExtractTrackedTurnReason(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		if reason := ExtractTrackedString(turn, "reason", "message"); reason != "" {
			return reason
		}
	}
	return ExtractTrackedString(payload, "reason", "message")
}

// ExtractTrackedString returns first non-empty trimmed string value by keys.
func ExtractTrackedString(payload map[string]any, keys ...string) string {
	if payload == nil {
		return ""
	}
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}
	return ""
}

// TrackedTurnPayloadDiagKV builds structured diagnostic key-value pairs from event payload.
func TrackedTurnPayloadDiagKV(payload map[string]any) []any {
	if payload == nil {
		return []any{"payload_nil", true}
	}

	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	const maxKeySample = 12
	keysTruncated := false
	if len(keys) > maxKeySample {
		keys = keys[:maxKeySample]
		keysTruncated = true
	}
	_, hasTurnObj := payload["turn"].(map[string]any)

	return []any{
		"payload_key_count", len(payload),
		"payload_keys_sample", strings.Join(keys, ","),
		"payload_keys_truncated", keysTruncated,
		"payload_has_turn_obj", hasTurnObj,
		"payload_turn_id", ExtractTrackedTurnID(payload),
		"payload_turn_status", ExtractTrackedTurnStatus(payload),
		"payload_turn_reason", ExtractTrackedTurnReason(payload),
		"payload_status_raw", ExtractTrackedString(payload, "status", "state"),
		"payload_reason_raw", ExtractTrackedString(payload, "reason", "message"),
	}
}

// IsTerminalEventType reports whether event type or method indicates a turn terminal event.
func IsTerminalEventType(eventType, method string) bool {
	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))
	switch {
	case eventKey == "turn_complete" || eventKey == "turn/completed" || eventKey == "idle" ||
		eventKey == "turn_aborted" || eventKey == "codex/event/task_complete" ||
		eventKey == "shutdown_complete":
		return true
	case methodKey == "turn/completed" || methodKey == "turn/aborted" ||
		methodKey == "codex/event/task_complete" || methodKey == "thread/status/changed":
		return true
	case eventKey == "error" || eventKey == "stream_error" ||
		methodKey == "error" || methodKey == "codex/event/stream_error":
		return true
	default:
		return false
	}
}

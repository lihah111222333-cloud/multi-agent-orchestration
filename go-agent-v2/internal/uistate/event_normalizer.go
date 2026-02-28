package uistate

import (
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/util"
)

var classifyMap = map[string]UIType{
	"agent_message_delta":         UITypeAssistantDelta,
	"agent_message_content_delta": UITypeAssistantDelta,
	"agent_message_completed":     UITypeAssistantDone,
	"agent_message":               UITypeAssistantDone,

	"agent_reasoning":               UITypeReasoningDelta,
	"agent_reasoning_delta":         UITypeReasoningDelta,
	"agent_reasoning_raw":           UITypeReasoningDelta,
	"agent_reasoning_raw_delta":     UITypeReasoningDelta,
	"agent_reasoning_section_break": UITypeReasoningDelta,

	"exec_command_begin":        UITypeCommandStart,
	"exec_output_delta":         UITypeCommandOutput,
	"exec_command_output_delta": UITypeCommandOutput,
	"exec_command_end":          UITypeCommandDone,
	"exec_terminal_interaction": UITypeSystem,

	"patch_apply_begin": UITypeFileEditStart,
	"file_read":         UITypeFileEditStart,
	"patch_apply":       UITypeCommandOutput,
	"patch_apply_delta": UITypeCommandOutput,
	"patch_apply_end":   UITypeFileEditDone,
	"file_updated":      UITypeFileEditDone,

	"mcp_tool_call_begin": UITypeToolCall,
	"mcp_tool_call":       UITypeToolCall,
	"dynamic_tool_call":   UITypeSystem,
	"mcp_tool_call_end":   UITypeToolCall,

	"exec_approval_request":        UITypeApprovalRequest,
	"file_change_approval_request": UITypeApprovalRequest,

	"turn_started":              UITypeTurnStarted,
	"task_started":              UITypeTurnStarted,
	"codex/event/task_started":  UITypeTurnStarted,
	"agent/event/task_started":  UITypeTurnStarted,
	"turn_complete":             UITypeTurnComplete,
	"task_complete":             UITypeTurnComplete,
	"codex/event/task_complete": UITypeTurnComplete,
	"agent/event/task_complete": UITypeTurnComplete,
	"turn/completed":            UITypeTurnComplete,
	"turn_aborted":              UITypeTurnComplete,
	"idle":                      UITypeTurnComplete,

	"plan_delta":             UITypePlanDelta,
	"plan_update":            UITypePlanDelta,
	"turn_plan":              UITypePlanDelta,
	"item/plan/delta":        UITypePlanDelta,
	"codex/event/plan_delta": UITypePlanDelta,
	"turn_diff":              UITypeDiffUpdate,

	"user_message": UITypeUserMessage,

	"error":           UITypeError,
	"stream_error":    UITypeError,
	"connection_dead": UITypeError,

	"warning": UITypeSystem,

	"shutdown_complete":       UITypeSystem,
	"session_configured":      UITypeSystem,
	"mcp_startup_update":      UITypeSystem,
	"mcp_startup_complete":    UITypeSystem,
	"mcp_list_tools_response": UITypeSystem,
	"list_skills_response":    UITypeSystem,
	"token_count":             UITypeSystem,
	"context_compacted":       UITypeSystem,
	"thread_name_updated":     UITypeSystem,
	"thread_rolled_back":      UITypeSystem,
	"undo_started":            UITypeSystem,
	"undo_completed":          UITypeSystem,
	"entered_review_mode":     UITypeSystem,
	"exited_review_mode":      UITypeSystem,
	"background_event":        UITypeSystem,

	"collab_agent_spawn_begin":       UITypeSystem,
	"collab_agent_interaction_begin": UITypeSystem,
	"collab_waiting_begin":           UITypeSystem,
	"collab_agent_spawn_end":         UITypeSystem,
	"collab_agent_interaction_end":   UITypeSystem,
	"collab_waiting_end":             UITypeSystem,
}

var classifyMethodMap = map[string]UIType{
	"turn/started":                              UITypeTurnStarted,
	"turn/completed":                            UITypeTurnComplete,
	"turn/plan/updated":                         UITypePlanDelta,
	"item/plan/delta":                           UITypePlanDelta,
	"codex/event/plan_delta":                    UITypePlanDelta,
	"codex/event/task_started":                  UITypeTurnStarted,
	"codex/event/task_complete":                 UITypeTurnComplete,
	"item/commandexecution/requestapproval":     UITypeApprovalRequest,
	"item/commandexecution/terminalinteraction": UITypeSystem,
	"codex/event/mcp_startup_update":            UITypeSystem,
	"codex/event/background_event":              UITypeSystem,
}

func normalizeLifecycleItemKind(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	value = strings.NewReplacer("_", "", "-", "", " ", "", ".", "", "/", "").Replace(value)
	switch {
	case strings.Contains(value, "commandexecution"), strings.HasPrefix(value, "execcommand"), value == "command":
		return "command"
	case strings.Contains(value, "filechange"), strings.Contains(value, "patchapply"), strings.HasPrefix(value, "file"):
		return "file"
	default:
		return ""
	}
}

func appendLifecycleTypeCandidates(candidates *[]string, payload map[string]any) {
	if payload == nil {
		return
	}
	for _, key := range []string{"type", "itemType", "item_type", "kind", "event_type"} {
		if text, ok := payload[key].(string); ok {
			value := strings.TrimSpace(text)
			if value != "" {
				*candidates = append(*candidates, value)
			}
		}
	}
	if nested, ok := payload["item"].(map[string]any); ok {
		appendLifecycleTypeCandidates(candidates, nested)
	}
}

func parseNestedMapAny(raw any) map[string]any {
	if nested, ok := raw.(map[string]any); ok {
		return nested
	}
	var data []byte
	switch nested := raw.(type) {
	case string:
		data = []byte(nested)
	case json.RawMessage:
		data = nested
	case []byte:
		data = nested
	default:
		return nil
	}
	var decoded map[string]any
	if json.Unmarshal(data, &decoded) != nil {
		return nil
	}
	return decoded
}

func classifyItemLifecycleEvent(codexType, method string, payload map[string]any) (UIType, bool) {
	isStart := codexType == "item/started" || codexType == "codex/event/item_started" || method == "item/started"
	isDone := codexType == "item/completed" || codexType == "codex/event/item_completed" || method == "item/completed"
	if !isStart && !isDone {
		return "", false
	}

	candidates := make([]string, 0, 8)
	appendLifecycleTypeCandidates(&candidates, payload)
	for _, key := range []string{"msg", "data", "payload"} {
		appendLifecycleTypeCandidates(&candidates, parseNestedMapAny(payload[key]))
	}

	for _, candidate := range candidates {
		switch normalizeLifecycleItemKind(candidate) {
		case "command":
			if isStart {
				return UITypeCommandStart, true
			}
			return UITypeCommandDone, true
		case "file":
			if isStart {
				return UITypeFileEditStart, true
			}
			return UITypeFileEditDone, true
		}
	}
	return "", false
}

func classifyEventWithMethodAndPayload(codexType, method string, payload map[string]any) UIType {
	codexKey := strings.ToLower(strings.TrimSpace(codexType))
	methodKey := strings.ToLower(strings.TrimSpace(method))
	if uiType, ok := classifyMap[codexKey]; ok {
		return uiType
	}
	if uiType, ok := classifyMethodMap[methodKey]; ok {
		return uiType
	}
	if uiType, ok := classifyItemLifecycleEvent(codexKey, methodKey, payload); ok {
		return uiType
	}
	return UITypeSystem
}

func extractText(payload map[string]any) string {
	return util.ExtractFirstString(payload, "delta", "text", "content", "output", "message")
}

func extractNormalizedCommand(payload map[string]any) string {
	command := strings.TrimSpace(util.ExtractFirstString(
		payload,
		"uiCommand", "command", "cmd",
		"command_display", "commandDisplay", "displayCommand",
	))
	if command != "" {
		return command
	}
	command = strings.TrimSpace(extractNestedFirstString(
		payload,
		[]string{"item", "command"},
		[]string{"item", "cmd"},
		[]string{"item", "command_display"},
		[]string{"item", "commandDisplay"},
		[]string{"item", "displayCommand"},
		[]string{"process", "command"},
		[]string{"process", "command_display"},
		[]string{"process", "commandDisplay"},
		[]string{"process", "displayCommand"},
		[]string{"args", "command"},
		[]string{"args", "cmd"},
		[]string{"arguments", "command"},
		[]string{"arguments", "cmd"},
		[]string{"msg", "command"},
		[]string{"msg", "cmd"},
		[]string{"data", "command"},
		[]string{"data", "cmd"},
		[]string{"payload", "command"},
		[]string{"payload", "cmd"},
	))
	if command != "" {
		return command
	}
	for _, key := range []string{"args", "arguments"} {
		nested := parseNestedMapAny(payload[key])
		if nested == nil {
			continue
		}
		command = strings.TrimSpace(util.ExtractFirstString(
			nested,
			"command", "cmd",
			"command_display", "commandDisplay", "displayCommand",
		))
		if command != "" {
			return command
		}
	}
	return ""
}

func extractNormalizedFiles(payload map[string]any) (file string, files []string) {
	if file := util.AsString(payload["file"]); file != "" {
		return file, []string{file}
	}
	if files := util.AsStringSlice(payload["files"]); len(files) > 0 {
		return files[0], files
	}
	return "", nil
}

func extractExitCodeFromPayload(codexType string, payload map[string]any) *int {
	codexType = strings.ToLower(strings.TrimSpace(codexType))
	if codexType != "exec_command_end" &&
		codexType != "item/completed" &&
		codexType != "codex/event/item_completed" {
		return nil
	}
	switch code := payload["exit_code"].(type) {
	case float64:
		c := int(code)
		return &c
	case int:
		return &code
	case int32:
		c := int(code)
		return &c
	case int64:
		c := int(code)
		return &c
	}
	return nil
}

func NormalizeEvent(codexType, method string, data json.RawMessage) NormalizedEvent {
	var payload map[string]any
	if len(data) > 0 { _ = json.Unmarshal(data, &payload) }
	return NormalizeEventFromPayload(codexType, method, payload)
}

func NormalizeEventFromPayload(codexType, method string, payload map[string]any) NormalizedEvent {
	uiType := classifyEventWithMethodAndPayload(codexType, method, payload)
	result := NormalizedEvent{
		UIType:  uiType,
		RawType: codexType,
		Method:  method,
	}
	result.Text = extractText(payload)
	result.Command = extractNormalizedCommand(payload)
	result.File, result.Files = extractNormalizedFiles(payload)
	result.ExitCode = extractExitCodeFromPayload(codexType, payload)
	return result
}

package uistate

import (
	"encoding/json"
	"strings"
)

// ── classifyEvent: map 查表替代 switch ──

type classifyResult struct {
	uiType UIType
}

var classifyMap = map[string]classifyResult{
	// Assistant Messages
	"agent_message_delta":         {UITypeAssistantDelta},
	"agent_message_content_delta": {UITypeAssistantDelta},
	"agent_message_completed":     {UITypeAssistantDone},
	"agent_message":               {UITypeAssistantDone},

	// Reasoning
	"agent_reasoning":               {UITypeReasoningDelta},
	"agent_reasoning_delta":         {UITypeReasoningDelta},
	"agent_reasoning_raw":           {UITypeReasoningDelta},
	"agent_reasoning_raw_delta":     {UITypeReasoningDelta},
	"agent_reasoning_section_break": {UITypeReasoningDelta},

	// Command Execution
	"exec_command_begin":        {UITypeCommandStart},
	"exec_output_delta":         {UITypeCommandOutput},
	"exec_command_output_delta": {UITypeCommandOutput},
	"exec_command_end":          {UITypeCommandDone},
	"exec_terminal_interaction": {UITypeSystem},

	// File Editing
	"patch_apply_begin": {UITypeFileEditStart},
	"file_read":         {UITypeFileEditStart},
	"patch_apply":       {UITypeCommandOutput},
	"patch_apply_delta": {UITypeCommandOutput},
	"patch_apply_end":   {UITypeFileEditDone},
	"file_updated":      {UITypeFileEditDone},

	// Tool Calls
	"mcp_tool_call_begin": {UITypeToolCall},
	"mcp_tool_call":       {UITypeToolCall},
	"dynamic_tool_call":   {UITypeSystem},
	"mcp_tool_call_end":   {UITypeToolCall},

	// Approval
	"exec_approval_request":        {UITypeApprovalRequest},
	"file_change_approval_request": {UITypeApprovalRequest},

	// Turn Lifecycle
	"turn_started":              {UITypeTurnStarted},
	"task_started":              {UITypeTurnStarted},
	"codex/event/task_started":  {UITypeTurnStarted},
	"agent/event/task_started":  {UITypeTurnStarted},
	"turn_complete":             {UITypeTurnComplete},
	"task_complete":             {UITypeTurnComplete},
	"codex/event/task_complete": {UITypeTurnComplete},
	"agent/event/task_complete": {UITypeTurnComplete},
	"turn/completed":            {UITypeTurnComplete},
	"turn_aborted":              {UITypeTurnComplete},
	"idle":                      {UITypeTurnComplete},

	// Plan / Diff
	"plan_delta":             {UITypePlanDelta},
	"plan_update":            {UITypePlanDelta},
	"turn_plan":              {UITypePlanDelta},
	"item/plan/delta":        {UITypePlanDelta},
	"codex/event/plan_delta": {UITypePlanDelta},
	"turn_diff":              {UITypeDiffUpdate},

	// User Message
	"user_message": {UITypeUserMessage},

	// Errors
	"error":        {UITypeError},
	"stream_error": {UITypeError},

	// Warnings
	"warning": {UITypeSystem},

	// System / Lifecycle
	"shutdown_complete":       {UITypeSystem},
	"session_configured":      {UITypeSystem},
	"mcp_startup_update":      {UITypeSystem},
	"mcp_startup_complete":    {UITypeSystem},
	"mcp_list_tools_response": {UITypeSystem},
	"list_skills_response":    {UITypeSystem},
	"token_count":             {UITypeSystem},
	"context_compacted":       {UITypeSystem},
	"thread_name_updated":     {UITypeSystem},
	"thread_rolled_back":      {UITypeSystem},
	"undo_started":            {UITypeSystem},
	"undo_completed":          {UITypeSystem},
	"entered_review_mode":     {UITypeSystem},
	"exited_review_mode":      {UITypeSystem},
	"background_event":        {UITypeSystem},

	// Collab Agents
	"collab_agent_spawn_begin":       {UITypeSystem},
	"collab_agent_interaction_begin": {UITypeSystem},
	"collab_waiting_begin":           {UITypeSystem},
	"collab_agent_spawn_end":         {UITypeSystem},
	"collab_agent_interaction_end":   {UITypeSystem},
	"collab_waiting_end":             {UITypeSystem},
}

var classifyMethodMap = map[string]classifyResult{
	"turn/started":                              {UITypeTurnStarted},
	"turn/completed":                            {UITypeTurnComplete},
	"turn/plan/updated":                         {UITypePlanDelta},
	"item/plan/delta":                           {UITypePlanDelta},
	"codex/event/plan_delta":                    {UITypePlanDelta},
	"codex/event/task_started":                  {UITypeTurnStarted},
	"codex/event/task_complete":                 {UITypeTurnComplete},
	"item/commandExecution/terminalInteraction": {UITypeSystem},
	"codex/event/mcp_startup_update":            {UITypeSystem},
	"codex/event/background_event":              {UITypeSystem},
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
	switch nested := raw.(type) {
	case map[string]any:
		return nested
	case string:
		var decoded map[string]any
		if json.Unmarshal([]byte(nested), &decoded) == nil {
			return decoded
		}
	case json.RawMessage:
		var decoded map[string]any
		if json.Unmarshal(nested, &decoded) == nil {
			return decoded
		}
	case []byte:
		var decoded map[string]any
		if json.Unmarshal(nested, &decoded) == nil {
			return decoded
		}
	}
	return nil
}

func classifyItemLifecycleEvent(codexType, method string, payload map[string]any) (UIType, bool) {
	codexLower := strings.ToLower(strings.TrimSpace(codexType))
	methodLower := strings.ToLower(strings.TrimSpace(method))

	isStart := codexLower == "item/started" || codexLower == "codex/event/item_started" || methodLower == "item/started"
	isDone := codexLower == "item/completed" || codexLower == "codex/event/item_completed" || methodLower == "item/completed"
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

// classifyEventWithMethodAndPayload 按 codex 原始事件类型 + method + payload 分类。
func classifyEventWithMethodAndPayload(codexType, method string, payload map[string]any) UIType {
	if r, ok := classifyMap[codexType]; ok {
		return r.uiType
	}
	if key := strings.TrimSpace(method); key != "" {
		if r, ok := classifyMethodMap[key]; ok {
			return r.uiType
		}
	}
	if uiType, ok := classifyItemLifecycleEvent(codexType, method, payload); ok {
		return uiType
	}
	return UITypeSystem
}

// classifyEventWithMethod 按 codex 原始事件类型 + method 分类 (map 查表, O(1))。
func classifyEventWithMethod(codexType, method string) UIType {
	return classifyEventWithMethodAndPayload(codexType, method, nil)
}

// classifyEvent 按 codex 原始事件类型分类 (map 查表, O(1))。
func classifyEvent(codexType string) UIType {
	return classifyEventWithMethodAndPayload(codexType, "", nil)
}

// ── NormalizeEvent 辅助函数 ──

// extractText 按优先级从 payload 提取文本: delta > text > content > output > message。
func extractText(payload map[string]any) string {
	for _, key := range []string{"delta", "text", "content", "output", "message"} {
		if v, ok := payload[key].(string); ok {
			return v
		}
	}
	return ""
}

func extractNormalizedCommand(payload map[string]any) string {
	command := strings.TrimSpace(extractFirstString(
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
		command = strings.TrimSpace(extractFirstString(
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

// extractNormalizedFiles 从 payload 提取文件路径。
func extractNormalizedFiles(codexType string, payload map[string]any) (file string, files []string) {
	switch {
	case codexType == "patch_apply_begin" || codexType == "item/fileChange/started":
		if f, ok := payload["file"].(string); ok {
			return f, []string{f}
		}
		return "", nil
	default:
		if v, ok := payload["file"].(string); ok {
			return v, []string{v}
		}
		if arr, ok := payload["files"].([]any); ok {
			var strs []string
			for _, f := range arr {
				if s, ok := f.(string); ok {
					strs = append(strs, s)
				}
			}
			if len(strs) > 0 {
				return strs[0], strs
			}
		}
		return "", nil
	}
}

// extractExitCodeFromPayload 仅在 exec_command_end 事件中提取退出码。
func extractExitCodeFromPayload(codexType string, payload map[string]any) *int {
	if codexType != "exec_command_end" &&
		codexType != "item/completed" &&
		codexType != "codex/event/item_completed" {
		return nil
	}
	if code, ok := payload["exit_code"].(float64); ok {
		c := int(code)
		return &c
	}
	return nil
}

// NormalizeEvent 将 codex 事件归一化为前端可渲染的结构化事件。
//
// 纯函数, 无状态, 无锁, 热路径安全。
func NormalizeEvent(codexType, method string, data json.RawMessage) NormalizedEvent {
	var payload map[string]any
	if len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}

	return NormalizeEventFromPayload(codexType, method, payload)
}

// NormalizeEventFromPayload 将已解码 payload 的事件归一化为前端可渲染结构。
func NormalizeEventFromPayload(codexType, method string, payload map[string]any) NormalizedEvent {
	if payload == nil {
		payload = map[string]any{}
	}
	uiType := classifyEventWithMethodAndPayload(codexType, method, payload)

	result := NormalizedEvent{
		UIType:  uiType,
		RawType: codexType,
		Method:  method,
	}

	result.Text = extractText(payload)
	result.Command = extractNormalizedCommand(payload)

	result.File, result.Files = extractNormalizedFiles(codexType, payload)

	if result.File == "" && len(result.Files) > 0 {
		result.File = result.Files[0]
	}

	result.ExitCode = extractExitCodeFromPayload(codexType, payload)

	return result
}

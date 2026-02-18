package uistate

import "encoding/json"

// ── classifyEvent: map 查表替代 switch ──

type classifyResult struct {
	uiType   UIType
	uiStatus UIStatus
}

var classifyMap = map[string]classifyResult{
	// Assistant Messages
	"agent_message_delta":         {UITypeAssistantDelta, UIStatusThinking},
	"agent_message_content_delta": {UITypeAssistantDelta, UIStatusThinking},
	"agent_message_completed":     {UITypeAssistantDone, UIStatusThinking},
	"agent_message":               {UITypeAssistantDone, UIStatusThinking},

	// Reasoning
	"agent_reasoning":               {UITypeReasoningDelta, UIStatusThinking},
	"agent_reasoning_delta":         {UITypeReasoningDelta, UIStatusThinking},
	"agent_reasoning_raw":           {UITypeReasoningDelta, UIStatusThinking},
	"agent_reasoning_raw_delta":     {UITypeReasoningDelta, UIStatusThinking},
	"agent_reasoning_section_break": {UITypeReasoningDelta, UIStatusThinking},

	// Command Execution
	"exec_command_begin":        {UITypeCommandStart, UIStatusRunning},
	"exec_output_delta":         {UITypeCommandOutput, UIStatusRunning},
	"exec_command_output_delta": {UITypeCommandOutput, UIStatusRunning},
	"exec_command_end":          {UITypeCommandDone, UIStatusRunning},

	// File Editing
	"patch_apply_begin": {UITypeFileEditStart, UIStatusRunning},
	"file_read":         {UITypeFileEditStart, UIStatusRunning},
	"patch_apply":       {UITypeCommandOutput, UIStatusRunning},
	"patch_apply_delta": {UITypeCommandOutput, UIStatusRunning},
	"patch_apply_end":   {UITypeFileEditDone, UIStatusRunning},
	"file_updated":      {UITypeFileEditDone, UIStatusRunning},

	// Tool Calls
	"mcp_tool_call_begin": {UITypeToolCall, UIStatusRunning},
	"mcp_tool_call":       {UITypeToolCall, UIStatusRunning},
	"dynamic_tool_call":   {UITypeToolCall, UIStatusRunning},
	"mcp_tool_call_end":   {UITypeCommandDone, UIStatusRunning},

	// Approval
	"exec_approval_request":        {UITypeApprovalRequest, UIStatusRunning},
	"file_change_approval_request": {UITypeApprovalRequest, UIStatusRunning},

	// Turn Lifecycle
	"turn_started":  {UITypeTurnStarted, UIStatusThinking},
	"turn_complete": {UITypeTurnComplete, UIStatusIdle},
	"idle":          {UITypeTurnComplete, UIStatusIdle},

	// Plan / Diff
	"plan_delta":  {UITypePlanDelta, UIStatusThinking},
	"plan_update": {UITypePlanDelta, UIStatusThinking},
	"turn_diff":   {UITypeDiffUpdate, UIStatusIdle},

	// User Message
	"user_message": {UITypeUserMessage, UIStatusThinking},

	// Errors
	"error":        {UITypeError, UIStatusError},
	"stream_error": {UITypeError, UIStatusError},

	// Warnings
	"warning": {UITypeSystem, ""},

	// System / Lifecycle
	"shutdown_complete":       {UITypeSystem, UIStatusIdle},
	"session_configured":      {UITypeSystem, ""},
	"mcp_startup_complete":    {UITypeSystem, ""},
	"mcp_list_tools_response": {UITypeSystem, ""},
	"list_skills_response":    {UITypeSystem, ""},
	"token_count":             {UITypeSystem, ""},
	"context_compacted":       {UITypeSystem, ""},
	"thread_name_updated":     {UITypeSystem, ""},
	"thread_rolled_back":      {UITypeSystem, ""},
	"undo_started":            {UITypeSystem, ""},
	"undo_completed":          {UITypeSystem, ""},
	"entered_review_mode":     {UITypeSystem, ""},
	"exited_review_mode":      {UITypeSystem, ""},
	"background_event":        {UITypeSystem, ""},

	// Collab Agents
	"collab_agent_spawn_begin":       {UITypeSystem, UIStatusRunning},
	"collab_agent_interaction_begin": {UITypeSystem, UIStatusRunning},
	"collab_waiting_begin":           {UITypeSystem, UIStatusRunning},
	"collab_agent_spawn_end":         {UITypeSystem, UIStatusRunning},
	"collab_agent_interaction_end":   {UITypeSystem, UIStatusRunning},
	"collab_waiting_end":             {UITypeSystem, UIStatusRunning},
}

// classifyEvent 按 codex 原始事件类型分类 (map 查表, O(1))。
func classifyEvent(codexType string) (UIType, UIStatus) {
	if r, ok := classifyMap[codexType]; ok {
		return r.uiType, r.uiStatus
	}
	return UITypeSystem, ""
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
	if codexType != "exec_command_end" {
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

	uiType, uiStatus := classifyEvent(codexType)

	result := NormalizedEvent{
		UIType:   uiType,
		UIStatus: uiStatus,
	}

	result.Text = extractText(payload)

	if v, ok := payload["command"].(string); ok {
		result.Command = v
	}

	result.File, result.Files = extractNormalizedFiles(codexType, payload)

	if result.File == "" && len(result.Files) > 0 {
		result.File = result.Files[0]
	}

	result.ExitCode = extractExitCodeFromPayload(codexType, payload)

	return result
}

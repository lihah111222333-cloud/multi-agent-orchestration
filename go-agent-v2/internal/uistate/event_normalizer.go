package uistate

import "encoding/json"

// NormalizeEvent 将 codex 事件归一化为前端可渲染的结构化事件。
//
// 纯函数, 无状态, 无锁, 热路径安全。
func NormalizeEvent(codexType, method string, data json.RawMessage) NormalizedEvent {
	var payload map[string]any
	// data is raw JSON, let's unmarshal it into a generic map
	if len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}
	if payload == nil {
		payload = map[string]any{} // Prevent panic on subsequent lookups
	}

	uiType, uiStatus := classifyEvent(codexType)

	result := NormalizedEvent{
		UIType:   uiType,
		UIStatus: uiStatus,
	}

	// 1. Extract Text (Priority: delta > text > content > output > message)
	if v, ok := payload["delta"].(string); ok {
		result.Text = v
	} else if v, ok := payload["text"].(string); ok {
		result.Text = v
	} else if v, ok := payload["content"].(string); ok {
		result.Text = v
	} else if v, ok := payload["output"].(string); ok {
		result.Text = v
	} else if v, ok := payload["message"].(string); ok {
		result.Text = v
	}

	// 2. Extract Command
	if v, ok := payload["command"].(string); ok {
		result.Command = v
	}

	// 3. Extract Files
	// Prioritize logic:
	// - If specific file fields exist, use them.
	// - Logic mimics JS: file > files
	if codexType == "patch_apply_begin" {
		if f, ok := payload["file"].(string); ok {
			result.Files = []string{f}
		} else if d, ok := payload["delta"].(string); ok {
			// Backward compatibility: parsing diff header is complex and prone to errors in Go without regex/state
			// JS version had fallback Logic. For now, rely on `file` field usually being present in structured events.
			// If strictly needed, we can add simple parsing.
			// Assuming `file` field is populated by the backend for these events now or will be.
			// If delta contains "diff --git a/...", we might extract it, but let's stick to mapped fields first.
			_ = d
		}
	} else if codexType == "item/fileChange/started" { // Use method if type matches? NO, classifyEvent only uses codexType mostly
		if f, ok := payload["file"].(string); ok {
			result.Files = []string{f}
		}
	} else {
		// Generic fallback
		if v, ok := payload["file"].(string); ok {
			result.Files = []string{v}
		} else if files, ok := payload["files"].([]any); ok {
			// handle []string from JSON
			var strs []string
			for _, f := range files {
				if s, ok := f.(string); ok {
					strs = append(strs, s)
				}
			}
			if len(strs) > 0 {
				result.Files = strs
			}
		}
	}

	// Ensure File field is also populated if Files has 1 element (for compatibility if needed, though NormalizedEvent struct has both?)
	// struct has File string and Files []string. Let's populate File if Files has 1, or vice versa?
	// The struct definition in plan has both `File string` and `Files []string`.
	// Logic: If `File` is set, append to `Files`.
	if result.File == "" && len(result.Files) > 0 {
		result.File = result.Files[0]
	}

	// 4. Extract ExitCode
	if codexType == "exec_command_end" {
		if code, ok := payload["exit_code"].(float64); ok { // JSON numbers are float64 in generic map
			c := int(code)
			result.ExitCode = &c
		}
	}

	return result
}

// classifyEvent 按 codex 原始事件类型分类。
func classifyEvent(codexType string) (UIType, UIStatus) {
	switch codexType {
	// ── Assistant Messages ──
	case "agent_message_delta", "agent_message_content_delta":
		return UITypeAssistantDelta, UIStatusThinking
	case "agent_message_completed", "agent_message":
		return UITypeAssistantDone, UIStatusThinking

	// ── Reasoning ──
	case "agent_reasoning", "agent_reasoning_delta",
		"agent_reasoning_raw", "agent_reasoning_raw_delta",
		"agent_reasoning_section_break":
		return UITypeReasoningDelta, UIStatusThinking

	// ── Command Execution ──
	case "exec_command_begin":
		return UITypeCommandStart, UIStatusRunning
	case "exec_output_delta", "exec_command_output_delta":
		return UITypeCommandOutput, UIStatusRunning
	case "exec_command_end":
		return UITypeCommandDone, UIStatusRunning

	// ── File Editing ──
	case "patch_apply_begin", "file_read":
		return UITypeFileEditStart, UIStatusRunning
	case "patch_apply", "patch_apply_delta":
		return UITypeCommandOutput, UIStatusRunning // Treat patch output as command output/logs
	case "patch_apply_end", "file_updated":
		return UITypeFileEditDone, UIStatusRunning

	// ── Tool Calls ──
	case "mcp_tool_call_begin", "mcp_tool_call", "dynamic_tool_call":
		return UITypeToolCall, UIStatusRunning
	case "mcp_tool_call_end":
		return UITypeCommandDone, UIStatusRunning

	// ── Approval ──
	case "exec_approval_request", "file_change_approval_request":
		return UITypeApprovalRequest, UIStatusRunning

	// ── Turn Lifecycle ──
	case "turn_started":
		return UITypeTurnStarted, UIStatusThinking
	case "turn_complete", "idle":
		return UITypeTurnComplete, UIStatusIdle

	// ── Plan / Diff ──
	case "plan_delta", "plan_update":
		return UITypePlanDelta, UIStatusThinking
	case "turn_diff":
		return UITypeDiffUpdate, UIStatusIdle

	// ── User Message ──
	case "user_message":
		return UITypeUserMessage, UIStatusThinking

	// ── Errors ──
	case "error", "stream_error":
		return UITypeError, UIStatusError

	// ── Warnings (System) ──
	case "warning":
		return UITypeSystem, ""

	// ── System / Lifecycle ──
	case "shutdown_complete":
		return UITypeSystem, UIStatusIdle
	case "session_configured", "mcp_startup_complete",
		"mcp_list_tools_response", "list_skills_response",
		"token_count", "context_compacted",
		"thread_name_updated", "thread_rolled_back",
		"undo_started", "undo_completed",
		"entered_review_mode", "exited_review_mode",
		"background_event":
		return UITypeSystem, ""

	// ── Collab Agents ──
	case "collab_agent_spawn_begin", "collab_agent_interaction_begin",
		"collab_waiting_begin":
		return UITypeSystem, UIStatusRunning
	case "collab_agent_spawn_end", "collab_agent_interaction_end",
		"collab_waiting_end":
		return UITypeSystem, UIStatusRunning
	}

	// Fallback
	return UITypeSystem, ""
}

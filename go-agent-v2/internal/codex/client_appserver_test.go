package codex

import (
	"testing"
)

// TestMethodToEventMap_PackageLevel 验证 methodToEventMap 为包级变量 (非函数内局部变量)。
// 确保零分配热路径查找。
func TestMethodToEventMap_PackageLevel(t *testing.T) {
	// 验证 map 已初始化且非空
	if len(methodToEventMap) == 0 {
		t.Fatal("methodToEventMap should not be empty")
	}

	// 验证关键映射存在
	tests := []struct {
		method string
		event  string
	}{
		{"thread/started", EventSessionConfigured},
		{"turn/started", EventTurnStarted},
		{"turn/completed", EventTurnComplete},
		{"item/agentMessage/delta", EventAgentMessageDelta},
		{"item/commandExecution/outputDelta", EventExecCommandOutputDelta},
		{"item/reasoning/textDelta", EventAgentReasoningRawDelta},
		{"item/commandExecution/requestApproval", EventExecApprovalRequest},
		{"item/tool/call", EventDynamicToolCall},
		{"configWarning", EventWarning},
		{"thread/compacted", EventContextCompacted},
		{"agent/event/agent_message_content_delta", EventAgentMessageDelta},
		{"agent/event/turn_started", EventTurnStarted},
		{"agent/event/turn_completed", EventTurnComplete},
		{"agent/event/dynamic_tool_call", EventDynamicToolCall},
		{"agent/event/shutdown_complete", EventShutdownComplete},
		{"agent/event/error", EventError},
		{"codex/event/task_started", EventTurnStarted},
		{"codex/event/task_complete", "codex/event/task_complete"},
		{"codex/event/agent_message_content_delta", EventAgentMessageDelta},
		{"codex/event/item_completed", "item/completed"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got, ok := mapMethodToEventType(tt.method)
			if !ok {
				t.Fatalf("mapMethodToEventType missing key %q", tt.method)
			}
			if got != tt.event {
				t.Errorf("mapMethodToEventType[%q] = %q, want %q", tt.method, got, tt.event)
			}
		})
	}
}

func TestMapMethodToEventType_AllProtocolFamilies(t *testing.T) {
	methods := []string{
		"error",
		"thread/started",
		"thread/name/updated",
		"thread/tokenUsage/updated",
		"turn/started",
		"turn/completed",
		"turn/diff/updated",
		"turn/plan/updated",
		"item/started",
		"item/completed",
		"rawResponseItem/completed",
		"item/agentMessage/delta",
		"item/plan/delta",
		"item/commandExecution/outputDelta",
		"item/commandExecution/terminalInteraction",
		"item/fileChange/outputDelta",
		"item/mcpToolCall/progress",
		"mcpServer/oauthLogin/completed",
		"account/updated",
		"account/rateLimits/updated",
		"app/list/updated",
		"item/reasoning/summaryTextDelta",
		"item/reasoning/summaryPartAdded",
		"item/reasoning/textDelta",
		"thread/compacted",
		"deprecationNotice",
		"configWarning",
		"fuzzyFileSearch/sessionUpdated",
		"fuzzyFileSearch/sessionCompleted",
		"windows/worldWritableWarning",
		"account/login/completed",
		"authStatusChange",
		"loginChatGptComplete",
		"sessionConfigured",
		"item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"item/tool/requestUserInput",
		"item/tool/call",
		"account/chatgptAuthTokens/refresh",
		"applyPatchApproval",
		"execCommandApproval",
	}

	for _, method := range methods {
		if _, ok := mapMethodToEventType(method); !ok {
			t.Fatalf("method should be mapped: %s", method)
		}
	}
}

func TestMapMethodToEventType_CodexAndAgentFamiliesFallback(t *testing.T) {
	tests := []string{
		"codex/event/some_new_event",
		"agent/event/some_new_event",
		"item/someNewKind/someUpdate",
		"turn/someFutureEvent",
		"thread/someFutureEvent",
	}

	for _, method := range tests {
		got, ok := mapMethodToEventType(method)
		if !ok {
			t.Fatalf("expected mapped fallback for %q", method)
		}
		if got != method {
			t.Fatalf("fallback should keep method as event type: got %q want %q", got, method)
		}
	}
}

// TestJsonRPCToEvent_KnownMethod 验证已知方法正确映射。
func TestJsonRPCToEvent_KnownMethod(t *testing.T) {
	c := &AppServerClient{}
	msg := jsonRPCMessage{Method: "agent/event/turn_started", Params: []byte(`{}`)}
	event := c.jsonRPCToEvent(msg)
	if event.Type != EventTurnStarted {
		t.Errorf("jsonRPCToEvent(%q).Type = %q, want %q",
			msg.Method, event.Type, EventTurnStarted)
	}
}

// TestJsonRPCToEvent_UnknownMethod 验证未知方法使用 method 名作为 type。
func TestJsonRPCToEvent_UnknownMethod(t *testing.T) {
	c := &AppServerClient{}
	msg := jsonRPCMessage{Method: "some/unknown/method", Params: []byte(`{}`)}
	event := c.jsonRPCToEvent(msg)
	if event.Type != "some/unknown/method" {
		t.Errorf("jsonRPCToEvent(%q).Type = %q, want %q",
			msg.Method, event.Type, "some/unknown/method")
	}
}

// TestJsonRPCToEvent_RawDynamicToolCallRequest_RemainsRaw 验证 raw 通知副本不会被当成可回复的工具调用。
func TestJsonRPCToEvent_RawDynamicToolCallRequest_RemainsRaw(t *testing.T) {
	c := &AppServerClient{}
	msg := jsonRPCMessage{
		Method: "codex/event/dynamic_tool_call_request",
		Params: []byte(`{"msg":{"callId":"call_1"}}`),
	}
	event := c.jsonRPCToEvent(msg)
	if event.Type != "codex/event/dynamic_tool_call_request" {
		t.Errorf("jsonRPCToEvent(%q).Type = %q, want raw method",
			msg.Method, event.Type)
	}
}

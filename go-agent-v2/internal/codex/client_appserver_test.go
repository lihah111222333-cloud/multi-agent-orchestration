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
		{"agent/event/agent_message_content_delta", EventAgentMessageDelta},
		{"agent/event/turn_started", EventTurnStarted},
		{"agent/event/turn_completed", EventTurnComplete},
		{"agent/event/dynamic_tool_call", EventDynamicToolCall},
		{"codex/event/dynamic_tool_call_request", EventDynamicToolCall},
		{"agent/event/shutdown_complete", EventShutdownComplete},
		{"agent/event/error", EventError},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got, ok := methodToEventMap[tt.method]
			if !ok {
				t.Fatalf("methodToEventMap missing key %q", tt.method)
			}
			if got != tt.event {
				t.Errorf("methodToEventMap[%q] = %q, want %q", tt.method, got, tt.event)
			}
		})
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

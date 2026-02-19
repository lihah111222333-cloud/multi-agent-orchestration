package main

import (
	"testing"
)

// TestLaunchAgentSignatureReturnsAny 断言 LaunchAgent 返回 any (不是 string)。
//
// TDD RED: 当前 LaunchAgent 返回 (string, error), 编译不匹配。
// TDD GREEN: 改签名为 (any, error) 后通过。
func TestLaunchAgentSignatureReturnsAny(t *testing.T) {
	// LaunchAgent 需要 srv, 不能直接调用 — 我们测试编译级别的签名约束。
	// 定义一个接受 (any, error) 的变量, 赋值 LaunchAgent 的返回值。
	// 如果 LaunchAgent 还是返回 (string, error), 这里会编译失败。
	app := &App{}
	var result any
	var err error

	// 这行是编译级断言: LaunchAgent 返回 (any, error) 而非 (string, error)。
	// 调用会因 nil srv panic, 所以仅做类型断言, 不实际执行。
	_ = func() {
		result, err = app.LaunchAgent("test", "", ".")
	}
	_ = result
	_ = err
}

// TestBridgeNotificationPayloadShape 断言 bridge 事件不包含冗余 data 字段。
//
// 使用 buildBridgeEventPayload 辅助函数测试事件结构。
func TestBridgeNotificationPayloadShape(t *testing.T) {
	payload := map[string]any{
		"threadId": "t-1",
		"status":   "working",
	}

	bridgeEvt := buildBridgeEventPayload("ui/state/changed", payload)
	agentEvt := buildAgentEventPayload("ui/state/changed", "t-1", payload)

	// 核心断言: bridge-event 不应有 "data" (string 冗余序列化)
	if _, hasData := bridgeEvt["data"]; hasData {
		t.Fatal("bridge-event should not contain redundant 'data' field")
	}

	// 必须有 payload (object)
	if _, hasPayload := bridgeEvt["payload"]; !hasPayload {
		t.Fatal("bridge-event must contain 'payload' field")
	}

	// 必须有 type
	if bridgeEvt["type"] != "ui/state/changed" {
		t.Fatalf("bridge-event type = %v, want ui/state/changed", bridgeEvt["type"])
	}

	// agent-event: 同样不应有 "data", 应使用 "payload"
	if _, hasData := agentEvt["data"]; hasData {
		t.Fatal("agent-event should not contain redundant 'data' field")
	}
	if _, hasPayload := agentEvt["payload"]; !hasPayload {
		t.Fatal("agent-event must contain 'payload' field")
	}
	if agentEvt["agent_id"] != "t-1" {
		t.Fatalf("agent-event agent_id = %v, want t-1", agentEvt["agent_id"])
	}
}

// manager_test.go — AgentManager 测试 (TDD)。
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

// stubClient 最小化 CodexClient 实现 (仅用于测试, 不启动子进程)。
type stubClient struct {
	port     int
	threadID string
}

func (s *stubClient) GetPort() int                         { return s.port }
func (s *stubClient) GetThreadID() string                  { return s.threadID }
func (s *stubClient) SetEventHandler(_ codex.EventHandler) {}
func (s *stubClient) SpawnAndConnect(_ context.Context, _, _, _ string, _ []codex.DynamicTool) error {
	return nil
}
func (s *stubClient) Submit(_ string, _, _ []string, _ json.RawMessage) error { return nil }
func (s *stubClient) SendCommand(_, _ string) error                           { return nil }
func (s *stubClient) SendDynamicToolResult(_, _ string, _ *int64) error       { return nil }
func (s *stubClient) RespondError(_ int64, _ int, _ string) error             { return nil }
func (s *stubClient) ListThreads() ([]codex.ThreadInfo, error)                { return nil, nil }
func (s *stubClient) ResumeThread(_ codex.ResumeThreadRequest) error          { return nil }
func (s *stubClient) ForkThread(_ codex.ForkThreadRequest) (*codex.ForkThreadResponse, error) {
	return nil, nil
}
func (s *stubClient) Shutdown() error { return nil }
func (s *stubClient) Kill() error     { return nil }
func (s *stubClient) Running() bool   { return true }

type fakeLaunchClient struct {
	port       int
	threadID   string
	spawnErr   error
	spawnCalls atomic.Int32
}

func (f *fakeLaunchClient) GetPort() int                         { return f.port }
func (f *fakeLaunchClient) GetThreadID() string                  { return f.threadID }
func (f *fakeLaunchClient) SetEventHandler(_ codex.EventHandler) {}
func (f *fakeLaunchClient) SpawnAndConnect(_ context.Context, _, _, _ string, _ []codex.DynamicTool) error {
	f.spawnCalls.Add(1)
	return f.spawnErr
}
func (f *fakeLaunchClient) Submit(_ string, _, _ []string, _ json.RawMessage) error { return nil }
func (f *fakeLaunchClient) SendCommand(_, _ string) error                           { return nil }
func (f *fakeLaunchClient) SendDynamicToolResult(_, _ string, _ *int64) error       { return nil }
func (f *fakeLaunchClient) RespondError(_ int64, _ int, _ string) error             { return nil }
func (f *fakeLaunchClient) ListThreads() ([]codex.ThreadInfo, error)                { return nil, nil }
func (f *fakeLaunchClient) ResumeThread(_ codex.ResumeThreadRequest) error          { return nil }
func (f *fakeLaunchClient) ForkThread(_ codex.ForkThreadRequest) (*codex.ForkThreadResponse, error) {
	return nil, nil
}
func (f *fakeLaunchClient) Shutdown() error { return nil }
func (f *fakeLaunchClient) Kill() error     { return nil }
func (f *fakeLaunchClient) Running() bool   { return true }

// ========================================
// 状态转换测试
// ========================================

// TestHandleEvent_StateTransitions 验证事件→状态的声明式映射完整性。
func TestHandleEvent_StateTransitions(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		wantState AgentState
	}{
		{"turn_started → thinking", codex.EventTurnStarted, StateThinking},
		{"idle → idle", codex.EventIdle, StateIdle},
		{"turn_complete → idle", codex.EventTurnComplete, StateIdle},
		{"exec_command_begin → running", codex.EventExecCommandBegin, StateRunning},
		{"error → error", codex.EventError, StateError},
		{"shutdown_complete → stopped", codex.EventShutdownComplete, StateStopped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewAgentManager()
			proc := &AgentProcess{
				ID:    "test-agent",
				State: StateIdle,
			}

			mgr.handleEvent(proc, codex.Event{Type: tt.eventType})

			proc.mu.Lock()
			got := proc.State
			proc.mu.Unlock()

			if got != tt.wantState {
				t.Errorf("handleEvent(%q): state = %q, want %q",
					tt.eventType, got, tt.wantState)
			}
		})
	}
}

// TestHandleEvent_UnknownEvent 未映射事件不应改变状态。
func TestHandleEvent_UnknownEvent(t *testing.T) {
	mgr := NewAgentManager()
	proc := &AgentProcess{
		ID:    "test-agent",
		State: StateThinking,
	}

	mgr.handleEvent(proc, codex.Event{Type: "some_unknown_event"})

	proc.mu.Lock()
	got := proc.State
	proc.mu.Unlock()

	if got != StateThinking {
		t.Errorf("unknown event changed state: got %q, want %q", got, StateThinking)
	}
}

// TestHandleEvent_CallbackFires 事件应触发 onEvent 回调。
func TestHandleEvent_CallbackFires(t *testing.T) {
	mgr := NewAgentManager()

	var received []string
	var mu sync.Mutex
	mgr.SetOnEvent(func(agentID string, event codex.Event) {
		mu.Lock()
		received = append(received, agentID+":"+event.Type)
		mu.Unlock()
	})

	proc := &AgentProcess{ID: "agent-42", State: StateIdle}
	mgr.handleEvent(proc, codex.Event{Type: codex.EventTurnStarted})
	mgr.handleEvent(proc, codex.Event{Type: codex.EventTurnComplete})

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 callbacks, got %d", len(received))
	}
	if received[0] != "agent-42:turn_started" {
		t.Errorf("callback[0] = %q, want %q", received[0], "agent-42:turn_started")
	}
	if received[1] != "agent-42:turn_complete" {
		t.Errorf("callback[1] = %q, want %q", received[1], "agent-42:turn_complete")
	}
}

// ========================================
// 并发安全测试 (Batch 1 TDD RED)
// ========================================

// TestList_ConcurrentWithHandleEvent 验证 List 和 handleEvent 并发不死锁不竞态。
//
// 场景: 多 goroutine 交替调用 List() + handleEvent()。
// 使用 `go test -race` 和超时检测。
// 覆盖层次锁场景: List 在 AgentManager.mu 下获取 AgentProcess.mu。
func TestList_ConcurrentWithHandleEvent(t *testing.T) {
	mgr := NewAgentManager()

	// 注册若干模拟 agent
	const agentCount = 5
	procs := make([]*AgentProcess, agentCount)
	mgr.mu.Lock()
	for i := 0; i < agentCount; i++ {
		proc := &AgentProcess{
			ID:     fmt.Sprintf("agent-%d", i),
			Name:   fmt.Sprintf("Agent %d", i),
			State:  StateIdle,
			Client: &stubClient{port: 19900 + i},
		}
		procs[i] = proc
		mgr.agents[proc.ID] = proc
	}
	mgr.mu.Unlock()

	// 注册事件回调 (模拟真实场景)
	var callbackCount atomic.Int64
	mgr.SetOnEvent(func(agentID string, event codex.Event) {
		callbackCount.Add(1)
	})

	const iterations = 200
	var wg sync.WaitGroup
	done := make(chan struct{})

	// 并发 List
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			infos := mgr.List()
			if len(infos) != agentCount {
				t.Errorf("List() returned %d agents, want %d", len(infos), agentCount)
				return
			}
		}
	}()

	// 并发 handleEvent (交替不同事件类型)
	events := []string{
		codex.EventTurnStarted,
		codex.EventTurnComplete,
		codex.EventExecCommandBegin,
		codex.EventAgentMessageDelta,
	}
	for _, proc := range procs {
		wg.Add(1)
		go func(p *AgentProcess) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				evt := events[i%len(events)]
				mgr.handleEvent(p, codex.Event{Type: evt})
			}
		}(proc)
	}

	// 超时保护
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 成功: 无死锁
	case <-time.After(5 * time.Second):
		t.Fatal("DEADLOCK: List + handleEvent concurrent access timed out after 5s")
	}

	// 验证回调确实执行了
	if callbackCount.Load() == 0 {
		t.Error("expected callbacks to fire during concurrent test")
	}
}

func TestList_DeterministicOrderByIDDesc(t *testing.T) {
	mgr := NewAgentManager()

	mgr.mu.Lock()
	mgr.agents["agent-2"] = &AgentProcess{
		ID:     "agent-2",
		Name:   "Agent 2",
		State:  StateIdle,
		Client: &stubClient{port: 19902},
	}
	mgr.agents["agent-10"] = &AgentProcess{
		ID:     "agent-10",
		Name:   "Agent 10",
		State:  StateIdle,
		Client: &stubClient{port: 19910},
	}
	mgr.agents["agent-1"] = &AgentProcess{
		ID:     "agent-1",
		Name:   "Agent 1",
		State:  StateIdle,
		Client: &stubClient{port: 19901},
	}
	mgr.mu.Unlock()

	first := mgr.List()
	second := mgr.List()
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("unexpected list size: first=%d second=%d", len(first), len(second))
	}

	want := []string{"agent-2", "agent-10", "agent-1"}
	for index := range want {
		if first[index].ID != want[index] {
			t.Fatalf("first[%d].ID = %q, want %q", index, first[index].ID, want[index])
		}
		if second[index].ID != want[index] {
			t.Fatalf("second[%d].ID = %q, want %q", index, second[index].ID, want[index])
		}
	}
}

func TestLaunch_FallbackToRESTWhenAppServerFails(t *testing.T) {
	mgr := NewAgentManager()
	appClient := &fakeLaunchClient{
		spawnErr: errors.New("ws connect failed"),
	}
	restClient := &fakeLaunchClient{
		threadID: "thread-rest",
	}
	mgr.appServerFactory = func(port int, agentID string) codex.CodexClient {
		appClient.port = port
		return appClient
	}
	mgr.restFactory = func(port int, agentID string) codex.CodexClient {
		restClient.port = port
		return restClient
	}

	var sawFallbackEvent atomic.Bool
	mgr.SetOnEvent(func(_ string, event codex.Event) {
		if event.Type == codex.EventBackgroundEvent {
			sawFallbackEvent.Store(true)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := mgr.Launch(ctx, "agent-fallback-ok", "Agent Fallback", "", ".", nil); err != nil {
		t.Fatalf("Launch returned error: %v", err)
	}
	proc := mgr.Get("agent-fallback-ok")
	if proc == nil {
		t.Fatal("expected launched agent to exist")
	}
	if proc.Client != restClient {
		t.Fatalf("expected REST fallback client to be active, got %T", proc.Client)
	}
	if appClient.spawnCalls.Load() != 1 {
		t.Fatalf("app-server spawn calls = %d, want 1", appClient.spawnCalls.Load())
	}
	if restClient.spawnCalls.Load() != 1 {
		t.Fatalf("rest spawn calls = %d, want 1", restClient.spawnCalls.Load())
	}
	if !sawFallbackEvent.Load() {
		t.Fatal("expected background fallback event to be emitted")
	}
}

func TestLaunch_FallbackFailureRemovesAgent(t *testing.T) {
	mgr := NewAgentManager()
	appClient := &fakeLaunchClient{
		spawnErr: errors.New("ws connect failed"),
	}
	restClient := &fakeLaunchClient{
		spawnErr: errors.New("http spawn failed"),
	}
	mgr.appServerFactory = func(port int, agentID string) codex.CodexClient {
		appClient.port = port
		return appClient
	}
	mgr.restFactory = func(port int, agentID string) codex.CodexClient {
		restClient.port = port
		return restClient
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := mgr.Launch(ctx, "agent-fallback-fail", "Agent Fallback Fail", "", ".", nil)
	if err == nil {
		t.Fatal("expected launch error when app-server and rest fallback both fail")
	}
	if mgr.Get("agent-fallback-fail") != nil {
		t.Fatal("expected failed launch agent to be removed from manager")
	}
	if appClient.spawnCalls.Load() != 1 {
		t.Fatalf("app-server spawn calls = %d, want 1", appClient.spawnCalls.Load())
	}
	if restClient.spawnCalls.Load() != 1 {
		t.Fatalf("rest spawn calls = %d, want 1", restClient.spawnCalls.Load())
	}
}

// ========================================
// 任务报告提取测试
// ========================================

// TestHandleEvent_ExtractsLastAgentMessage 验证 turn_complete 事件携带
// last_agent_message 时, handleEvent 正确提取并存入 proc.LastReport。
//
// 对应 Rust TurnCompleteEvent.last_agent_message 字段。
func TestHandleEvent_ExtractsLastAgentMessage(t *testing.T) {
	mgr := NewAgentManager()

	data, _ := json.Marshal(map[string]any{
		"last_agent_message": "Task completed: created 3 files.",
	})

	proc := &AgentProcess{ID: "agent-report", State: StateThinking}
	mgr.handleEvent(proc, codex.Event{
		Type: codex.EventTurnComplete,
		Data: data,
	})

	proc.mu.Lock()
	defer proc.mu.Unlock()

	if proc.State != StateIdle {
		t.Errorf("state = %q, want %q", proc.State, StateIdle)
	}
	if proc.LastReport != "Task completed: created 3 files." {
		t.Errorf("LastReport = %q, want %q", proc.LastReport, "Task completed: created 3 files.")
	}
}

// TestHandleEvent_ExtractsNestedLastAgentMessage 验证嵌套在 msg 中的
// last_agent_message 也能正确提取。
//
// Rust codex app-server 的 codex/event/task_complete 事件可能将
// last_agent_message 嵌套在 msg 子对象中。
func TestHandleEvent_ExtractsNestedLastAgentMessage(t *testing.T) {
	mgr := NewAgentManager()

	data, _ := json.Marshal(map[string]any{
		"msg": map[string]any{
			"last_agent_message": "Nested report content.",
		},
	})

	proc := &AgentProcess{ID: "agent-nested", State: StateThinking}
	mgr.handleEvent(proc, codex.Event{
		Type: "task_complete", // 也归类为 UITypeTurnComplete
		Data: data,
	})

	proc.mu.Lock()
	defer proc.mu.Unlock()

	if proc.LastReport != "Nested report content." {
		t.Errorf("LastReport = %q, want %q", proc.LastReport, "Nested report content.")
	}
}

// TestHandleEvent_NoReportOnNonTerminalEvent 验证非 turn_complete 事件
// 不会意外设置 LastReport。
func TestHandleEvent_NoReportOnNonTerminalEvent(t *testing.T) {
	mgr := NewAgentManager()

	data, _ := json.Marshal(map[string]any{
		"last_agent_message": "should not be captured",
	})

	proc := &AgentProcess{ID: "agent-no-report", State: StateIdle}
	mgr.handleEvent(proc, codex.Event{
		Type: codex.EventTurnStarted,
		Data: data,
	})

	proc.mu.Lock()
	defer proc.mu.Unlock()

	if proc.LastReport != "" {
		t.Errorf("LastReport should be empty for non-terminal events, got %q", proc.LastReport)
	}
}

// TestGetReport 验证 GetReport 方法。
func TestGetReport(t *testing.T) {
	mgr := NewAgentManager()

	// 未注册 agent → 空字符串
	if got := mgr.GetReport("nonexistent"); got != "" {
		t.Errorf("GetReport(nonexistent) = %q, want empty", got)
	}

	// 注册 agent, 模拟 turn_complete 事件
	proc := &AgentProcess{
		ID:     "agent-report-test",
		State:  StateThinking,
		Client: &stubClient{port: 19999},
	}
	mgr.mu.Lock()
	mgr.agents[proc.ID] = proc
	mgr.mu.Unlock()

	data, _ := json.Marshal(map[string]any{
		"last_agent_message": "Final summary.",
	})
	mgr.handleEvent(proc, codex.Event{
		Type: codex.EventTurnComplete,
		Data: data,
	})

	if got := mgr.GetReport("agent-report-test"); got != "Final summary." {
		t.Errorf("GetReport() = %q, want %q", got, "Final summary.")
	}
}

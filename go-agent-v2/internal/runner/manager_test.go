// manager_test.go — handleEvent 状态映射测试 (TDD RED→GREEN)。
package runner

import (
	"sync"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

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

// TestEventStateMap_Completeness 验证 eventStateMap 包含所有预期映射。
func TestEventStateMap_Completeness(t *testing.T) {
	// 确保 map 至少覆盖 6 个核心事件
	expected := map[string]AgentState{
		codex.EventTurnStarted:      StateThinking,
		codex.EventIdle:             StateIdle,
		codex.EventTurnComplete:     StateIdle,
		codex.EventExecCommandBegin: StateRunning,
		codex.EventError:            StateError,
		codex.EventShutdownComplete: StateStopped,
	}

	for eventType, wantState := range expected {
		got, ok := eventStateMap[eventType]
		if !ok {
			t.Errorf("eventStateMap missing key %q", eventType)
			continue
		}
		if got != wantState {
			t.Errorf("eventStateMap[%q] = %q, want %q", eventType, got, wantState)
		}
	}
}

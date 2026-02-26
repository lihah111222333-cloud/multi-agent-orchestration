package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
)

func TestPublishAgentEventSubtopicRouting(t *testing.T) {
	bus := NewMessageBus()
	sub := bus.Subscribe("router-events", "agent.a0")
	router := NewAgentRouter(bus, stubDiscoverer{})

	cases := []struct {
		name        string
		eventType   string
		wantSubtopic string
	}{
		{name: "output", eventType: agentcore.EventAgentMessageDelta, wantSubtopic: "output"},
		{name: "exec", eventType: agentcore.EventExecCommandBegin, wantSubtopic: "exec"},
		{name: "error", eventType: agentcore.EventError, wantSubtopic: "error"},
		{name: "lifecycle", eventType: agentcore.EventTurnComplete, wantSubtopic: "lifecycle"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router.PublishAgentEvent("a0", AgentEvent{Type: tc.eventType})

			msg := mustReadBusMessage(t, sub.Ch)
			wantTopic := fmt.Sprintf("agent.a0.%s", tc.wantSubtopic)
			if msg.Topic != wantTopic {
				t.Fatalf("topic = %q, want %q", msg.Topic, wantTopic)
			}
			if msg.Type != MsgAgentEvent {
				t.Fatalf("type = %q, want %q", msg.Type, MsgAgentEvent)
			}

			var got AgentEvent
			if err := json.Unmarshal(msg.Payload, &got); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if got.Type != tc.eventType {
				t.Fatalf("event type = %q, want %q", got.Type, tc.eventType)
			}
		})
	}
}

func TestGetOrCreateClientReusesRunningClient(t *testing.T) {
	runningClient := &stubAgentClient{running: true}
	factoryCalls := 0

	router := NewAgentRouter(NewMessageBus(), stubDiscoverer{}, func(threadID string, port int) AgentClient {
		factoryCalls++
		return runningClient
	})

	first, err := router.getOrCreateClient("a1", 7001)
	if err != nil {
		t.Fatalf("first getOrCreateClient error: %v", err)
	}
	second, err := router.getOrCreateClient("a1", 7001)
	if err != nil {
		t.Fatalf("second getOrCreateClient error: %v", err)
	}

	if factoryCalls != 1 {
		t.Fatalf("factory calls = %d, want 1", factoryCalls)
	}
	if first != second {
		t.Fatal("expected cached running client to be reused")
	}
}

func TestCleanupStaleRemovesNonRunningClients(t *testing.T) {
	created := map[string]*stubAgentClient{}
	router := NewAgentRouter(NewMessageBus(), stubDiscoverer{}, func(threadID string, port int) AgentClient {
		client := &stubAgentClient{running: true}
		created[threadID] = client
		return client
	})

	if _, err := router.getOrCreateClient("alive", 7101); err != nil {
		t.Fatalf("create alive client: %v", err)
	}
	if _, err := router.getOrCreateClient("stale", 7102); err != nil {
		t.Fatalf("create stale client: %v", err)
	}
	created["stale"].running = false

	router.CleanupStale()

	router.mu.RLock()
	_, hasAlive := router.clients["alive"]
	_, hasStale := router.clients["stale"]
	router.mu.RUnlock()

	if !hasAlive {
		t.Fatal("alive client should remain cached")
	}
	if hasStale {
		t.Fatal("stale client should be removed")
	}
}

func mustReadBusMessage(t *testing.T, ch <-chan Message) Message {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for bus message")
		return Message{}
	}
}

type stubDiscoverer struct {
	endpoints []AgentEndpoint
	err       error
}

func (d stubDiscoverer) ListRunning(context.Context) ([]AgentEndpoint, error) {
	if d.err != nil {
		return nil, d.err
	}
	out := make([]AgentEndpoint, len(d.endpoints))
	copy(out, d.endpoints)
	return out, nil
}

type stubAgentClient struct {
	running    bool
	submitErr  error
	submitCall int
}

func (c *stubAgentClient) Submit(prompt string, images, files []string, outputSchema json.RawMessage) error {
	c.submitCall++
	return c.submitErr
}

func (c *stubAgentClient) Running() bool {
	return c.running
}

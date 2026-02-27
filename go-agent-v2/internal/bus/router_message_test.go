package bus

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBroadcastEmitsUserMessageEvents(t *testing.T) {
	bus := NewMessageBus()
	// Subscribe to "agent" prefix to capture all agent.*.input topics.
	// matchTopic("agent", "agent.sub-a.input") matches via "agent"+"." prefix.
	sub := bus.Subscribe("broadcast-test", "agent")

	endpoints := []AgentEndpoint{
		{ThreadID: "main", Port: 7001},
		{ThreadID: "sub-a", Port: 7002},
		{ThreadID: "sub-b", Port: 7003},
	}

	clients := map[string]*stubAgentClient{}
	router := NewAgentRouter(bus, stubDiscoverer{endpoints: endpoints}, func(threadID string, port int) AgentClient {
		c := &stubAgentClient{running: true}
		clients[threadID] = c
		return c
	})

	if err := router.Broadcast(context.Background(), "main", "hello all"); err != nil {
		t.Fatalf("Broadcast() error = %v", err)
	}

	// sub-a and sub-b should each receive a submit call
	if clients["sub-a"] == nil || clients["sub-a"].submitCall != 1 {
		t.Fatalf("sub-a submit calls = %d, want 1", clients["sub-a"].submitCall)
	}
	if clients["sub-b"] == nil || clients["sub-b"].submitCall != 1 {
		t.Fatalf("sub-b submit calls = %d, want 1", clients["sub-b"].submitCall)
	}

	// Should have published 2 user_message events (one per sub-agent).
	userMsgCount := 0
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-sub.Ch:
			if msg.Type == MsgUserMessage {
				userMsgCount++
			}
			if userMsgCount >= 2 {
				return // success
			}
		case <-timeout:
			t.Fatalf("timed out waiting for user_message events, got %d, want 2", userMsgCount)
			return
		}
	}
}

func TestBroadcastCollectsErrors(t *testing.T) {
	endpoints := []AgentEndpoint{
		{ThreadID: "main", Port: 7001},
		{ThreadID: "sub-fail", Port: 7002},
	}

	router := NewAgentRouter(NewMessageBus(), stubDiscoverer{endpoints: endpoints}, func(threadID string, port int) AgentClient {
		return &stubAgentClient{running: true, submitErr: errors.New("submit failed")}
	})

	err := router.Broadcast(context.Background(), "main", "test")
	if err == nil {
		t.Fatal("Broadcast() should return error when submit fails")
	}
}

func TestSendToAgentEmitsUserMessageEvent(t *testing.T) {
	bus := NewMessageBus()
	sub := bus.Subscribe("send-test", "agent.sub-1")

	endpoints := []AgentEndpoint{
		{ThreadID: "sub-1", Port: 7001},
	}

	router := NewAgentRouter(bus, stubDiscoverer{endpoints: endpoints}, func(threadID string, port int) AgentClient {
		return &stubAgentClient{running: true}
	})

	if err := router.SendToAgent(context.Background(), "sub-1", "hello"); err != nil {
		t.Fatalf("SendToAgent() error = %v", err)
	}

	msg := mustReadBusMessage(t, sub.Ch)
	if msg.Type != MsgUserMessage {
		t.Fatalf("type = %q, want %q", msg.Type, MsgUserMessage)
	}
}

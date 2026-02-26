package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/discovery"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// AgentDiscoverer is the discovery contract used by the router.
type AgentDiscoverer = discovery.Discoverer

// AgentEndpoint is the discovered running agent endpoint.
type AgentEndpoint = discovery.RunningAgent

// AgentRouter routes messages between agents discovered from agent_threads.
type AgentRouter struct {
	bus       *MessageBus
	discover  AgentDiscoverer
	mu        sync.RWMutex
	clients   map[string]AgentClient
	newClient AgentClientFactory
}

// NewAgentRouter creates a router with optional client factory injection.
func NewAgentRouter(bus *MessageBus, discover AgentDiscoverer, factories ...AgentClientFactory) *AgentRouter {
	var factory AgentClientFactory
	if len(factories) > 0 {
		factory = factories[0]
	}

	return &AgentRouter{
		bus:       bus,
		discover:  discover,
		clients:   make(map[string]AgentClient),
		newClient: factory,
	}
}

// DelegateTask routes one task from fromID to toThreadID.
func (r *AgentRouter) DelegateTask(ctx context.Context, fromID, toThreadID, prompt string, images, files []string) error {
	endpoints, err := r.discover.ListRunning(ctx)
	if err != nil {
		return apperrors.Wrap(err, "AgentRouter.DelegateTask", "discover agents")
	}

	var target *AgentEndpoint
	for i := range endpoints {
		if endpoints[i].ThreadID == toThreadID {
			target = &endpoints[i]
			break
		}
	}
	if target == nil {
		return apperrors.Newf("AgentRouter.DelegateTask", "agent %s not found or not running", toThreadID)
	}

	client, err := r.getOrCreateClient(target.ThreadID, target.Port)
	if err != nil {
		return apperrors.Wrapf(err, "AgentRouter.DelegateTask", "get client for %s (port %d)", toThreadID, target.Port)
	}
	if err := client.Submit(prompt, images, files, nil); err != nil {
		return apperrors.Wrapf(err, "AgentRouter.DelegateTask", "submit to %s (port %d)", toThreadID, target.Port)
	}

	payload := delegatePayload{
		Prompt: prompt,
		Images: images,
		Files:  files,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return apperrors.Wrap(err, "AgentRouter.DelegateTask", "marshal delegate payload")
	}

	if r.bus != nil {
		r.bus.Publish(Message{
			Topic:   fmt.Sprintf("agent.%s.input", toThreadID),
			From:    fromID,
			To:      toThreadID,
			Type:    MsgTaskDelegate,
			Payload: data,
		})
	}

	return nil
}

// SendToAgent sends one prompt to one target agent.
func (r *AgentRouter) SendToAgent(ctx context.Context, threadID, prompt string) error {
	endpoints, err := r.discover.ListRunning(ctx)
	if err != nil {
		return apperrors.Wrap(err, "AgentRouter.SendToAgent", "discover")
	}

	for _, ep := range endpoints {
		if ep.ThreadID == threadID {
			client, err := r.getOrCreateClient(ep.ThreadID, ep.Port)
			if err != nil {
				return apperrors.Wrapf(err, "AgentRouter.SendToAgent", "get client for %s (port %d)", ep.ThreadID, ep.Port)
			}
			return client.Submit(prompt, nil, nil, nil)
		}
	}
	return apperrors.Newf("AgentRouter.SendToAgent", "agent %s not found", threadID)
}

// Broadcast sends one prompt to all running agents except sender.
func (r *AgentRouter) Broadcast(ctx context.Context, fromID, prompt string) error {
	endpoints, err := r.discover.ListRunning(ctx)
	if err != nil {
		return apperrors.Wrap(err, "AgentRouter.Broadcast", "discover")
	}

	for _, ep := range endpoints {
		if ep.ThreadID == fromID {
			continue
		}

		client, err := r.getOrCreateClient(ep.ThreadID, ep.Port)
		if err != nil {
			continue
		}
		if err := client.Submit(prompt, nil, nil, nil); err != nil {
			continue
		}
	}
	return nil
}

// PublishAgentEvent publishes one normalized agent event to bus with subtopic routing.
func (r *AgentRouter) PublishAgentEvent(agentID string, event AgentEvent) {
	if r.bus == nil {
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	subtopic := "event"
	switch event.Type {
	case agentcore.EventAgentMessageDelta, agentcore.EventAgentMessage:
		subtopic = "output"
	case agentcore.EventExecCommandBegin, agentcore.EventExecCommandEnd, agentcore.EventExecCommandOutputDelta:
		subtopic = "exec"
	case agentcore.EventError:
		subtopic = "error"
	case agentcore.EventIdle, agentcore.EventTurnComplete:
		subtopic = "lifecycle"
	}

	r.bus.Publish(Message{
		Topic:   fmt.Sprintf("agent.%s.%s", agentID, subtopic),
		From:    agentID,
		To:      "*",
		Type:    MsgAgentEvent,
		Payload: data,
	})
}

// ListAgents lists running agents from discovery.
func (r *AgentRouter) ListAgents(ctx context.Context) ([]AgentEndpoint, error) {
	return r.discover.ListRunning(ctx)
}

// getOrCreateClient returns a running cached client or creates a new one via factory.
func (r *AgentRouter) getOrCreateClient(threadID string, port int) (AgentClient, error) {
	r.mu.RLock()
	c, ok := r.clients[threadID]
	r.mu.RUnlock()
	if ok && c != nil && c.Running() {
		return c, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[threadID]; ok && c != nil && c.Running() {
		return c, nil
	}

	if r.newClient == nil {
		return nil, apperrors.New("AgentRouter.getOrCreateClient", "agent client factory is nil")
	}

	client := r.newClient(threadID, port)
	if client == nil {
		return nil, apperrors.Newf("AgentRouter.getOrCreateClient", "factory returned nil client for %s:%d", threadID, port)
	}

	r.clients[threadID] = client
	return client, nil
}

// CleanupStale removes cached clients that are no longer running.
func (r *AgentRouter) CleanupStale() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, c := range r.clients {
		if c == nil || !c.Running() {
			delete(r.clients, id)
		}
	}
}

// delegatePayload is the task delegation payload.
type delegatePayload struct {
	Prompt string   `json:"prompt"`
	Images []string `json:"images,omitempty"`
	Files  []string `json:"files,omitempty"`
}

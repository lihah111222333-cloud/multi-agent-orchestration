// router.go — Agent 消息路由器 (基于 agent_threads 表的服务发现)。
//
// 简化架构:
//
//	agent_threads 表 = 服务注册表 (port/pid/status)
//	任何 Agent 查表 → 拿端口 → 直接 HTTP/WS 互通
//
// 路由流程:
//  1. DelegateTask(fromID, toID, prompt) → 查 agent_threads 获取 toID 端口
//  2. 构造 codex.Client(port) → Submit(prompt)
//  3. 事件通过 bus 广播给监听者
package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

// AgentDiscoverer 服务发现接口 (由 store.AgentThreadStore 实现)。
type AgentDiscoverer interface {
	FindByPort(ctx context.Context, port int) (threadID string, pid int, err error)
	ListRunning(ctx context.Context) ([]AgentEndpoint, error)
}

// AgentEndpoint 发现的 Agent 端点。
type AgentEndpoint struct {
	ThreadID string `json:"thread_id"`
	Port     int    `json:"port"`
	PID      int    `json:"pid"`
	Status   string `json:"status"`
}

// AgentRouter Agent 消息路由器 (基于 agent_threads 服务发现)。
type AgentRouter struct {
	bus        *MessageBus
	discover   AgentDiscoverer
	mu         sync.RWMutex
	clients    map[string]*codex.Client // threadID → 活跃连接缓存
	httpClient *http.Client
}

// NewAgentRouter 创建路由器。
func NewAgentRouter(bus *MessageBus, discover AgentDiscoverer) *AgentRouter {
	return &AgentRouter{
		bus:        bus,
		discover:   discover,
		clients:    make(map[string]*codex.Client),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// DelegateTask Agent A 委托任务给 Agent B (基于 agent_threads 查表路由)。
//
// 流程: 查 agent_threads → 获取端口 → POST /threads/:id/submit。
func (r *AgentRouter) DelegateTask(ctx context.Context, fromID, toThreadID, prompt string, images, files []string) error {
	// 查表获取目标 Agent 的端点
	endpoints, err := r.discover.ListRunning(ctx)
	if err != nil {
		return fmt.Errorf("router: discover agents: %w", err)
	}

	var target *AgentEndpoint
	for i := range endpoints {
		if endpoints[i].ThreadID == toThreadID {
			target = &endpoints[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("router: agent %s not found or not running", toThreadID)
	}

	// 直接 HTTP POST submit 到目标 Agent
	client := r.getOrCreateClient(target.ThreadID, target.Port)
	if err := client.Submit(prompt, images, files, nil); err != nil {
		return fmt.Errorf("router: submit to %s (port %d): %w", toThreadID, target.Port, err)
	}

	// 发布委托事件到总线 (通知监听者)
	payload := delegatePayload{
		Prompt: prompt,
		Images: images,
		Files:  files,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("router: marshal delegate payload: %w", err)
	}
	r.bus.Publish(Message{
		Topic:   fmt.Sprintf("agent.%s.input", toThreadID),
		From:    fromID,
		To:      toThreadID,
		Type:    MsgTaskDelegate,
		Payload: data,
	})

	return nil
}

// SendToAgent 直接向指定 Agent 发送消息。
func (r *AgentRouter) SendToAgent(ctx context.Context, threadID, prompt string) error {
	endpoints, err := r.discover.ListRunning(ctx)
	if err != nil {
		return fmt.Errorf("router: discover: %w", err)
	}

	for _, ep := range endpoints {
		if ep.ThreadID == threadID {
			client := r.getOrCreateClient(ep.ThreadID, ep.Port)
			return client.Submit(prompt, nil, nil, nil)
		}
	}
	return fmt.Errorf("router: agent %s not found", threadID)
}

// Broadcast 向所有运行中的 Agent 广播消息。
func (r *AgentRouter) Broadcast(ctx context.Context, fromID, prompt string) error {
	endpoints, err := r.discover.ListRunning(ctx)
	if err != nil {
		return fmt.Errorf("router: discover: %w", err)
	}

	var lastErr error
	for _, ep := range endpoints {
		if ep.ThreadID == fromID {
			continue // 不发给自己
		}
		client := r.getOrCreateClient(ep.ThreadID, ep.Port)
		if err := client.Submit(prompt, nil, nil, nil); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// PublishAgentEvent 将 Codex 事件发布到总线 (由 runner 事件回调调用)。
func (r *AgentRouter) PublishAgentEvent(agentID string, event codex.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return // marshal 失败时静默跳过 (避免崩溃事件循环)
	}

	subtopic := "event"
	switch event.Type {
	case codex.EventAgentMessageDelta, codex.EventAgentMessage:
		subtopic = "output"
	case codex.EventExecCommandBegin, codex.EventExecCommandEnd, codex.EventExecCommandOutputDelta:
		subtopic = "exec"
	case codex.EventError:
		subtopic = "error"
	case codex.EventIdle, codex.EventTurnComplete:
		subtopic = "lifecycle"
	}

	r.bus.Publish(Message{
		Topic:   fmt.Sprintf("agent.%s.%s", agentID, subtopic),
		From:    agentID,
		To:      TopicAll,
		Type:    MsgAgentEvent,
		Payload: data,
	})
}

// ListAgents 列出所有运行中的 Agent。
func (r *AgentRouter) ListAgents(ctx context.Context) ([]AgentEndpoint, error) {
	return r.discover.ListRunning(ctx)
}

// getOrCreateClient 获取或创建到目标 Agent 的客户端连接。
func (r *AgentRouter) getOrCreateClient(threadID string, port int) *codex.Client {
	r.mu.RLock()
	c, ok := r.clients[threadID]
	r.mu.RUnlock()
	if ok && c.Running() {
		return c
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// double-check
	if c, ok := r.clients[threadID]; ok && c.Running() {
		return c
	}

	client := codex.NewClient(port)
	client.ThreadID = threadID
	client.Transport = codex.TransportSSE // 跨 Agent 通信用 POST+SSE (无需维持 WS)
	r.clients[threadID] = client
	return client
}

// CleanupStale 清理已停止的 Agent 连接缓存。
func (r *AgentRouter) CleanupStale() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, c := range r.clients {
		if !c.Running() {
			delete(r.clients, id)
		}
	}
}

// delegatePayload 任务委托载荷。
type delegatePayload struct {
	Prompt string   `json:"prompt"`
	Images []string `json:"images,omitempty"`
	Files  []string `json:"files,omitempty"`
}

// Package runner 管理 Codex Agent 子进程生命周期。
//
// 每个 Agent = 一个 `codex app-server --listen ws://IP:PORT` 进程
//   - 一个线程 (thread/start JSON-RPC)
//   - 一条 WebSocket 连接 (JSON-RPC 2.0)
//
// 生命周期: Launch → (Submit/Command) → Stop。
package runner

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

// basePort 自动分配端口的起始值。
const basePort = 19836

// AgentState Agent 运行状态。
type AgentState string

const (
	// StateIdle Agent 空闲，等待输入。
	StateIdle AgentState = "idle"
	// StateThinking Agent 正在思考。
	StateThinking AgentState = "thinking"
	// StateRunning Agent 正在执行命令。
	StateRunning AgentState = "running"
	// StateStopped Agent 已停止。
	StateStopped AgentState = "stopped"
	// StateError Agent 遇到错误。
	StateError AgentState = "error"
)

// AgentProcess 单个 Codex Agent 实例。
type AgentProcess struct {
	ID     string            // 唯一标识
	Name   string            // 显示名称
	Client codex.CodexClient // Codex API 客户端 (支持 http-api 或 app-server)
	State  AgentState        // 当前状态
	mu     sync.Mutex
}

// AgentInfo Agent 信息快照 (线程安全复制)。
type AgentInfo struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Port     int        `json:"port"`
	ThreadID string     `json:"thread_id"`
	State    AgentState `json:"state"`
}

// AgentEvent 封装 Agent 事件 (用于 UI 展示)。
type AgentEvent struct {
	AgentID string      `json:"agent_id"`
	Event   codex.Event `json:"event"`
}

// AgentMessage 兼容旧 WebSocket 消息格式。
type AgentMessage struct {
	Type    string `json:"type"` // "output" | "input" | "status"
	AgentID string `json:"agent_id"`
	Data    string `json:"data"`
	Ts      string `json:"ts"`
}

// EventHandler Agent 事件回调。
type EventHandler func(agentID string, event codex.Event)

// AgentManager 管理多个 Codex Agent 子进程。
type AgentManager struct {
	mu       sync.RWMutex
	agents   map[string]*AgentProcess
	nextPort atomic.Int32
	onEvent  EventHandler
}

// NewAgentManager 创建管理器。
func NewAgentManager() *AgentManager {
	m := &AgentManager{
		agents: make(map[string]*AgentProcess),
	}
	m.nextPort.Store(int32(basePort))
	return m
}

// SetOnEvent 设置事件回调 (线程安全)。
func (m *AgentManager) SetOnEvent(fn EventHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEvent = fn
}

// SetOnOutput 设置输出回调 (兼容旧 API, 将 agent_message_delta 转为 []byte)。
func (m *AgentManager) SetOnOutput(fn func(agentID string, data []byte)) {
	m.SetOnEvent(func(agentID string, event codex.Event) {
		if event.Type == codex.EventAgentMessageDelta || event.Type == codex.EventExecCommandOutputDelta {
			fn(agentID, event.Data)
		}
	})
}

// Launch 启动一个 Codex Agent。
//
// 流程: 分配端口 → spawn codex app-server → JSON-RPC initialize → thread/start。
// ctx 控制 spawn 超时和子进程生命周期。
// dynamicTools 为 nil 时不注入自定义工具。
func (m *AgentManager) Launch(ctx context.Context, id, name, prompt, cwd string, dynamicTools []codex.DynamicTool) error {
	m.mu.Lock()
	if _, exists := m.agents[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("runner: agent %s already exists", id)
	}

	port := int(m.nextPort.Add(1) - 1) // 每个 codex app-server 占 1 个端口

	// 使用 AppServerClient (JSON-RPC) 支持 dynamicTools
	client := codex.NewAppServerClient(port)

	proc := &AgentProcess{
		ID:     id,
		Name:   name,
		Client: client,
		State:  StateRunning,
	}
	m.agents[id] = proc
	m.mu.Unlock()

	// 注册事件回调 — 更新 Agent 状态 + 转发给 UI
	client.SetEventHandler(func(event codex.Event) {
		m.handleEvent(proc, event)
	})

	// SpawnAndConnect: 启动 app-server → WS 连接 → initialize → thread/start (with dynamicTools)
	if err := client.SpawnAndConnect(ctx, prompt, cwd, "", dynamicTools); err != nil {
		proc.mu.Lock()
		proc.State = StateError
		proc.mu.Unlock()
		return fmt.Errorf("runner: launch %s: %w", id, err)
	}

	return nil
}

// handleEvent 处理 Codex 事件 — 更新 Agent 状态并转发给 UI。
func (m *AgentManager) handleEvent(proc *AgentProcess, event codex.Event) {
	proc.mu.Lock()
	switch event.Type {
	case codex.EventTurnStarted:
		proc.State = StateThinking
	case codex.EventIdle, codex.EventTurnComplete:
		proc.State = StateIdle
	case codex.EventExecCommandBegin:
		proc.State = StateRunning
	case codex.EventError:
		proc.State = StateError
	case codex.EventShutdownComplete:
		proc.State = StateStopped
	}
	proc.mu.Unlock()

	m.mu.RLock()
	handler := m.onEvent
	m.mu.RUnlock()
	if handler != nil {
		handler(proc.ID, event)
	}
}

// Submit 向 Agent 发送对话消息 (支持图片 + 文件)。
func (m *AgentManager) Submit(id, prompt string, images, files []string) error {
	proc, err := m.get(id)
	if err != nil {
		return err
	}
	return proc.Client.Submit(prompt, images, files, nil)
}

// SendCommand 向 Agent 发送斜杠命令。
func (m *AgentManager) SendCommand(id, cmd, args string) error {
	proc, err := m.get(id)
	if err != nil {
		return err
	}
	return proc.Client.SendCommand(cmd, args)
}

// SendInput 向 Agent 发送输入 (兼容旧接口, 转为 Submit)。
func (m *AgentManager) SendInput(id string, data []byte) error {
	return m.Submit(id, string(data), nil, nil)
}

// Stop 停止指定 Agent。
func (m *AgentManager) Stop(id string) error {
	m.mu.Lock()
	proc, ok := m.agents[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("runner: agent %s not found", id)
	}
	delete(m.agents, id)
	m.mu.Unlock()

	if err := proc.Client.Shutdown(); err != nil {
		return fmt.Errorf("runner: stop %s: %w", id, err)
	}

	proc.mu.Lock()
	proc.State = StateStopped
	proc.mu.Unlock()
	return nil
}

// StopAll 停止所有 Agent。
func (m *AgentManager) StopAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		_ = m.Stop(id)
	}
}

// List 返回所有 Agent 信息快照。
func (m *AgentManager) List() []AgentInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]AgentInfo, 0, len(m.agents))
	for _, proc := range m.agents {
		proc.mu.Lock()
		info := AgentInfo{
			ID:       proc.ID,
			Name:     proc.Name,
			Port:     proc.Client.GetPort(),
			ThreadID: proc.Client.GetThreadID(),
			State:    proc.State,
		}
		proc.mu.Unlock()
		infos = append(infos, info)
	}
	return infos
}

// Get 获取 Agent 进程 (公开接口)。nil 表示不存在。
func (m *AgentManager) Get(id string) *AgentProcess {
	m.mu.RLock()
	proc := m.agents[id]
	m.mu.RUnlock()
	return proc
}

// get 获取 Agent 进程 (线程安全, 返回 error)。
func (m *AgentManager) get(id string) (*AgentProcess, error) {
	m.mu.RLock()
	proc, ok := m.agents[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("runner: agent %s not found", id)
	}
	return proc, nil
}

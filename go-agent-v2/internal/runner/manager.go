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
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
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
	mu     sync.Mutex        // 保护 State 字段读写
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

type clientFactory func(port int, agentID string) codex.CodexClient

// AgentManager 管理多个 Codex Agent 子进程。
type AgentManager struct {
	// ========================================
	// 锁层次 (Lock Hierarchy)
	// ========================================
	// 获取顺序: mu < AgentProcess.mu
	// mu 保护 agents map + onEvent, AgentProcess.mu 保护单个进程状态。
	// NEVER 在持有 AgentProcess.mu 时获取 mu 的写锁。
	// ========================================

	mu       sync.RWMutex
	agents   map[string]*AgentProcess
	nextPort atomic.Int32
	onEvent  EventHandler

	// 传输构造器 (便于测试注入 + fallback)
	appServerFactory clientFactory
	restFactory      clientFactory
}

// NewAgentManager 创建管理器。
func NewAgentManager() *AgentManager {
	m := &AgentManager{
		agents:           make(map[string]*AgentProcess),
		appServerFactory: func(port int, agentID string) codex.CodexClient { return codex.NewAppServerClient(port, agentID) },
		restFactory:      func(port int, agentID string) codex.CodexClient { return codex.NewClient(port, agentID) },
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

// maxPortRetries 最多尝试的连续端口数 (防止耗尽)。
const maxPortRetries = 20

// findFreePort 从 nextPort 开始探测, 跳过被占用端口, 返回可用端口。
//
// 每次探测: net.Listen → Close。最多尝试 maxPortRetries 个端口。
func (m *AgentManager) findFreePort() (int, error) {
	for i := 0; i < maxPortRetries; i++ {
		port := int(m.nextPort.Add(1) - 1)
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue // 端口被占用，跳到下一个
		}
		_ = ln.Close()
		return port, nil
	}

	// 回退策略: 使用内核分配的随机可用端口 (127.0.0.1:0)。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		if port > 0 {
			m.nextPort.Store(int32(port + 1))
			return port, nil
		}
	}

	return 0, apperrors.Newf("AgentManager.findFreePort", "no free port found after %d attempts from %d, and fallback random port failed",
		maxPortRetries, int(m.nextPort.Load())-maxPortRetries)
}

// Launch 启动一个 Codex Agent。
//
// 流程: 探测空闲端口 → spawn codex app-server → JSON-RPC initialize → thread/start。
// ctx 控制 spawn 超时和子进程生命周期。
// dynamicTools 为 nil 时不注入自定义工具。
func (m *AgentManager) Launch(ctx context.Context, id, name, prompt, cwd string, dynamicTools []codex.DynamicTool) error {
	logger.Infow("runner: launching agent",
		logger.FieldAgentID, id,
		logger.FieldName, name,
		logger.FieldCwd, cwd,
	)

	m.mu.Lock()
	if _, exists := m.agents[id]; exists {
		m.mu.Unlock()
		return apperrors.Newf("AgentManager.Launch", "agent %s already exists", id)
	}

	port, err := m.findFreePort()
	if err != nil {
		m.mu.Unlock()
		logger.Error("runner: no free port", logger.FieldAgentID, id, logger.FieldError, err)
		return err
	}

	// 优先使用 AppServerClient (JSON-RPC, 支持实时事件 + dynamicTools)。
	client := m.appServerFactory(port, id)
	if client == nil {
		m.mu.Unlock()
		return apperrors.New("AgentManager.Launch", "app-server client factory returned nil")
	}

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
		logger.Warn("runner: app-server launch failed, attempting REST fallback",
			logger.FieldAgentID, id,
			logger.FieldPort, port,
			logger.FieldError, err,
		)
		_ = client.Kill()

		fallback := m.restFactory(port, id)
		if fallback != nil {
			proc.mu.Lock()
			proc.Client = fallback
			proc.mu.Unlock()
			fallback.SetEventHandler(func(event codex.Event) {
				m.handleEvent(proc, event)
			})
			if fallbackErr := fallback.SpawnAndConnect(ctx, prompt, cwd, "", dynamicTools); fallbackErr == nil {
				payload, _ := json.Marshal(map[string]any{
					"message": "App-server unavailable; using HTTP fallback",
					"status":  "degraded",
					"active":  false,
					"done":    true,
					"phase":   "transport_fallback",
				})
				m.handleEvent(proc, codex.Event{
					Type: codex.EventBackgroundEvent,
					Data: payload,
				})
				logger.Warn("runner: launched with REST fallback",
					logger.FieldAgentID, id,
					logger.FieldPort, port,
				)
				return nil
			} else {
				logger.Error("runner: REST fallback launch failed",
					logger.FieldAgentID, id,
					logger.FieldPort, port,
					logger.FieldError, fallbackErr,
				)
				err = apperrors.Wrapf(fallbackErr, "AgentManager.Launch", "fallback launch %s after app-server failure: %v", id, err)
			}
		} else {
			err = apperrors.Wrap(err, "AgentManager.Launch", "app-server launch failed and REST fallback unavailable")
		}

		proc.mu.Lock()
		proc.State = StateError
		proc.mu.Unlock()

		// 启动失败时移除残留 agent，避免 list_agents 返回 error 态幽灵实例。
		m.mu.Lock()
		if existing, ok := m.agents[id]; ok && existing == proc {
			delete(m.agents, id)
		}
		m.mu.Unlock()
		logger.Error("runner: launch failed", logger.FieldAgentID, id, logger.FieldPort, port, logger.FieldError, err)
		return apperrors.Wrapf(err, "AgentManager.Launch", "launch %s", id)
	}

	logger.Infow("runner: agent launched", logger.FieldAgentID, id, logger.FieldPort, port)
	return nil
}

// handleEvent 处理 Codex 事件 — 更新 Agent 状态并转发给 UI。
func (m *AgentManager) handleEvent(proc *AgentProcess, event codex.Event) {
	// 归一化事件以确定状态
	normalized := uistate.NormalizeEvent(event.Type, "", event.Data)

	var newState AgentState
	switch normalized.UIType {
	case uistate.UITypeAssistantDelta,
		uistate.UITypeAssistantDone,
		uistate.UITypeReasoningDelta,
		uistate.UITypePlanDelta,
		uistate.UITypeTurnStarted,
		uistate.UITypeUserMessage:
		newState = StateThinking
	case uistate.UITypeCommandStart,
		uistate.UITypeCommandOutput,
		uistate.UITypeCommandDone,
		uistate.UITypeFileEditStart,
		uistate.UITypeFileEditDone,
		uistate.UITypeToolCall,
		uistate.UITypeApprovalRequest:
		newState = StateRunning
	case uistate.UITypeTurnComplete, uistate.UITypeDiffUpdate:
		newState = StateIdle
	case uistate.UITypeError:
		newState = StateError
	case uistate.UITypeSystem:
		switch normalized.RawType {
		case codex.EventCollabAgentSpawnBegin,
			codex.EventCollabAgentInteractionBegin,
			codex.EventCollabWaitingBegin,
			codex.EventCollabAgentSpawnEnd,
			codex.EventCollabAgentInteractionEnd,
			codex.EventCollabWaitingEnd:
			newState = StateRunning
		}
	}

	// 特殊状态处理
	if event.Type == codex.EventShutdownComplete {
		newState = StateStopped
	}

	if newState != "" {
		proc.mu.Lock()
		if proc.State != newState {
			logger.Debug("runner: state transition",
				logger.FieldAgentID, proc.ID,
				logger.FieldEventType, event.Type,
				logger.FieldState, string(newState),
			)
			proc.State = newState
		}
		proc.mu.Unlock()
	}

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
	logger.Infow("runner: stopping agent", logger.FieldAgentID, id)

	m.mu.Lock()
	proc, ok := m.agents[id]
	if !ok {
		m.mu.Unlock()
		return apperrors.Newf("AgentManager.Stop", "agent %s not found", id)
	}
	delete(m.agents, id)
	m.mu.Unlock()

	if err := proc.Client.Shutdown(); err != nil {
		logger.Warn("runner: shutdown error", logger.FieldAgentID, id, logger.FieldError, err)
		return apperrors.Wrapf(err, "AgentManager.Stop", "stop %s", id)
	}

	proc.mu.Lock()
	proc.State = StateStopped
	proc.mu.Unlock()
	logger.Infow("runner: agent stopped", logger.FieldAgentID, id)
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

	logger.Infow("runner: stopping all agents", logger.FieldCount, len(ids))
	for _, id := range ids {
		if err := m.Stop(id); err != nil {
			logger.Warn("runner: stop agent failed during StopAll", logger.FieldAgentID, id, logger.FieldError, err)
		}
	}
}

// List 返回所有 Agent 信息快照。
//
// 使用 snapshot-then-lock 模式:
//   - 先持 mu.RLock 快照 agents slice, 立即释放
//   - 再逐个持 proc.mu 读取状态
//
// 这样避免在持有 mu 的同时获取 proc.mu, 缩小持锁范围。
func (m *AgentManager) List() []AgentInfo {
	m.mu.RLock()
	snapshot := make([]*AgentProcess, 0, len(m.agents))
	for _, proc := range m.agents {
		snapshot = append(snapshot, proc)
	}
	m.mu.RUnlock()

	infos := make([]AgentInfo, 0, len(snapshot))
	for _, proc := range snapshot {
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
	sort.SliceStable(infos, func(i, j int) bool {
		leftID := strings.TrimSpace(infos[i].ID)
		rightID := strings.TrimSpace(infos[j].ID)
		if leftID != rightID {
			return leftID > rightID
		}
		leftName := strings.TrimSpace(infos[i].Name)
		rightName := strings.TrimSpace(infos[j].Name)
		if leftName != rightName {
			return leftName > rightName
		}
		return infos[i].Port > infos[j].Port
	})
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
		return nil, apperrors.Newf("AgentManager.get", "agent %s not found", id)
	}
	return proc, nil
}

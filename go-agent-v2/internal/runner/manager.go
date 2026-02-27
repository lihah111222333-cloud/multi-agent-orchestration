package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const basePort = 19836

type AgentState string

const (
	StateIdle     AgentState = "idle"
	StateThinking AgentState = "thinking"
	StateRunning  AgentState = "running"
	StateStopped  AgentState = "stopped"
	StateError    AgentState = "error"
)

type AgentProcess struct {
	ID          string
	Name        string
	Client      agentcore.Client
	State       AgentState
	LastReport  string
	LastMessage string
	messageBuf  strings.Builder
	mu          sync.Mutex
}

func (p *AgentProcess) IsAlive() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.State != StateError && p.State != StateStopped
}

func (p *AgentProcess) Port() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	client := p.Client
	p.mu.Unlock()
	if client == nil {
		return 0
	}
	return client.GetPort()
}

type AgentInfo struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Port       int        `json:"port"`
	ThreadID   string     `json:"thread_id"`
	State      AgentState `json:"state"`
	LastReport string     `json:"last_report,omitempty"` // 最近一次任务报告
}

type AgentEvent struct {
	AgentID string          `json:"agent_id"`
	Event   agentcore.Event `json:"event"`
}

type AgentMessage struct {
	Type    string `json:"type"`
	AgentID string `json:"agent_id"`
	Data    string `json:"data"`
	Ts      string `json:"ts"`
}

type EventHandler func(agentID string, event agentcore.Event)

type AgentManager struct {
	mu       sync.RWMutex
	agents   map[string]*AgentProcess
	nextPort atomic.Int32
	onEvent  EventHandler

	appServerFactory agentcore.ClientFactory
	restFactory      agentcore.ClientFactory
}

func NewAgentManager(appFactory, restFactory agentcore.ClientFactory) (*AgentManager, error) {
	if appFactory == nil {
		return nil, apperrors.New("AgentManager.NewAgentManager", "appFactory must not be nil")
	}
	if restFactory == nil {
		return nil, apperrors.New("AgentManager.NewAgentManager", "restFactory must not be nil")
	}

	m := &AgentManager{
		agents:           make(map[string]*AgentProcess),
		appServerFactory: appFactory,
		restFactory:      restFactory,
	}
	m.nextPort.Store(int32(basePort))
	return m, nil
}

func (m *AgentManager) SetClientFactories(appFactory, restFactory agentcore.ClientFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if appFactory != nil {
		m.appServerFactory = appFactory
	}
	if restFactory != nil {
		m.restFactory = restFactory
	}
}

func (m *AgentManager) SetOnEvent(fn EventHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEvent = fn
}

func (m *AgentManager) SetOnOutput(fn func(agentID string, data []byte)) {
	m.SetOnEvent(func(agentID string, event agentcore.Event) {
		if event.Type == agentcore.EventAgentMessageDelta || event.Type == agentcore.EventExecCommandOutputDelta {
			fn(agentID, event.Data)
		}
	})
}

const maxPortRetries = 200

func (m *AgentManager) findFreePort() (int, error) {
	for range maxPortRetries {
		port := int(m.nextPort.Add(1) - 1)
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue // 端口被占用，跳到下一个
		}
		_ = ln.Close()
		return port, nil
	}

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

func (m *AgentManager) Launch(ctx context.Context, id, name, prompt, cwd string, instructions string, dynamicTools []agentcore.DynamicTool) error {
	logger.Info("runner: launching agent",
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

	client := m.appServerFactory(port, id)
	if client == nil {
		m.mu.Unlock()
		return apperrors.New("AgentManager.Launch", "app-server client factory returned nil")
	}

	if len(m.agents) > 0 {
		type approvalPolicySetter interface{ SetApprovalPolicy(string) }
		if setter, ok := client.(approvalPolicySetter); ok {
			setter.SetApprovalPolicy("never")
			logger.Info("runner: sub-agent approval policy set to never",
				logger.FieldAgentID, id,
				"agent_count_before", len(m.agents),
			)
		}
	}

	proc := &AgentProcess{
		ID:     id,
		Name:   name,
		Client: client,
		State:  StateRunning,
	}
	m.agents[id] = proc
	m.mu.Unlock()

	client.SetEventHandler(func(event agentcore.Event) {
		m.handleEvent(proc, event)
	})

	if err := client.SpawnAndConnect(ctx, prompt, cwd, "", instructions, dynamicTools); err != nil {
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
			fallback.SetEventHandler(func(event agentcore.Event) {
				m.handleEvent(proc, event)
			})
			if fallbackErr := fallback.SpawnAndConnect(ctx, prompt, cwd, "", instructions, dynamicTools); fallbackErr == nil {
				event, marshalErr := buildJSONEvent(
					agentcore.EventBackgroundEvent,
					map[string]any{
						"message": "App-server unavailable; using HTTP fallback",
						"status":  "degraded",
						"active":  false,
						"done":    true,
						"phase":   "transport_fallback",
					},
				)
				if marshalErr != nil {
					logger.Warn("runner: fallback event marshal failed", logger.FieldAgentID, id, logger.FieldError, marshalErr)
				}
				m.handleEvent(proc, event)
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

		m.mu.Lock()
		if existing, ok := m.agents[id]; ok && existing == proc {
			delete(m.agents, id)
		}
		m.mu.Unlock()
		logger.Error("runner: launch failed", logger.FieldAgentID, id, logger.FieldPort, port, logger.FieldError, err, logger.FieldDecision, "removed_from_agents_map")
		return apperrors.Wrapf(err, "AgentManager.Launch", "launch %s", id)
	}

	logger.Info("runner: agent launched", logger.FieldAgentID, id, logger.FieldPort, port)
	return nil
}

func (m *AgentManager) handleEvent(proc *AgentProcess, event agentcore.Event) {
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
		case agentcore.EventCollabAgentSpawnBegin,
			agentcore.EventCollabAgentInteractionBegin,
			agentcore.EventCollabWaitingBegin,
			agentcore.EventCollabAgentSpawnEnd,
			agentcore.EventCollabAgentInteractionEnd,
			agentcore.EventCollabWaitingEnd:
			newState = StateRunning
		}
	}

	switch event.Type {
	case agentcore.EventShutdownComplete:
		newState = StateStopped
	case agentcore.EventConnectionDead:
		newState = StateError
	}

	if newState != "" {
		proc.mu.Lock()
		if proc.State != newState {
			logger.Info("runner: state transition",
				logger.FieldAgentID, proc.ID,
				logger.FieldEventType, event.Type,
				"prev_state", string(proc.State),
				logger.FieldState, string(newState),
			)
			proc.State = newState
		}
		proc.mu.Unlock()
	}

	switch normalized.UIType {
	case uistate.UITypeTurnStarted:
		proc.mu.Lock()
		proc.messageBuf.Reset()
		proc.mu.Unlock()
	case uistate.UITypeAssistantDelta:
		if normalized.Text != "" {
			proc.mu.Lock()
			proc.messageBuf.WriteString(normalized.Text)
			proc.mu.Unlock()
		}
	case uistate.UITypeTurnComplete:
		proc.mu.Lock()
		if proc.messageBuf.Len() > 0 {
			proc.LastMessage = proc.messageBuf.String()
			proc.messageBuf.Reset()
		}
		proc.mu.Unlock()
	}

	if normalized.UIType == uistate.UITypeTurnComplete {
		if report := extractLastAgentMessage(event.Data); report != "" {
			proc.mu.Lock()
			proc.LastReport = report
			proc.mu.Unlock()
			logger.Info("runner: captured task report",
				logger.FieldAgentID, proc.ID,
				"report_len", len(report),
			)
		}
	}

	m.mu.RLock()
	handler := m.onEvent
	m.mu.RUnlock()
	if handler != nil {
		handler(proc.ID, event)
	}
}

func (m *AgentManager) Submit(id, prompt string, images, files []string) error {
	proc, err := m.get(id)
	if err != nil {
		return err
	}
	m.emitUserMessageEvent(proc, prompt, images, files)
	return proc.Client.Submit(prompt, images, files, nil)
}

func (m *AgentManager) emitUserMessageEvent(proc *AgentProcess, prompt string, images, files []string) {
	m.mu.RLock()
	handler := m.onEvent
	m.mu.RUnlock()
	if proc == nil || handler == nil {
		return
	}

	payloadMap := map[string]any{
		"role":    "user",
		"content": prompt,
	}
	if len(images) > 0 {
		payloadMap["images"] = images
	}
	if len(files) > 0 {
		payloadMap["files"] = files
	}
	event, err := buildJSONEvent("user_message", payloadMap)
	if err != nil {
		logger.Warn("runner: emitUserMessageEvent marshal failed", logger.FieldAgentID, proc.ID, logger.FieldError, err)
		return
	}
	handler(proc.ID, event)
}

func buildJSONEvent(eventType string, payload map[string]any) (agentcore.Event, error) {
	data, err := json.Marshal(payload)
	return agentcore.Event{Type: eventType, Data: data}, err
}

func (m *AgentManager) SendCommand(id, cmd, args string) error {
	proc, err := m.get(id)
	if err != nil {
		return err
	}
	return proc.Client.SendCommand(cmd, args)
}

func (m *AgentManager) SendInput(id string, data []byte) error {
	return m.Submit(id, string(data), nil, nil)
}

func (m *AgentManager) Stop(id string) error {
	logger.Info("runner: stopping agent", logger.FieldAgentID, id)

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
	logger.Info("runner: agent stopped", logger.FieldAgentID, id)
	return nil
}

func (m *AgentManager) StopAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	if len(ids) == 0 {
		return
	}
	logger.Info("runner: stopping all agents (parallel)", logger.FieldCount, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(agentID string) {
			defer wg.Done()
			if err := m.Stop(agentID); err != nil {
				logger.Warn("runner: stop agent failed during StopAll", logger.FieldAgentID, agentID, logger.FieldError, err)
			}
		}(id)
	}
	wg.Wait()
}

func (m *AgentManager) KillAll() {
	m.mu.Lock()
	procs := make([]*AgentProcess, 0, len(m.agents))
	for _, proc := range m.agents {
		procs = append(procs, proc)
	}
	clear(m.agents)
	m.mu.Unlock()

	if len(procs) == 0 {
		return
	}
	logger.Info("runner: force killing all agents", logger.FieldCount, len(procs))
	for _, proc := range procs {
		if err := proc.Client.Kill(); err != nil {
			logger.Warn("runner: KillAll: kill failed", logger.FieldAgentID, proc.ID, logger.FieldError, err)
		}
	}
}

func CleanOrphanedProcesses() {
	out, err := exec.Command("pgrep", "-f", "codex app-server --listen").Output()
	if err != nil {
		return
	}
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	killed := 0
	for _, line := range lines {
		pidStr := strings.TrimSpace(string(line))
		pid, parseErr := strconv.Atoi(pidStr)
		if parseErr != nil || pid <= 0 {
			continue
		}
		if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr == nil {
			killed++
		}
	}
	if killed > 0 {
		logger.Warn("runner: cleaned orphaned codex app-server processes",
			logger.FieldCount, killed,
			"total_found", len(lines),
		)
	}
}

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
			ID:         proc.ID,
			Name:       proc.Name,
			Port:       proc.Client.GetPort(),
			ThreadID:   proc.Client.GetThreadID(),
			State:      proc.State,
			LastReport: proc.LastReport,
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

func (m *AgentManager) Get(id string) *AgentProcess {
	m.mu.RLock()
	proc := m.agents[id]
	m.mu.RUnlock()
	return proc
}

func (m *AgentManager) GetProcess(id string) agentcore.Process {
	return m.Get(id)
}

func (m *AgentManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

func (m *AgentManager) FirstAgentID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.agents) == 0 {
		return ""
	}
	first := ""
	for id := range m.agents {
		if first == "" || id < first {
			first = id
		}
	}
	return first
}

func (m *AgentManager) get(id string) (*AgentProcess, error) {
	m.mu.RLock()
	proc, ok := m.agents[id]
	m.mu.RUnlock()
	if !ok {
		return nil, apperrors.Newf("AgentManager.get", "agent %s not found", id)
	}
	return proc, nil
}

func (m *AgentManager) GetReport(id string) string {
	proc := m.Get(id)
	if proc == nil {
		return ""
	}
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.LastMessage != "" {
		return proc.LastMessage
	}
	return proc.LastReport
}

func (m *AgentManager) GetState(id string) AgentState {
	proc := m.Get(id)
	if proc == nil {
		return ""
	}
	proc.mu.Lock()
	defer proc.mu.Unlock()
	return proc.State
}

func (m *AgentManager) RecoverAgent(id, reason string) error {
	proc := m.Get(id)
	if proc == nil {
		return apperrors.Newf("AgentManager.RecoverAgent", "agent %s not found", id)
	}

	type recoverer interface {
		RecoverConnection(reason string) error
	}

	proc.mu.Lock()
	client := proc.Client
	proc.mu.Unlock()

	if client == nil {
		return apperrors.Newf("AgentManager.RecoverAgent", "agent %s has nil client", id)
	}

	rc, ok := client.(recoverer)
	if !ok {
		logger.Warn("runner: RecoverAgent — client does not support RecoverConnection",
			logger.FieldAgentID, id, "reason", reason)
		return apperrors.Newf("AgentManager.RecoverAgent", "agent %s client does not support recovery", id)
	}

	logger.Info("runner: RecoverAgent — triggering process recovery",
		logger.FieldAgentID, id, "reason", reason)

	if err := rc.RecoverConnection(reason); err != nil {
		logger.Error("runner: RecoverAgent — recovery failed",
			logger.FieldAgentID, id, "reason", reason, logger.FieldError, err)
		proc.mu.Lock()
		proc.State = StateError
		proc.mu.Unlock()
		return apperrors.Wrapf(err, "AgentManager.RecoverAgent", "recover %s", id)
	}

	proc.mu.Lock()
	proc.State = StateIdle
	proc.mu.Unlock()

	logger.Info("runner: RecoverAgent — recovery succeeded",
		logger.FieldAgentID, id, "reason", reason)
	return nil
}

func extractLastAgentMessage(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil || payload == nil {
		return ""
	}
	return extractLastAgentMessageFromMap(payload)
}

func extractLastAgentMessageFromMap(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	for _, key := range []string{"last_agent_message", "lastAgentMessage", "summary", "result", "message", "output", "content", "response", "text"} {
		if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if turn, ok := payload["turn"].(map[string]any); ok {
		for _, key := range []string{"last_agent_message", "lastAgentMessage"} {
			if v, ok := turn[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	for _, key := range []string{"msg", "data", "payload"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if report := extractLastAgentMessageFromMap(nested); report != "" {
			return report
		}
	}
	return ""
}

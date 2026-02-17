// orchestration_tools.go — Agent 编排动态工具 (list/send/launch/stop)。
//
// 通过 Dynamic Tool 注入机制暴露给 codex agent,
// 使 agent 具备多 agent 编排能力。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

// maxAgents 最大 Agent 数量 (fork-bomb 保护)。
const maxAgents = 20

// buildOrchestrationTools 返回 Agent 编排工具定义 (注入 codex agent)。
func (s *Server) buildOrchestrationTools() []codex.DynamicTool {
	return []codex.DynamicTool{
		{
			Name:        "orchestration_list_agents",
			Description: "List all running agents with their ID, name, state, port and thread ID.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "orchestration_send_message",
			Description: "Send a message to another running agent by its ID. The message will be submitted as a new turn prompt.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_id": map[string]any{"type": "string", "description": "Target agent ID"},
					"message":  map[string]any{"type": "string", "description": "Message to send"},
				},
				"required": []string{"agent_id", "message"},
			},
		},
		{
			Name:        "orchestration_launch_agent",
			Description: "Launch a new agent subprocess. The new agent will also have orchestration tools injected.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":   map[string]any{"type": "string", "description": "Display name for the new agent"},
					"prompt": map[string]any{"type": "string", "description": "Initial prompt (optional)"},
					"cwd":    map[string]any{"type": "string", "description": "Working directory (optional, defaults to '.')"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "orchestration_stop_agent",
			Description: "Stop a running agent by its ID.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_id": map[string]any{"type": "string", "description": "Agent ID to stop"},
				},
				"required": []string{"agent_id"},
			},
		},
	}
}

// orchestrationListAgents 列出所有 Agent。
func (s *Server) orchestrationListAgents() string {
	infos := s.mgr.List()
	data, err := json.Marshal(infos)
	if err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err.Error())
	}
	if len(infos) == 0 {
		return "[]"
	}
	return string(data)
}

// orchestrationSendMessage 发送消息给其他 Agent。
func (s *Server) orchestrationSendMessage(args json.RawMessage) string {
	var p struct {
		AgentID string `json:"agent_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}
	if p.AgentID == "" || p.Message == "" {
		return `{"error":"agent_id and message are required"}`
	}

	if err := s.mgr.Submit(p.AgentID, p.Message, nil, nil); err != nil {
		return fmt.Sprintf(`{"error":"send failed: %s"}`, err.Error())
	}

	slog.Info("orchestration: message sent", "to", p.AgentID, "len", len(p.Message))
	return fmt.Sprintf(`{"success":true,"agent_id":"%s"}`, p.AgentID)
}

// orchestrationLaunchAgent 启动新 Agent。
func (s *Server) orchestrationLaunchAgent(args json.RawMessage) string {
	var p struct {
		Name   string `json:"name"`
		Prompt string `json:"prompt"`
		Cwd    string `json:"cwd"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}
	if p.Name == "" {
		return `{"error":"name is required"}`
	}
	if p.Cwd == "" {
		p.Cwd = "."
	}

	// fork-bomb 保护
	if len(s.mgr.List()) >= maxAgents {
		return fmt.Sprintf(`{"error":"max agents (%d) reached"}`, maxAgents)
	}

	// 生成唯一 ID
	id := fmt.Sprintf("agent-%d-%d", time.Now().UnixMilli(), s.threadSeq.Add(1))

	// 30s 超时 context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 构建完整工具列表 (LSP + 编排 + 资源)
	tools := s.buildAllDynamicTools()

	if err := s.mgr.Launch(ctx, id, p.Name, p.Prompt, p.Cwd, tools); err != nil {
		return fmt.Sprintf(`{"error":"launch failed: %s"}`, err.Error())
	}

	slog.Info("orchestration: agent launched", "id", id, "name", p.Name)
	return fmt.Sprintf(`{"agent_id":"%s","name":"%s","status":"running"}`, id, p.Name)
}

// orchestrationStopAgent 停止 Agent。
func (s *Server) orchestrationStopAgent(args json.RawMessage) string {
	var p struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %s"}`, err.Error())
	}
	if p.AgentID == "" {
		return `{"error":"agent_id is required"}`
	}

	if err := s.mgr.Stop(p.AgentID); err != nil {
		return fmt.Sprintf(`{"error":"stop failed: %s"}`, err.Error())
	}

	slog.Info("orchestration: agent stopped", "id", p.AgentID)
	return fmt.Sprintf(`{"success":true,"agent_id":"%s"}`, p.AgentID)
}

// buildAllDynamicTools 构建全部动态工具列表 (LSP + 编排 + 资源)。
func (s *Server) buildAllDynamicTools() []codex.DynamicTool {
	var tools []codex.DynamicTool
	tools = append(tools, s.buildLSPDynamicTools()...)
	tools = append(tools, s.buildOrchestrationTools()...)
	tools = append(tools, s.buildResourceTools()...)
	return tools
}

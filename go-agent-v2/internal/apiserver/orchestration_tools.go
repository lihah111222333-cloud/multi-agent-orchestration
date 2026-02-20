// orchestration_tools.go — Agent 编排动态工具 (list/send/launch/stop)。
//
// 通过 Dynamic Tool 注入机制暴露给 codex agent,
// 使 agent 具备多 agent 编排能力。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
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
					"name":              map[string]any{"type": "string", "description": "Display name for the new agent"},
					"prompt":            map[string]any{"type": "string", "description": "Initial prompt (optional)"},
					"cwd":               map[string]any{"type": "string", "description": "Working directory (optional, defaults to '.')"},
					"workspace_run_key": map[string]any{"type": "string", "description": "Optional workspace run key. If provided, agent cwd is resolved to that run's virtual workspace."},
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
		return toolError(err)
	}
	if len(infos) == 0 {
		return "[]"
	}
	return string(data)
}

// orchestrationSendMessage 发送消息给其他 Agent。
func (s *Server) orchestrationSendMessage(args json.RawMessage) string {
	return s.orchestrationSendMessageFrom("", args)
}

func (s *Server) orchestrationSendMessageFrom(senderID string, args json.RawMessage) string {
	var p struct {
		AgentID string `json:"agent_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(apperrors.Wrap(err, "orchestrationSendMessage", "unmarshal args"))
	}
	if p.AgentID == "" || p.Message == "" {
		return `{"error":"agent_id and message are required"}`
	}

	if err := s.submitAgentPrompt(p.AgentID, p.Message, nil, nil); err != nil {
		return toolError(apperrors.Wrap(err, "orchestrationSendMessage", "submit message"))
	}
	s.rememberOrchestrationReportRequest(senderID, p.AgentID)

	logger.Info("orchestration: message sent",
		"from", strings.TrimSpace(senderID),
		"to", p.AgentID,
		logger.FieldLen, len(p.Message),
	)
	return toolJSON(map[string]any{"success": true, "agent_id": p.AgentID})
}

// orchestrationLaunchAgent 启动新 Agent。
func (s *Server) orchestrationLaunchAgent(args json.RawMessage) string {
	var p struct {
		Name            string `json:"name"`
		Prompt          string `json:"prompt"`
		Cwd             string `json:"cwd"`
		WorkspaceRunKey string `json:"workspace_run_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(apperrors.Wrap(err, "orchestrationLaunchAgent", "unmarshal args"))
	}
	if p.Name == "" {
		return `{"error":"name is required"}`
	}

	if p.WorkspaceRunKey != "" {
		if s.workspaceMgr == nil {
			return toolError(apperrors.New("orchestrationLaunchAgent", "workspace manager not initialized"))
		}
		workspacePath, err := s.workspaceMgr.ResolveRunWorkspace(context.Background(), p.WorkspaceRunKey)
		if err != nil {
			return toolError(apperrors.Wrapf(err, "orchestrationLaunchAgent", "resolve workspace run %s", p.WorkspaceRunKey))
		}
		p.Cwd = workspacePath
	}
	if p.Cwd == "" {
		p.Cwd = "."
	}

	// fork-bomb 保护
	if len(s.mgr.List()) >= maxAgents {
		return toolError(apperrors.Newf("orchestrationLaunchAgent", "max agents (%d) reached", maxAgents))
	}

	// 生成唯一 ID
	id := fmt.Sprintf("agent-%d-%d", time.Now().UnixMilli(), s.threadSeq.Add(1))

	// 30s 超时 context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 构建完整工具列表 (LSP + 编排 + 资源)
	tools := s.buildAllDynamicTools()

	if err := s.mgr.Launch(ctx, id, p.Name, p.Prompt, p.Cwd, tools); err != nil {
		return toolError(apperrors.Wrap(err, "orchestrationLaunchAgent", "launch agent"))
	}

	logger.Info("orchestration: agent launched", logger.FieldID, id, logger.FieldName, p.Name, logger.FieldCwd, p.Cwd, logger.FieldRunKey, p.WorkspaceRunKey)
	return toolJSON(map[string]any{
		"agent_id":          id,
		"name":              p.Name,
		"status":            "running",
		"cwd":               p.Cwd,
		"workspace_run_key": p.WorkspaceRunKey,
	})
}

// orchestrationStopAgent 停止 Agent。
func (s *Server) orchestrationStopAgent(args json.RawMessage) string {
	var p struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return toolError(apperrors.Wrap(err, "orchestrationStopAgent", "unmarshal args"))
	}
	if p.AgentID == "" {
		return `{"error":"agent_id is required"}`
	}

	if err := s.mgr.Stop(p.AgentID); err != nil {
		return toolError(apperrors.Wrap(err, "orchestrationStopAgent", "stop agent"))
	}

	// 共生共灭: agent 停止 → 解除 codexThreadId 绑定。
	if s.bindingStore != nil {
		if ubErr := s.bindingStore.Unbind(context.Background(), p.AgentID); ubErr != nil {
			logger.Warn("orchestration: unbind failed on stop", logger.FieldAgentID, p.AgentID, logger.FieldError, ubErr)
		}
	}

	logger.Info("orchestration: agent stopped", logger.FieldID, p.AgentID)
	return toolJSON(map[string]any{"success": true, "agent_id": p.AgentID})
}

// buildAllDynamicTools 构建全部动态工具列表 (LSP + 编排 + 资源)。
func (s *Server) buildAllDynamicTools() []codex.DynamicTool {
	var tools []codex.DynamicTool
	tools = append(tools, s.buildLSPDynamicTools()...)
	tools = append(tools, s.buildOrchestrationTools()...)
	tools = append(tools, s.buildResourceTools()...)
	return tools
}

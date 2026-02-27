package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const maxAgents = 200

func OrchestrationTools(provider OrchestrationProvider, runtime AgentRuntimeProvider, schemaProvider SchemaProvider) []Tool {
	return []Tool{
		{
			Schema: agentcore.DynamicTool{
				Name:        "orchestration_list_agents",
				Description: "List all running agents with their ID, name, state, port and thread ID.",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			Handler: func(ctx ToolCallContext, _ json.RawMessage) string {
				return orchestrationListAgents(provider, ctx.AgentID)
			},
		},
		{
			Schema: agentcore.DynamicTool{
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
			Handler: func(ctx ToolCallContext, args json.RawMessage) string {
				return orchestrationSendMessage(provider, ctx.AgentID, args)
			},
		},
		{
			Schema: agentcore.DynamicTool{
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
			Handler: func(ctx ToolCallContext, args json.RawMessage) string {
				return orchestrationLaunchAgent(provider, runtime, schemaProvider, ctx, args)
			},
		},
		{
			Schema: agentcore.DynamicTool{
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
			Handler: func(_ ToolCallContext, args json.RawMessage) string {
				return orchestrationStopAgent(provider, runtime, args)
			},
		},
		{
			Schema: agentcore.DynamicTool{
				Name:        "orchestration_get_agent_report",
				Description: "Get the last completion report from a running agent. Returns the agent's last response/summary after it finished a task. Empty string means the agent has not completed any task yet.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"agent_id": map[string]any{"type": "string", "description": "Agent ID to get report from"},
					},
					"required": []string{"agent_id"},
				},
			},
			Handler: func(_ ToolCallContext, args json.RawMessage) string {
				return orchestrationGetAgentReport(provider, args)
			},
		},
	}
}

func orchestrationListAgents(provider OrchestrationProvider, callerID string) string {
	if provider == nil || provider.AgentLauncher() == nil {
		return "[]"
	}
	infos := provider.AgentLauncher().List()
	if orchestrationListLen(infos) == 0 {
		return "[]"
	}
	callerID = strings.TrimSpace(callerID)
	if callerID == "" {
		data, err := json.Marshal(infos)
		if err != nil {
			return ToolError(err)
		}
		return string(data)
	}
	data, err := json.Marshal(infos)
	if err != nil {
		return ToolError(err)
	}
	var list []map[string]any
	if err := json.Unmarshal(data, &list); err != nil {
		return string(data)
	}
	filtered := make([]map[string]any, 0, len(list))
	for _, item := range list {
		agentID, _ := item["id"].(string)
		if strings.TrimSpace(agentID) == callerID {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		return "[]"
	}
	out, err := json.Marshal(filtered)
	if err != nil {
		return ToolError(err)
	}
	return string(out)
}

func orchestrationSendMessage(provider OrchestrationProvider, senderID string, args json.RawMessage) string {
	var p struct {
		AgentID string `json:"agent_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(apperrors.Wrap(err, "orchestrationSendMessage", "unmarshal args"))
	}
	if p.AgentID == "" || p.Message == "" {
		return `{"error":"agent_id and message are required"}`
	}
	if strings.TrimSpace(senderID) != "" && strings.TrimSpace(p.AgentID) == strings.TrimSpace(senderID) {
		return `{"error":"cannot send message to self"}`
	}
	if provider == nil {
		return ToolError(apperrors.New("orchestrationSendMessage", "orchestration provider not initialized"))
	}

	if err := provider.SubmitPrompt(p.AgentID, p.Message, nil, nil); err != nil {
		return ToolError(apperrors.Wrap(err, "orchestrationSendMessage", "submit message"))
	}
	provider.RememberReportRequest(senderID, p.AgentID)

	logger.Info("orchestration: message sent",
		"from", strings.TrimSpace(senderID),
		"to", p.AgentID,
		logger.FieldLen, len(p.Message),
	)
	return ToolJSON(map[string]any{"success": true, "agent_id": p.AgentID})
}

func orchestrationLaunchAgent(provider OrchestrationProvider, runtime AgentRuntimeProvider, schemaProvider SchemaProvider, callCtx ToolCallContext, args json.RawMessage) string {
	var p struct {
		Name            string `json:"name"`
		Prompt          string `json:"prompt"`
		Cwd             string `json:"cwd"`
		WorkspaceRunKey string `json:"workspace_run_key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(apperrors.Wrap(err, "orchestrationLaunchAgent", "unmarshal args"))
	}
	if p.Name == "" {
		return `{"error":"name is required"}`
	}
	if provider == nil || provider.AgentLauncher() == nil {
		return ToolError(apperrors.New("orchestrationLaunchAgent", "agent manager not initialized"))
	}

	if p.WorkspaceRunKey != "" {
		if provider.WorkspaceOps() == nil {
			return ToolError(apperrors.New("orchestrationLaunchAgent", "workspace manager not initialized"))
		}
		workspacePath, err := provider.WorkspaceOps().ResolveRunWorkspace(context.Background(), p.WorkspaceRunKey)
		if err != nil {
			return ToolError(apperrors.Wrapf(err, "orchestrationLaunchAgent", "resolve workspace run %s", p.WorkspaceRunKey))
		}
		p.Cwd = workspacePath
	}
	if p.Cwd == "" {
		p.Cwd = "."
	}

	if orchestrationListLen(provider.AgentLauncher().List()) >= maxAgents {
		return ToolError(apperrors.Newf("orchestrationLaunchAgent", "max agents (%d) reached", maxAgents))
	}

	id := fmt.Sprintf("agent-%d-%d", time.Now().UnixMilli(), nextThreadSeq(provider))

	baseCtx := context.Background()
	if callCtx.Ctx != nil {
		baseCtx = callCtx.Ctx
	}
	ctx, cancel := context.WithTimeout(baseCtx, 30*time.Second)
	defer cancel()

	var schemas []agentcore.DynamicTool
	if schemaProvider != nil {
		schemas = schemaProvider.AllSchemas()
	}
	if err := provider.AgentLauncher().Launch(ctx, id, p.Name, p.Prompt, p.Cwd, "", schemas); err != nil {
		return ToolError(apperrors.Wrap(err, "orchestrationLaunchAgent", "launch agent"))
	}
	if runtime != nil {
		runtime.SetAgentWorkDir(id, p.Cwd)
	}
	provider.SaveSubAgent(id, p.Name, p.Cwd)

	logger.Info("orchestration: agent launched", logger.FieldID, id, logger.FieldName, p.Name, logger.FieldCwd, p.Cwd, logger.FieldRunKey, p.WorkspaceRunKey)
	return ToolJSON(map[string]any{
		"agent_id":          id,
		"name":              p.Name,
		"status":            "running",
		"cwd":               p.Cwd,
		"workspace_run_key": p.WorkspaceRunKey,
	})
}

func orchestrationStopAgent(provider OrchestrationProvider, runtime AgentRuntimeProvider, args json.RawMessage) string {
	var p struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(apperrors.Wrap(err, "orchestrationStopAgent", "unmarshal args"))
	}
	if p.AgentID == "" {
		return `{"error":"agent_id is required"}`
	}
	if provider == nil || provider.AgentLauncher() == nil {
		return ToolError(apperrors.New("orchestrationStopAgent", "agent manager not initialized"))
	}

	if runtime != nil {
		if cancelled := runtime.CancelCodeRuns(p.AgentID); cancelled > 0 {
			logger.Info("orchestration: cancelled running code_run executions before stop",
				logger.FieldAgentID, p.AgentID,
				"cancelled_runs", cancelled,
			)
		}
	}

	if err := provider.AgentLauncher().Stop(p.AgentID); err != nil {
		return ToolError(apperrors.Wrap(err, "orchestrationStopAgent", "stop agent"))
	}
	if runtime != nil {
		runtime.ClearAgentWorkDir(p.AgentID)
	}
	provider.DeleteSubAgent(p.AgentID)

	logger.Info("orchestration: agent stopped", logger.FieldID, p.AgentID)
	return ToolJSON(map[string]any{"success": true, "agent_id": p.AgentID})
}

func orchestrationGetAgentReport(provider OrchestrationProvider, args json.RawMessage) string {
	var p struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return ToolError(apperrors.Wrap(err, "orchestrationGetAgentReport", "unmarshal args"))
	}
	if p.AgentID == "" {
		return `{"error":"agent_id is required"}`
	}
	if provider == nil || provider.AgentLauncher() == nil {
		return ToolError(apperrors.New("orchestrationGetAgentReport", "agent manager not initialized"))
	}
	report := provider.AgentLauncher().GetReport(p.AgentID)
	state := provider.AgentLauncher().GetState(p.AgentID)
	return ToolJSON(map[string]any{"agent_id": p.AgentID, "state": state, "report": report})
}

func orchestrationListLen(v any) int {
	if v == nil {
		return 0
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return 0
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.String:
		return rv.Len()
	default:
		return 0
	}
}

func nextThreadSeq(provider OrchestrationProvider) int64 {
	if provider == nil {
		return time.Now().UnixNano()
	}
	seq := provider.NextThreadSeq()
	if seq > 0 {
		return seq
	}
	return time.Now().UnixNano()
}

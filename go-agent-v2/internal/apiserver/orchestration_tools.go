// orchestration_tools.go — Agent 编排动态工具 apiserver 薄委派。
package apiserver

import (
	"encoding/json"
	"fmt"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/tools"
)

func (s *Server) orchestrationToolset() []tools.Tool {
	return tools.OrchestrationTools(s, s, s)
}

func (s *Server) orchestrationToolSchemas() []agentcore.DynamicTool {
	return tools.Schemas(s.orchestrationToolset())
}

func (s *Server) runOrchestrationTool(name, senderID string, args json.RawMessage) string {
	tool, ok := tools.FindTool(s.orchestrationToolset(), name)
	if !ok || tool.Handler == nil {
		return tools.ToolError(fmt.Errorf("orchestration tool %s unavailable", name))
	}
	return tool.Handler(tools.ToolCallContext{AgentID: senderID}, args)
}

func (s *Server) orchestrationListAgents() string {
	return s.runOrchestrationTool("orchestration_list_agents", "", nil)
}

func (s *Server) orchestrationSendMessage(args json.RawMessage) string {
	return s.orchestrationSendMessageFrom("", args)
}

func (s *Server) orchestrationSendMessageFrom(senderID string, args json.RawMessage) string {
	return s.runOrchestrationTool("orchestration_send_message", senderID, args)
}

func (s *Server) orchestrationLaunchAgent(args json.RawMessage) string {
	return s.runOrchestrationTool("orchestration_launch_agent", "", args)
}

func (s *Server) orchestrationStopAgent(args json.RawMessage) string {
	return s.runOrchestrationTool("orchestration_stop_agent", "", args)
}

// allDynamicToolSchemas 构建全部动态工具列表 (LSP + 编排 + 资源 + 代码执行)。
func (s *Server) allDynamicToolSchemas() []agentcore.DynamicTool {
	var all []agentcore.DynamicTool
	all = append(all, s.lspDynamicToolSchemas()...)
	all = append(all, s.orchestrationToolSchemas()...)
	all = append(all, s.resourceToolSchemas()...)
	all = append(all, s.codeRunToolSchemas()...)
	return all
}

func (s *Server) AllSchemas() []agentcore.DynamicTool {
	return s.allDynamicToolSchemas()
}

func (s *Server) NextThreadSeq() int64 {
	return s.threadSeq.Add(1)
}

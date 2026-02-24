// orchestration_tools.go — 编排类工具聚合/Provider 实现（生命周期层）。
package apiserver

import (
	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/tooladapter"
)

// allDynamicToolSchemas 构建全部动态工具列表。
func (s *Server) allDynamicToolSchemas() []agentcore.DynamicTool {
	if s == nil {
		return nil
	}
	return tooladapter.AllSchemas(s.toolAdapterProviders())
}

func (s *Server) AllSchemas() []agentcore.DynamicTool {
	return s.allDynamicToolSchemas()
}

func (s *Server) NextThreadSeq() int64 {
	return s.threadSeq.Add(1)
}

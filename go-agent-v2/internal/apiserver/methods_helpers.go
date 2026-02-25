// methods_helpers.go — 仅保留 thread/* 斜杠命令相关 handlers。
package apiserver

import (
	"context"
	"encoding/json"
)

// threadBgTerminalsClean 清理后台终端 (experimental)。
func (s *Server) threadBgTerminalsClean(ctx context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, "/clean")
}

// threadUndo 撤销上一步 (/undo)。
func (s *Server) threadUndo(ctx context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, "/undo")
}

// threadModelSet 切换模型 (/model <name>)。
func (s *Server) threadModelSet(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandWithArgs(params, "/model", "model")
}

// threadPersonality 设置人格 (/personality <type>)。
func (s *Server) threadPersonality(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandWithArgs(params, "/personality", "personality")
}

// threadApprovals 设置审批策略 (/approvals <policy>)。
func (s *Server) threadApprovals(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandWithArgs(params, "/approvals", "policy")
}

// threadMCPList 列出 MCP 工具 (/mcp)。
func (s *Server) threadMCPList(ctx context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, "/mcp")
}

// threadSkillsList 列出 Skills（统一走本地 SkillService 缓存，不透传外部 /skills）。
func (s *Server) threadSkillsList(_ context.Context, _ json.RawMessage) (any, error) {
	return s.codexAdapter.ThreadSkillsList()
}

// threadDebugMemory 调试记忆 (/debug-m-drop 或 /debug-m-update)。
func (s *Server) threadDebugMemory(_ context.Context, params json.RawMessage) (any, error) {
	return s.codexAdapter.SendSlashCommandWithArgs(params, "/debug-m-drop", "action")
}

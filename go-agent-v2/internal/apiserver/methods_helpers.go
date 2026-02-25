// methods_helpers.go — 仅保留 thread/* 斜杠命令相关 handlers。
package apiserver

import (
	"context"
	"encoding/json"
	"strings"
)

// threadBgTerminalsClean 清理后台终端 (experimental)。
func (s *Server) threadBgTerminalsClean(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/clean")
}

// threadUndo 撤销上一步 (/undo)。
func (s *Server) threadUndo(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/undo")
}

// threadModelSet 切换模型 (/model <name>)。
func (s *Server) threadModelSet(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommandWithArgs(params, "/model", "model")
}

// threadPersonality 设置人格 (/personality <type>)。
func (s *Server) threadPersonality(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommandWithArgs(params, "/personality", "personality")
}

// threadApprovals 设置审批策略 (/approvals <policy>)。
func (s *Server) threadApprovals(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommandWithArgs(params, "/approvals", "policy")
}

// threadMCPList 列出 MCP 工具 (/mcp)。
func (s *Server) threadMCPList(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/mcp")
}

// threadSkillsList 列出 Skills（统一走本地 SkillService 缓存，不透传外部 /skills）。
func (s *Server) threadSkillsList(_ context.Context, _ json.RawMessage) (any, error) {
	return s.codexAdapter.ThreadSkillsList(func() ([]string, error) {
		if s.skillSvc == nil {
			return []string{}, nil
		}
		list, err := s.skillSvc.ListSkills()
		if err != nil {
			return nil, err
		}
		skills := make([]string, 0, len(list))
		for _, item := range list {
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			skills = append(skills, name)
		}
		return skills, nil
	})
}

// threadDebugMemory 调试记忆 (/debug-m-drop 或 /debug-m-update)。
func (s *Server) threadDebugMemory(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommandWithArgs(params, "/debug-m-drop", "action")
}

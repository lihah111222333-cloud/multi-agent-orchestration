// methods_thread_helpers.go — thread 辅助逻辑与斜杠命令底层实现。
package apiserver

import (
	"context"
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
)

// isLikelyCodexThreadID delegates to codexadapter (single canonical impl).
func isLikelyCodexThreadID(raw string) bool {
	return codexadapter.IsLikelyCodexThreadID(raw)
}

// normalizeCodexThreadID delegates to codexadapter (single canonical impl).
func normalizeCodexThreadID(raw string) string {
	return codexadapter.NormalizeCodexThreadID(raw)
}

func appendUniqueThreadID(dst []string, seen map[string]struct{}, candidate string) []string {
	id := normalizeCodexThreadID(candidate)
	if id == "" {
		return dst
	}
	if _, ok := seen[id]; ok {
		return dst
	}
	seen[id] = struct{}{}
	return append(dst, id)
}

// ========================================
// 斜杠命令 (sendSlashCommand + handlers)
// ========================================

func (s *Server) sendSlashCommand(ctx context.Context, params json.RawMessage, command string) (any, error) {
	return s.codexAdapter.SendSlashCommandFromRawParams(ctx, params, command)
}

// sendSlashCommandWithArgs 带参数的斜杠命令。
func (s *Server) sendSlashCommandWithArgs(params json.RawMessage, command string, argsField string) (any, error) {
	return s.codexAdapter.SendSlashCommandWithArgs(params, command, argsField)
}

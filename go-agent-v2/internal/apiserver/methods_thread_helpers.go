// methods_thread_helpers.go — thread 辅助逻辑与斜杠命令底层实现。
package apiserver

import (
	"context"
	"encoding/json"
	"strings"
)

func isLikelyCodexThreadID(raw string) bool {
	id := strings.TrimSpace(raw)
	if id == "" {
		return false
	}
	id = strings.TrimPrefix(strings.ToLower(id), "urn:uuid:")
	return codexThreadIDPattern.MatchString(id)
}

func normalizeCodexThreadID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	id = strings.TrimPrefix(strings.ToLower(id), "urn:uuid:")
	if !codexThreadIDPattern.MatchString(id) {
		return ""
	}
	return id
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

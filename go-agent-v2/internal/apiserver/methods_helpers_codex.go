// methods_helpers_codex.go — codex 线程恢复与斜杠命令底层实现。
package apiserver

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (s *Server) threadExistsInHistory(ctx context.Context, threadID string) bool {
	return s.codexAdapter.ThreadExistsInHistory(ctx, threadID, isLikelyCodexThreadID, s.loadThreadArchiveMap)
}

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

func buildResumeCandidates(threadID string, resolved []string) []string {
	return codexadapter.BuildResumeCandidates(threadID, resolved, normalizeCodexThreadID)
}

// tryResumeCandidates 按顺序尝试候选 thread ID 恢复会话。
//
// 行为:
//   - 成功 → 返回 (成功ID, nil)
//   - 候选错误 (isHistoricalResumeCandidateError) → 跳过,尝试下一个
//   - 非候选错误 (网络等) → 立即返回 error
//   - 所有候选耗尽 → 返回 error (避免伪造 resumed 成功)
//   - 无候选 → 返回 error
func tryResumeCandidates(candidates []string, fallbackID string, resumeFn func(string) error) (string, error) {
	return codexadapter.TryResumeCandidates(candidates, fallbackID, resumeFn, isHistoricalResumeCandidateError)
}

func previewResumeCandidates(candidates []string, max int) []string {
	return codexadapter.PreviewResumeCandidates(candidates, max)
}

func (s *Server) resolvePrimaryCodexThreadID(ctx context.Context, agentID string) string {
	ids := s.resolveCodexThreadCandidates(ctx, agentID)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func (s *Server) resolveCodexThreadCandidates(ctx context.Context, agentID string) []string {
	return s.codexAdapter.ResolveCodexThreadCandidates(ctx, agentID, appendUniqueThreadID, previewResumeCandidates)
}

func isHistoricalResumeCandidateError(err error) bool {
	return codexadapter.IsHistoricalResumeCandidateError(err)
}

// isCodexProcessCrashError 判断错误是否为 codex 进程 crash (需要 re-spawn)。
//
// 与 isHistoricalResumeCandidateError 的区别:
//   - candidateError: rollout 不可用但进程仍健在 → 直接尝试下一个候选
//   - crashError: 进程已死 → 必须 Stop + Launch 新进程后才能继续
func isCodexProcessCrashError(err error) bool {
	return codexadapter.IsCodexProcessCrashError(err)
}

// buildSessionLostNotification 构建会话丢失降级通知 (method + payload)。
//
// 使用 ui/state/changed 以复用前端已有的事件监听，无需前端新增处理。
func buildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
	detail := ""
	if lastErr != nil {
		detail = lastErr.Error()
	}
	return "ui/state/changed", map[string]any{
		"source":   "session_lost_warning",
		"agent_id": agentID,
		"warning":  "会话历史已丢失 (codex session 文件不存在)，已自动回退到全新会话",
		"detail":   detail,
	}
}
func (s *Server) ensureThreadReadyForTurn(ctx context.Context, threadID, cwd string) (*runner.AgentProcess, error) {
	return s.codexAdapter.EnsureThreadReadyForTurn(ctx, codexadapter.EnsureThreadReadyOptions{
		ThreadID:                          threadID,
		Cwd:                               cwd,
		Manager:                           s.mgr,
		BindingStore:                      s.bindingStore,
		ThreadExistsInHistory:             s.threadExistsInHistory,
		ResolveCodexThreadCandidates:      s.resolveCodexThreadCandidates,
		BuildAllDynamicTools:              s.allDynamicToolSchemas,
		ResolveStartInstructionsForLaunch: s.resolveStartInstructionsForLaunch,
		SetAgentWorkDir:                   s.setAgentWorkDir,
		RegisterBinding:                   s.registerBinding,
		CancelCodeRuns:                    s.cancelCodeRuns,
		BroadcastNotification: func(method string, payload map[string]any) {
			s.broadcastNotification(method, payload)
		},
		BuildSessionLostNotification: buildSessionLostNotification,
	})
}

// registerBinding 注册 agentId ↔ codexThreadId 绑定。
//
// ⚠️  根基约束: agent_id 与 codex_thread_id 1:1 共生。
// 此函数在每次 ensureThreadReadyForTurn 成功后调用,
// 确保 DB 绑定记录始终与运行时状态一致。
func (s *Server) registerBinding(ctx context.Context, agentID string, proc *runner.AgentProcess) {
	if s.bindingStore == nil || proc == nil || proc.Client == nil {
		return
	}
	codexThreadID := s.codexAdapter.GetThreadID(proc)
	if codexThreadID == "" {
		return
	}
	if err := s.bindingStore.Bind(ctx, agentID, codexThreadID, ""); err != nil {
		logger.Warn("turn/start: failed to register binding",
			logger.FieldAgentID, agentID,
			"codex_thread_id", codexThreadID,
			logger.FieldError, err,
		)
	}
}

// ========================================
// 斜杠命令 (sendSlashCommand + handlers)
// ========================================

// sendSlashCommand 通用斜杠命令发送 (compact, interrupt 等)。
func resolveSlashCommandThread(
	ctx context.Context,
	threadID string,
	getProc func(string) *runner.AgentProcess,
	ensureReady func(context.Context, string, string) (*runner.AgentProcess, error),
) (*runner.AgentProcess, error) {
	return codexadapter.ResolveSlashCommandThread(ctx, threadID, getProc, ensureReady)
}

func (s *Server) resolveThreadForSlashCommand(ctx context.Context, threadID string) (*runner.AgentProcess, error) {
	if s == nil || s.mgr == nil {
		return nil, apperrors.New("Server.sendSlashCommand", "thread manager is not initialized")
	}
	return resolveSlashCommandThread(ctx, threadID, s.mgr.Get, s.ensureThreadReadyForTurn)
}

func (s *Server) sendSlashCommand(ctx context.Context, params json.RawMessage, command string) (any, error) {
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.sendSlashCommand", "unmarshal params")
	}
	return codexadapter.SendSlashCommand(ctx, codexadapter.SendSlashCommandOptions{
		ThreadID:               p.ThreadID,
		Command:                command,
		ParamsLen:              len(params),
		ReadThreadRuntimeState: s.readThreadRuntimeState,
		HasActiveTrackedTurn:   s.hasActiveTrackedTurn,
		ResolveThread:          s.resolveThreadForSlashCommand,
		SendCommand:            s.codexAdapter.SendCommand,
		GetThreadID:            s.codexAdapter.GetThreadID,
	})
}

// sendSlashCommandWithArgs 带参数的斜杠命令。
func (s *Server) sendSlashCommandWithArgs(params json.RawMessage, command string, argsField string) (any, error) {
	threadID, args, err := codexadapter.ParseSlashCommandWithArgsParams(params, argsField)
	if err != nil {
		return nil, err
	}
	return s.withThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		if err := s.codexAdapter.SendCommand(proc, command, args); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

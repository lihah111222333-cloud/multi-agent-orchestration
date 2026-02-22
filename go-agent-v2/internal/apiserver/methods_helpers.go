// methods_helpers.go — 线程解析/恢复、斜杠命令、输入处理、debug 诊断辅助函数。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ========================================
// helpers — thread resolution
// ========================================

// withThread 查找线程并执行回调 (消除重复的 getThread→notFound 样板)。
func (s *Server) withThread(threadID string, fn func(*runner.AgentProcess) (any, error)) (any, error) {
	proc := s.mgr.Get(threadID)
	if proc == nil {
		return nil, apperrors.Newf("Server.withThread", "thread %s not found", threadID)
	}
	return fn(proc)
}

func (s *Server) threadExistsInHistory(ctx context.Context, threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	if isLikelyCodexThreadID(id) {
		return true
	}

	if s.bindingStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		binding, err := s.bindingStore.FindByAgentID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from agent_codex_binding failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if binding != nil {
			return true
		}
	}

	if s.agentStatusStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		status, err := s.agentStatusStore.Get(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: check thread history from agent_status failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if status != nil {
			return true
		}
	}

	return false
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
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	if normalized := normalizeCodexThreadID(id); normalized != "" {
		return []string{normalized}
	}

	candidates := make([]string, 0, len(resolved))
	seen := map[string]struct{}{}
	for _, candidate := range resolved {
		value := strings.TrimSpace(candidate)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}
	if len(candidates) > 0 {
		return candidates
	}
	return []string{id}
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
	if len(candidates) == 0 {
		logger.Warn("thread/resume: no resume candidates available",
			logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
			"reason", "no codex thread ID resolved from history",
		)
		return "", apperrors.Newf("tryResumeCandidates", "no resume candidates available for thread %s", fallbackID)
	}

	var lastErr error
	for _, id := range candidates {
		err := resumeFn(id)
		if err == nil {
			return id, nil
		}
		lastErr = err
		if isHistoricalResumeCandidateError(err) {
			logger.Warn("thread/resume: candidate unavailable, trying next",
				logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
				"resume_thread_id", id,
				logger.FieldError, err,
			)
			continue
		}
		// 非候选错误 (网络断开等) → 直接传播
		return "", err
	}

	// 所有候选都是 candidate error → 返回 error，避免伪装恢复成功
	logger.Warn("thread/resume: all resume candidates exhausted",
		logger.FieldAgentID, fallbackID, logger.FieldThreadID, fallbackID,
		"candidate_count", len(candidates),
		"last_error", lastErr,
		"reason", "all historical rollouts unavailable",
	)
	if lastErr != nil {
		return "", apperrors.Wrapf(lastErr, "tryResumeCandidates", "all resume candidates unavailable for thread %s after %d attempts", fallbackID, len(candidates))
	}
	return "", apperrors.Newf("tryResumeCandidates", "all resume candidates unavailable for thread %s after %d attempts", fallbackID, len(candidates))
}

func previewResumeCandidates(candidates []string, max int) []string {
	if len(candidates) == 0 || max <= 0 {
		return nil
	}
	if len(candidates) <= max {
		return append([]string(nil), candidates...)
	}
	preview := append([]string(nil), candidates[:max]...)
	preview = append(preview, fmt.Sprintf("...+%d more", len(candidates)-max))
	return preview
}

func (s *Server) resolvePrimaryCodexThreadID(ctx context.Context, agentID string) string {
	ids := s.resolveCodexThreadCandidates(ctx, agentID)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func (s *Server) resolveCodexThreadCandidates(ctx context.Context, agentID string) []string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil
	}

	ids := make([]string, 0, 2)
	seen := map[string]struct{}{}
	ids = appendUniqueThreadID(ids, seen, id)

	if s.bindingStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		binding, err := s.bindingStore.FindByAgentID(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: resolve codex thread id from binding failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if binding != nil {
			ids = appendUniqueThreadID(ids, seen, binding.CodexThreadID)
		}
	}

	if s.agentStatusStore != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		status, err := s.agentStatusStore.Get(dbCtx, id)
		cancel()
		if err != nil {
			logger.Warn("turn/start: resolve codex thread id from agent_status failed",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else if status != nil {
			ids = appendUniqueThreadID(ids, seen, status.SessionID)
		}
	}

	logger.Info("turn/start: historical resume candidates",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"candidate_count", len(ids),
		"candidates", previewResumeCandidates(ids, 4),
	)
	return ids
}

func isHistoricalResumeCandidateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no rollout found for thread id"),
		strings.Contains(msg, "failed to load rollout"),
		strings.Contains(msg, "invalid thread id"),
		strings.Contains(msg, "thread/resume returned empty thread id"),
		strings.Contains(msg, "thread/resume returned empty response without fallback thread id"),
		strings.Contains(msg, "websocket: close 1006"),
		strings.Contains(msg, "abnormal closure"):
		return true
	default:
		return false
	}
}

// isCodexProcessCrashError 判断错误是否为 codex 进程 crash (需要 re-spawn)。
//
// 与 isHistoricalResumeCandidateError 的区别:
//   - candidateError: rollout 不可用但进程仍健在 → 直接尝试下一个候选
//   - crashError: 进程已死 → 必须 Stop + Launch 新进程后才能继续
func isCodexProcessCrashError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "websocket: close 1006") ||
		strings.Contains(msg, "abnormal closure")
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
	// D11: 总超时 45s，避免 Launch(30s)+Resume(30s) 串行导致前端 turn/start 永不回。
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New("Server.ensureThreadReady", "threadId is required")
	}
	launchCwd := strings.TrimSpace(cwd)
	if launchCwd == "" {
		launchCwd = "."
	}

	if proc := s.mgr.Get(id); proc != nil {
		logger.Info("turn/start: using running process",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			logger.FieldPort, proc.Client.GetPort(),
			"codex_thread_id", strings.TrimSpace(proc.Client.GetThreadID()),
		)
		s.registerBinding(ctx, id, proc)
		return proc, nil
	}
	hasHistory := s.threadExistsInHistory(ctx, id)
	if !hasHistory {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s not found", id)
	}
	resumeCandidates := make([]string, 0, 4)

	// 优先从 agent_codex_binding 表获取绑定的 codexThreadId (根基约束: 1:1 共生)。
	if s.bindingStore != nil {
		if binding, err := s.bindingStore.FindByAgentID(ctx, id); err == nil && binding != nil {
			resumeCandidates = append(resumeCandidates, binding.CodexThreadID)
			logger.Info("turn/start: found DB binding",
				logger.FieldAgentID, id,
				"bound_codex_thread_id", binding.CodexThreadID,
			)
		}
	}

	// Fallback: 如果 DB 无绑定, 仅尝试将输入 threadId 作为 codexThreadId 使用。
	if len(resumeCandidates) == 0 {
		if isLikelyCodexThreadID(id) {
			resumeCandidates = append(resumeCandidates, id)
		} else {
			resumeCandidates = append(resumeCandidates, s.resolveCodexThreadCandidates(ctx, id)...)
		}
	}

	logger.Info("turn/start: restoring historical thread",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"has_history", hasHistory,
		logger.FieldCwd, launchCwd,
		"candidate_count", len(resumeCandidates),
		"candidates", previewResumeCandidates(resumeCandidates, 4),
	)

	dynamicTools := s.buildAllDynamicTools()

	if err := s.mgr.Launch(ctx, id, id, "", launchCwd, s.resolveJsonRenderPrompt(ctx), dynamicTools); err != nil {
		// 并发补加载时可能已被其他请求拉起，二次确认后再报错。
		if proc := s.mgr.Get(id); proc != nil {
			return proc, nil
		}
		return nil, apperrors.Wrapf(err, "Server.ensureThreadReady", "auto-load thread %s", id)
	}

	proc := s.mgr.Get(id)
	if proc == nil {
		return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s loaded but not found", id)
	}
	logger.Info("turn/start: process launched for restore",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		logger.FieldPort, proc.Client.GetPort(),
		"codex_thread_id_before_resume", strings.TrimSpace(proc.Client.GetThreadID()),
	)
	if len(resumeCandidates) == 0 {
		logger.Warn("turn/start: no valid historical codex thread id, continue with fresh session",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
		)
		proc.MarkSessionLost()
		return proc, nil
	}
	var lastResumeErr error
	for _, resumeThreadID := range resumeCandidates {
		err := proc.Client.ResumeThread(codex.ResumeThreadRequest{
			ThreadID: resumeThreadID,
			Cwd:      launchCwd,
		})
		if err == nil {
			logger.Info("turn/start: historical thread auto-loaded",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				"codex_thread_id_after_resume", strings.TrimSpace(proc.Client.GetThreadID()),
				logger.FieldCwd, launchCwd,
			)
			s.registerBinding(ctx, id, proc)
			return proc, nil
		}

		lastResumeErr = err

		// codex 进程 crash → 直接返回错误，不偷偷换 fresh session
		if isCodexProcessCrashError(err) {
			logger.Error("turn/start: codex crashed during resume, returning error",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)
			_ = s.mgr.Stop(id)
			s.broadcastNotification(buildSessionLostNotification(id, err))
			return nil, apperrors.Wrapf(err, "Server.ensureThreadReady",
				"codex crashed while resuming thread %s (rollout=%s)", id, resumeThreadID)
		}

		// 非 crash 的候选错误 (rollout 不存在等) → 尝试下一个候选
		if isHistoricalResumeCandidateError(err) {
			logger.Warn("turn/start: resume candidate unavailable, try next",
				logger.FieldAgentID, id, logger.FieldThreadID, id,
				"resume_thread_id", resumeThreadID,
				logger.FieldError, err,
			)
			continue
		}

		// 不可识别的错误 → 也返回错误
		logger.Error("turn/start: unrecognized resume error",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			"resume_thread_id", resumeThreadID,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrapf(err, "Server.ensureThreadReady",
			"resume failed for thread %s (rollout=%s)", id, resumeThreadID)
	}

	// 所有候选的 rollout 都不可用 (非 crash) → fallback 到 fresh session + 通知前端
	if lastResumeErr != nil {
		logger.Warn("turn/start: all resume candidates exhausted, fallback to fresh session",
			logger.FieldAgentID, id, logger.FieldThreadID, id,
			"candidate_count", len(resumeCandidates),
			"last_error", lastResumeErr,
			logger.FieldCwd, launchCwd,
		)
		// proc 在 non-crash 路径中仍然存活，但可能被 mgr 移除
		if s.mgr.Get(id) == nil {
			_ = s.mgr.Stop(id)
			if launchErr := s.mgr.Launch(ctx, id, id, "", launchCwd, s.resolveJsonRenderPrompt(ctx), dynamicTools); launchErr != nil {
				return nil, apperrors.Wrapf(launchErr, "Server.ensureThreadReady", "final re-spawn thread %s", id)
			}
			proc = s.mgr.Get(id)
			if proc == nil {
				return nil, apperrors.Newf("Server.ensureThreadReady", "thread %s final re-spawn failed", id)
			}
		}
		proc.MarkSessionLost()
		s.broadcastNotification(buildSessionLostNotification(id, lastResumeErr))
		s.registerBinding(ctx, id, proc)
		return proc, nil
	}

	logger.Warn("turn/start: no available historical rollout, continue with fresh session",
		logger.FieldAgentID, id, logger.FieldThreadID, id,
		"candidate_count", len(resumeCandidates),
		logger.FieldCwd, launchCwd,
	)
	proc.MarkSessionLost()
	s.registerBinding(ctx, id, proc)
	return proc, nil
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
	codexThreadID := strings.TrimSpace(proc.Client.GetThreadID())
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
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New("Server.sendSlashCommand", "threadId is required")
	}
	if getProc != nil {
		if proc := getProc(id); proc != nil {
			return proc, nil
		}
	}
	if ensureReady == nil {
		return nil, apperrors.Newf("Server.sendSlashCommand", "thread %s not found", id)
	}
	proc, err := ensureReady(ctx, id, "")
	if err != nil {
		return nil, err
	}
	if proc == nil {
		return nil, apperrors.Newf("Server.sendSlashCommand", "thread %s not found", id)
	}
	return proc, nil
}

func (s *Server) resolveThreadForSlashCommand(ctx context.Context, threadID string) (*runner.AgentProcess, error) {
	if s == nil || s.mgr == nil {
		return nil, apperrors.New("Server.sendSlashCommand", "thread manager is not initialized")
	}
	return resolveSlashCommandThread(ctx, threadID, s.mgr.Get, s.ensureThreadReadyForTurn)
}

func (s *Server) sendSlashCommand(ctx context.Context, params json.RawMessage, command string) (any, error) {
	start := time.Now()
	var p threadIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, apperrors.Wrap(err, "Server.sendSlashCommand", "unmarshal params")
	}
	stateBefore := s.readThreadRuntimeState(p.ThreadID)
	activeTrackedBefore := s.hasActiveTrackedTurn(p.ThreadID)
	activeBefore := isInterruptActiveState(stateBefore)
	logger.Info("slash/command: request",
		logger.FieldAgentID, p.ThreadID,
		logger.FieldThreadID, p.ThreadID,
		logger.FieldCommand, command,
		logger.FieldParamsLen, len(params),
		"state_before", stateBefore,
		"active_before", activeBefore,
		"active_tracked_before", activeTrackedBefore,
	)
	if strings.EqualFold(strings.TrimSpace(command), "/compact") && (activeBefore || activeTrackedBefore) {
		logger.Warn("thread/compact/start requested while turn active; compact may be ignored by codex",
			logger.FieldAgentID, p.ThreadID,
			logger.FieldThreadID, p.ThreadID,
			"state_before", stateBefore,
			"active_before", activeBefore,
			"active_tracked_before", activeTrackedBefore,
		)
	}
	proc, err := s.resolveThreadForSlashCommand(ctx, p.ThreadID)
	if err != nil {
		logger.Warn("slash/command: resolve thread failed",
			logger.FieldAgentID, p.ThreadID,
			logger.FieldThreadID, p.ThreadID,
			logger.FieldCommand, command,
			logger.FieldError, err,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return nil, err
	}
	if err := proc.Client.SendCommand(command, ""); err != nil {
		logger.Warn("slash/command: send failed",
			logger.FieldAgentID, p.ThreadID,
			logger.FieldThreadID, p.ThreadID,
			logger.FieldCommand, command,
			logger.FieldError, err,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return nil, err
	}
	logger.Info("slash/command: sent",
		logger.FieldAgentID, p.ThreadID,
		logger.FieldThreadID, p.ThreadID,
		logger.FieldCommand, command,
		"codex_thread_id", strings.TrimSpace(proc.Client.GetThreadID()),
		logger.FieldPort, proc.Client.GetPort(),
		logger.FieldDurationMS, time.Since(start).Milliseconds(),
	)
	return map[string]any{}, nil
}

// sendSlashCommandWithArgs 带参数的斜杠命令。
func (s *Server) sendSlashCommandWithArgs(params json.RawMessage, command string, argsField string) (any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(params, &raw); err != nil {
		return nil, apperrors.Wrap(err, "Server.sendSlashCommandWithArgs", "unmarshal params")
	}

	var threadID string
	if v, ok := raw["threadId"]; ok {
		if err := json.Unmarshal(v, &threadID); err != nil {
			return nil, apperrors.Wrap(err, "Server.sendSlashCommandWithArgs", "unmarshal threadId")
		}
	}
	if threadID == "" {
		return nil, apperrors.New("Server.sendSlashCommandWithArgs", "threadId is required")
	}

	var args string
	if v, ok := raw[argsField]; ok {
		_ = json.Unmarshal(v, &args)
	}

	return s.withThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		if err := proc.Client.SendCommand(command, args); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

// ========================================
// 输入/附件解析
// ========================================

// extractInputs 从 UserInput 数组提取 prompt/images/files。
func extractInputs(inputs []UserInput) (prompt string, images, files []string) {
	var texts []string
	isRemoteImageURL := func(raw string) bool {
		value := strings.ToLower(strings.TrimSpace(raw))
		return strings.HasPrefix(value, "http://") ||
			strings.HasPrefix(value, "https://") ||
			strings.HasPrefix(value, "data:image/")
	}
	for _, inp := range inputs {
		switch strings.ToLower(strings.TrimSpace(inp.Type)) {
		case "text":
			texts = append(texts, inp.Text)
		case "image":
			if value := strings.TrimSpace(inp.URL); value != "" {
				images = append(images, value)
				continue
			}
			if value := strings.TrimSpace(inp.Path); value != "" {
				images = append(images, value)
			}
		case "localimage":
			if value := strings.TrimSpace(inp.URL); isRemoteImageURL(value) {
				images = append(images, value)
				continue
			}
			if value := strings.TrimSpace(inp.Path); value != "" {
				images = append(images, value)
			}
		case "filecontent":
			if value := strings.TrimSpace(inp.Path); value != "" {
				files = append(files, value)
				continue
			}
			if inline := fileContentInputText(inp.Name, inp.Content); inline != "" {
				texts = append(texts, inline)
			}
		case "mention", "file":
			if value := strings.TrimSpace(inp.Path); value != "" {
				files = append(files, value)
			}
		case "skill":
			// 技能注入统一由 turn/start|steer 的 selectedSkills 处理，避免透传输入中的摘要内容。
			continue
		}
	}
	prompt = strings.Join(texts, "\n")
	return
}

func buildAttachmentName(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:image/") {
		ext := strings.TrimSpace(strings.TrimPrefix(lower, "data:image/"))
		if idx := strings.Index(ext, ";"); idx >= 0 {
			ext = ext[:idx]
		}
		ext = strings.TrimSpace(ext)
		if ext == "" {
			return "image"
		}
		return "image." + ext
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if parsed, err := url.Parse(value); err == nil {
			base := strings.TrimSpace(filepath.Base(parsed.Path))
			if base != "" && base != "." && base != string(filepath.Separator) {
				return base
			}
		}
		return value
	}
	base := strings.TrimSpace(filepath.Base(value))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return value
	}
	return base
}

func buildAttachmentPreviewURL(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:image/") ||
		strings.HasPrefix(lower, "file://") {
		return value
	}
	return (&url.URL{Scheme: "file", Path: value}).String()
}

func buildUserTimelineAttachments(images, files []string) []uistate.TimelineAttachment {
	attachments := make([]uistate.TimelineAttachment, 0, len(images)+len(files))
	for _, raw := range images {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = append(attachments, uistate.TimelineAttachment{
			Kind:       "image",
			Name:       buildAttachmentName(path),
			Path:       path,
			PreviewURL: buildAttachmentPreviewURL(path),
		})
	}
	for _, raw := range files {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		attachments = append(attachments, uistate.TimelineAttachment{
			Kind: "file",
			Name: buildAttachmentName(path),
			Path: path,
		})
	}
	return attachments
}

func buildUserTimelineAttachmentsFromInputs(inputs []UserInput) []uistate.TimelineAttachment {
	if len(inputs) == 0 {
		return nil
	}
	attachments := make([]uistate.TimelineAttachment, 0, len(inputs))
	for _, input := range inputs {
		kind := strings.ToLower(strings.TrimSpace(input.Type))
		switch kind {
		case "image":
			imageURL := strings.TrimSpace(input.URL)
			if imageURL == "" {
				imageURL = strings.TrimSpace(input.Path)
			}
			if imageURL == "" {
				continue
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind:       "image",
				Name:       buildAttachmentName(imageURL),
				Path:       imageURL,
				PreviewURL: buildAttachmentPreviewURL(imageURL),
			})
		case "localimage":
			imagePath := strings.TrimSpace(input.Path)
			preview := strings.TrimSpace(input.URL)
			if preview == "" {
				preview = imagePath
			}
			if preview == "" {
				continue
			}
			nameSource := imagePath
			if nameSource == "" {
				nameSource = preview
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind:       "image",
				Name:       buildAttachmentName(nameSource),
				Path:       imagePath,
				PreviewURL: buildAttachmentPreviewURL(preview),
			})
		case "mention", "file":
			path := strings.TrimSpace(input.Path)
			if path == "" {
				continue
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind: "file",
				Name: buildAttachmentName(path),
				Path: path,
			})
		case "filecontent":
			path := strings.TrimSpace(input.Path)
			if path != "" {
				attachments = append(attachments, uistate.TimelineAttachment{
					Kind: "file",
					Name: buildAttachmentName(path),
					Path: path,
				})
				continue
			}
			if strings.TrimSpace(input.Content) == "" {
				continue
			}
			name := strings.TrimSpace(input.Name)
			if name == "" {
				name = "inline-file"
			}
			attachments = append(attachments, uistate.TimelineAttachment{
				Kind: "file",
				Name: name,
			})
		}
	}
	return attachments
}

// ========================================
// §10 斜杠命令 handlers
// ========================================

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

// threadSkillsList 列出 Skills (/skills)。
func (s *Server) threadSkillsList(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/skills")
}

// threadDebugMemory 调试记忆 (/debug-m-drop 或 /debug-m-update)。
func (s *Server) threadDebugMemory(_ context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommandWithArgs(params, "/debug-m-drop", "action")
}

// ========================================
// Debug 运行时诊断
// ========================================

func (s *Server) debugRuntime(_ context.Context, _ json.RawMessage) (any, error) {
	var mem goruntime.MemStats
	goruntime.ReadMemStats(&mem)

	result := map[string]any{
		"go": map[string]any{
			"goroutines":     goruntime.NumGoroutine(),
			"heapAllocMB":    float64(mem.HeapAlloc) / 1024 / 1024,
			"heapSysMB":      float64(mem.HeapSys) / 1024 / 1024,
			"heapInuseMB":    float64(mem.HeapInuse) / 1024 / 1024,
			"heapObjects":    mem.HeapObjects,
			"sysMB":          float64(mem.Sys) / 1024 / 1024,
			"gcCycles":       mem.NumGC,
			"gcTotalPauseMs": float64(mem.PauseTotalNs) / 1e6,
			"gcLastPauseMs":  float64(mem.PauseNs[(mem.NumGC+255)%256]) / 1e6,
			"stackInuseMB":   float64(mem.StackInuse) / 1024 / 1024,
			"mallocs":        mem.Mallocs,
			"frees":          mem.Frees,
			"liveObjects":    mem.Mallocs - mem.Frees,
			"nextGCMB":       float64(mem.NextGC) / 1024 / 1024,
			"gcCPUPercent":   mem.GCCPUFraction * 100,
		},
	}

	if s.uiRuntime != nil {
		result["timeline"] = s.uiRuntime.TimelineStats()
	}

	return result, nil
}

func (s *Server) debugForceGC(_ context.Context, _ json.RawMessage) (any, error) {
	var before goruntime.MemStats
	goruntime.ReadMemStats(&before)

	goruntime.GC()

	var after goruntime.MemStats
	goruntime.ReadMemStats(&after)

	return map[string]any{
		"before": map[string]any{
			"heapAllocMB": float64(before.HeapAlloc) / 1024 / 1024,
			"heapObjects": before.HeapObjects,
			"liveObjects": before.Mallocs - before.Frees,
		},
		"after": map[string]any{
			"heapAllocMB": float64(after.HeapAlloc) / 1024 / 1024,
			"heapObjects": after.HeapObjects,
			"liveObjects": after.Mallocs - after.Frees,
		},
		"freedMB":      float64(before.HeapAlloc-after.HeapAlloc) / 1024 / 1024,
		"freedObjects": int64(before.HeapObjects) - int64(after.HeapObjects),
		"gcCycles":     after.NumGC,
	}, nil
}

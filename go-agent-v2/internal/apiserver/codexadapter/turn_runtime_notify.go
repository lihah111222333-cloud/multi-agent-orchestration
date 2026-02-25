package codexadapter

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func (a *Adapter) registerBinding(ctx context.Context, agentID string, proc *runner.AgentProcess) {
	if a == nil || a.ctx == nil || a.ctx.BindingStore == nil || proc == nil || proc.Client == nil {
		return
	}
	codexThreadID := a.GetThreadID(proc)
	if codexThreadID == "" {
		return
	}
	if err := a.ctx.BindingStore.Bind(ctx, agentID, codexThreadID, ""); err != nil {
		logger.Warn("turn/start: failed to register binding",
			logger.FieldAgentID, agentID,
			"codex_thread_id", codexThreadID,
			logger.FieldError, err,
		)
	}
}

func (a *Adapter) notifySessionLost(agentID string, lastErr error) {
	if a == nil || a.ctx == nil {
		return
	}
	method, payload := BuildSessionLostNotification(agentID, lastErr)
	a.ctx.Notify(method, payload)
}

// BuildSessionLostNotification builds "session lost" fallback notification payload.
func BuildSessionLostNotification(agentID string, lastErr error) (string, map[string]any) {
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

// with_thread.go — thread 进程解析与统一调用包装。
package codexadapter

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

func (a *Adapter) resolveProcess(caller, threadID string) (*runner.AgentProcess, error) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New(caller, "threadId is required")
	}
	if a == nil || a.ctx == nil || a.ctx.Manager() == nil {
		return nil, apperrors.New(caller, "thread resolver is not configured")
	}
	proc := a.ctx.Manager().Get(id)
	if proc == nil {
		return nil, apperrors.Newf(caller, "thread %s not found", id)
	}
	return proc, nil
}

func withProcess[T any](
	a *Adapter,
	caller string,
	threadID string,
	fn func(*runner.AgentProcess) (T, error),
) (T, error) {
	var zero T
	proc, err := a.resolveProcess(caller, threadID)
	if err != nil {
		return zero, err
	}
	return fn(proc)
}

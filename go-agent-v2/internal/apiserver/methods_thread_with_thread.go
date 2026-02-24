package apiserver

import (
	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// withThread 查找线程并执行回调 (消除重复的 getThread→notFound 样板)。
func (s *Server) withThread(threadID string, fn func(*runner.AgentProcess) (any, error)) (any, error) {
	proc := s.mgr.Get(threadID)
	if proc == nil {
		return nil, apperrors.Newf("Server.withThread", "thread %s not found", threadID)
	}
	return fn(proc)
}

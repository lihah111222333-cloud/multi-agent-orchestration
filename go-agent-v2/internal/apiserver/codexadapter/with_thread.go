// with_thread.go — 泛型 WithThread 包装器 (DRY: E1)。
//
// 消除 thread_turn_entry.go 中 7 处重复的:
//
//	validate → WithThread → type-assert 样板。
package codexadapter

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// WithThreadFunc 是 Server.withThread 的函数签名。
type WithThreadFunc = func(string, func(*runner.AgentProcess) (any, error)) (any, error)

// withThreadTyped 封装 validate → WithThread → type-assert 样板。
//
// 使用 Go 泛型消除 7 处 ~20 行的重复代码，每个调用点只需写核心逻辑。
func withThreadTyped[T any](
	threadID string,
	withThread WithThreadFunc,
	caller string,
	fn func(*runner.AgentProcess) (T, error),
) (T, error) {
	var zero T
	id := strings.TrimSpace(threadID)
	if id == "" {
		return zero, apperrors.New(caller, "threadId is required")
	}
	if withThread == nil {
		return zero, apperrors.New(caller, "thread resolver is not configured")
	}
	out, err := withThread(id, func(p *runner.AgentProcess) (any, error) {
		return fn(p)
	})
	if err != nil {
		return zero, err
	}
	result, ok := out.(T)
	if !ok {
		return zero, apperrors.Newf(caller, "unexpected result type %T", out)
	}
	return result, nil
}

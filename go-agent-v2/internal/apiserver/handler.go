// handler.go — 泛型 JSON-RPC handler 包装器。
//
// 消除每个方法中重复的 json.Unmarshal + error wrap 样板代码。
// 用法:
//
//	s.methods["thread/resume"] = typedHandler(s.threadResume)
//	func (s *Server) threadResume(ctx context.Context, p threadResumeParams) (any, error) { ... }
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
)

// typedHandler 将强类型函数包装为 Handler (json.RawMessage → 泛型参数自动解析)。
//
// 功能:
//   - nil params → 使用零值 struct
//   - 无效 JSON → 返回 "invalid params" 错误
//   - handler 签名即文档, 类型安全
func typedHandler[P any](fn func(ctx context.Context, p P) (any, error)) Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p P
		if raw != nil {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
		}
		return fn(ctx, p)
	}
}

// noopHandler 返回空 map 的 handler (协议要求注册但暂无实现)。
func noopHandler() Handler {
	return func(_ context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{}, nil
	}
}

// methods_account.go — 账号管理 JSON-RPC 方法 (登录/登出/读取)。
package apiserver

import (
	"context"
	"encoding/json"
	"os"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// accountLoginStartParams account/login/start 请求参数。
type accountLoginStartParams struct {
	AuthMode string `json:"authMode"`
	APIKey   string `json:"apiKey,omitempty"`
}

func (s *Server) accountLoginStartTyped(_ context.Context, p accountLoginStartParams) (any, error) {
	if p.APIKey != "" {
		if err := os.Setenv("OPENAI_API_KEY", p.APIKey); err != nil {
			logger.Warn("account/login: setenv failed", logger.FieldError, err)
			return nil, apperrors.Wrap(err, "Server.accountLoginStart", "setenv OPENAI_API_KEY")
		}
		return map[string]any{}, nil
	}
	return map[string]any{"loginUrl": "https://platform.openai.com/api-keys"}, nil
}

func (s *Server) accountLoginCancel(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{}, nil
}

func (s *Server) accountLogout(_ context.Context, _ json.RawMessage) (any, error) {
	if err := os.Unsetenv("OPENAI_API_KEY"); err != nil {
		logger.Warn("account/logout: unsetenv failed", logger.FieldError, err)
	}
	return map[string]any{}, nil
}

func (s *Server) accountRead(_ context.Context, _ json.RawMessage) (any, error) {
	key := os.Getenv("OPENAI_API_KEY")
	masked := ""
	if len(key) > 8 {
		masked = key[:4] + "..." + key[len(key)-4:]
	}
	return map[string]any{
		"account": map[string]any{
			"hasApiKey": key != "",
			"maskedKey": masked,
		},
	}, nil
}

// accountRateLimitsRead 读取速率限制。
func (s *Server) accountRateLimitsRead(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"rateLimits": map[string]any{}}, nil
}

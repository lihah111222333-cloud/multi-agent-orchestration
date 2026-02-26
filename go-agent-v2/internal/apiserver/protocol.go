// protocol.go — JSON-RPC 2.0 协议类型定义。
//
// 规范: https://www.jsonrpc.org/specification
//
//	Request:      {"jsonrpc":"2.0", "id":1, "method":"...", "params":{...}}
//	Response:     {"jsonrpc":"2.0", "id":1, "result":{...}}
//	Error:        {"jsonrpc":"2.0", "id":1, "error":{"code":..., "message":"..."}}
//	Notification: {"jsonrpc":"2.0", "method":"...", "params":{...}}
package apiserver

import (
	"context"
	"encoding/json"
	"strings"

	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

const jsonrpcVersion = "2.0"

// Request JSON-RPC 2.0 请求。
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response JSON-RPC 2.0 响应。
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// Notification JSON-RPC 2.0 通知 (无 id, 服务端主动推送)。
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// RPCError JSON-RPC 2.0 错误。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// 标准 JSON-RPC 2.0 错误码。
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeOverloaded     = -32001
)

// --- 便捷构造函数 ---

// newResult 成功响应。
func newResult(id any, result any) *Response {
	return &Response{JSONRPC: jsonrpcVersion, ID: id, Result: result}
}

func normalizeInternalErrorCode(code int, msg string) int {
	if code != CodeInternalError {
		return code
	}
	text := strings.ToLower(strings.TrimSpace(msg))
	if strings.Contains(text, "invalid params") {
		return CodeInvalidParams
	}
	if isLikelyInvalidParamsMessage(text) {
		return CodeInvalidParams
	}
	return code
}

func isLikelyInvalidParamsMessage(text string) bool {
	if text == "" {
		return false
	}
	if strings.Contains(text, " is required") {
		return true
	}
	if strings.Contains(text, "must not be empty") {
		return true
	}
	if strings.Contains(text, "invalid cursor") {
		return true
	}
	if strings.Contains(text, "invalid thread id") {
		return true
	}
	if strings.Contains(text, "input must not be empty") {
		return true
	}
	if strings.Contains(text, "no active turn") {
		return true
	}
	if strings.Contains(text, "expectedturnid mismatch") {
		return true
	}
	if strings.Contains(text, "expected active turn id") {
		return true
	}
	if strings.Contains(text, "turn not found") {
		return true
	}
	if strings.Contains(text, "unknown turn") {
		return true
	}
	if strings.Contains(text, "thread ") && strings.Contains(text, " not found") {
		return true
	}
	if strings.Contains(text, "invalid_request_id") {
		return true
	}
	return false
}

// newError 错误响应。
func newError(id any, code int, msg string) *Response {
	normalizedMessage := strings.TrimSpace(msg)
	normalizedCode := normalizeInternalErrorCode(code, normalizedMessage)
	return &Response{JSONRPC: jsonrpcVersion, ID: id, Error: &RPCError{Code: normalizedCode, Message: normalizedMessage}}
}

// newErrorData 带 data 的错误响应。
func newErrorData(id any, code int, msg string, data any) *Response {
	normalizedMessage := strings.TrimSpace(msg)
	normalizedCode := normalizeInternalErrorCode(code, normalizedMessage)
	return &Response{JSONRPC: jsonrpcVersion, ID: id, Error: &RPCError{Code: normalizedCode, Message: normalizedMessage, Data: data}}
}

// newNotification 通知。
func newNotification(method string, params any) *Notification {
	return &Notification{JSONRPC: jsonrpcVersion, Method: method, Params: params}
}

// ========================================
// Handler 包装器 (原 handler.go)
// ========================================

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
				return nil, pkgerr.Wrap(err, "TypedHandler", "invalid params")
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

// stubHandler 返回固定值的 handler (前端兼容 — 空数据占位)。
func stubHandler(result any) Handler {
	return func(_ context.Context, _ json.RawMessage) (any, error) {
		return result, nil
	}
}

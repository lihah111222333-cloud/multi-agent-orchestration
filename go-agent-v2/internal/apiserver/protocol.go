package apiserver

import (
	"context"
	"encoding/json"
	"strings"

	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

const jsonrpcVersion = "2.0"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeOverloaded     = -32001
)

func newResult(id any, result any) *Response {
	return &Response{JSONRPC: jsonrpcVersion, ID: id, Result: result}
}

func normalizeInternalErrorCode(code int, msg string) int {
	if code == CodeInternalError {
		text := strings.ToLower(strings.TrimSpace(msg))
		if strings.Contains(text, "invalid params") || isLikelyInvalidParamsMessage(text) {
			return CodeInvalidParams
		}
	}
	return code
}

var invalidParamsMarkers = [...]string{
	" is required",
	"must not be empty",
	"invalid cursor",
	"invalid thread id",
	"input must not be empty",
	"no active turn",
	"expectedturnid mismatch",
	"expected active turn id",
	"turn not found",
	"unknown turn",
	"invalid_request_id",
}

func isLikelyInvalidParamsMessage(text string) bool {
	if text == "" {
		return false
	}
	for _, marker := range invalidParamsMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return strings.Contains(text, "thread ") && strings.Contains(text, " not found")
}

func newError(id any, code int, msg string) *Response { return newErrorData(id, code, msg, nil) }

func newErrorData(id any, code int, msg string, data any) *Response {
	normalizedMessage := strings.TrimSpace(msg)
	normalizedCode := normalizeInternalErrorCode(code, normalizedMessage)
	return &Response{JSONRPC: jsonrpcVersion, ID: id, Error: &RPCError{Code: normalizedCode, Message: normalizedMessage, Data: data}}
}

func newNotification(method string, params any) *Notification {
	return &Notification{JSONRPC: jsonrpcVersion, Method: method, Params: params}
}

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

func noopHandler() Handler {
	return func(_ context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{}, nil
	}
}

func stubHandler(result any) Handler {
	return func(_ context.Context, _ json.RawMessage) (any, error) {
		return result, nil
	}
}

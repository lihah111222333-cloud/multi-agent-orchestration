// protocol.go — JSON-RPC 2.0 协议类型定义。
//
// 规范: https://www.jsonrpc.org/specification
//
//	Request:      {"jsonrpc":"2.0", "id":1, "method":"...", "params":{...}}
//	Response:     {"jsonrpc":"2.0", "id":1, "result":{...}}
//	Error:        {"jsonrpc":"2.0", "id":1, "error":{"code":..., "message":"..."}}
//	Notification: {"jsonrpc":"2.0", "method":"...", "params":{...}}
package apiserver

import "encoding/json"

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
)

// --- 便捷构造函数 ---

// newResult 成功响应。
func newResult(id any, result any) *Response {
	return &Response{JSONRPC: jsonrpcVersion, ID: id, Result: result}
}

// newError 错误响应。
func newError(id any, code int, msg string) *Response {
	return &Response{JSONRPC: jsonrpcVersion, ID: id, Error: &RPCError{Code: code, Message: msg}}
}

// newErrorData 带 data 的错误响应。
func newErrorData(id any, code int, msg string, data any) *Response {
	return &Response{JSONRPC: jsonrpcVersion, ID: id, Error: &RPCError{Code: code, Message: msg, Data: data}}
}

// newNotification 通知。
func newNotification(method string, params any) *Notification {
	return &Notification{JSONRPC: jsonrpcVersion, Method: method, Params: params}
}

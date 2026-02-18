// Package errors 提供统一错误类型与哨兵错误，遵循 wjboot-v2 三层错误体系。
//
// 本包为 go-agent-v2 精简版:
//   - L1 哨兵错误: ErrNotFound / ErrInvalidInput / ErrTimeout 等
//   - L2 AppError: 带 Op + Code + Message 的应用级错误 (替代 EngineError)
package errors

import (
	"errors"
	"fmt"
)

// ========================================
// L1 哨兵错误 (Sentinel Errors)
// ========================================

var (
	// ErrNotFound 资源不存在
	ErrNotFound = errors.New("not found")

	// ErrInvalidInput 输入参数无效
	ErrInvalidInput = errors.New("invalid input")

	// ErrUnauthorized 未授权
	ErrUnauthorized = errors.New("unauthorized")

	// ErrInternal 内部错误
	ErrInternal = errors.New("internal error")

	// ErrTimeout 操作超时
	ErrTimeout = errors.New("timeout")

	// ErrRowMissing 数据库查询未返回预期行 (对应 Python RowMissingError)
	ErrRowMissing = errors.New("row missing")

	// ErrReadOnly 只读查询校验失败
	ErrReadOnly = errors.New("read-only violation")
)

// ========================================
// L2 AppError (应用级错误)
// ========================================

// AppError 应用级错误，带操作上下文。
// 对应 wjboot-v2 EngineError，但更通用。
type AppError struct {
	Op      string // 操作名，如 "Store.CreateInteraction"
	Code    string // 错误码，如 "DB_ERROR"、"VALIDATION"
	Message string // 人类可读消息
	Err     error  // 原始错误
}

// Error 实现 error 接口。
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

// Unwrap 支持 errors.Is / errors.As 链式查找。
func (e *AppError) Unwrap() error {
	return e.Err
}

// ========================================
// 工厂函数
// ========================================

// New 创建无原因链的应用错误。
func New(op, message string) error {
	return &AppError{Op: op, Message: message}
}

// Newf 创建带格式化消息的应用错误。
func Newf(op, format string, args ...any) error {
	return &AppError{Op: op, Message: fmt.Sprintf(format, args...)}
}

// Wrap 包装错误并附加操作上下文。
func Wrap(err error, op string, message string) error {
	return &AppError{Op: op, Message: message, Err: err}
}

// Wrapf 用格式化消息包装错误。
func Wrapf(err error, op, format string, args ...any) error {
	return &AppError{Op: op, Message: fmt.Sprintf(format, args...), Err: err}
}

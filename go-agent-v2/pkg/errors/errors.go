package errors

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidInput = errors.New("invalid input")
	ErrUnauthorized = errors.New("unauthorized")
	ErrInternal     = errors.New("internal error")
	ErrTimeout      = errors.New("timeout")
	ErrRowMissing   = errors.New("row missing")
	ErrReadOnly     = errors.New("read-only violation")
)

type AppError struct {
	Op      string
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Op, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
}

func (e *AppError) Unwrap() error  { return e.Err }
func New(op, message string) error { return &AppError{Op: op, Message: message} }
func Newf(op, format string, args ...any) error {
	return &AppError{Op: op, Message: fmt.Sprintf(format, args...)}
}
func Wrap(err error, op string, message string) error {
	return &AppError{Op: op, Message: message, Err: err}
}
func Wrapf(err error, op, format string, args ...any) error {
	return &AppError{Op: op, Message: fmt.Sprintf(format, args...), Err: err}
}

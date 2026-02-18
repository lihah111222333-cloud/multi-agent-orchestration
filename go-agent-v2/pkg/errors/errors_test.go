// errors_test.go — 验证 AppError / Wrap / Wrapf 的行为契约。
package errors

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// TestWrapUnwrap 验证 Wrap 保留原始错误链，errors.Is 和 errors.As 正常工作。
func TestWrapUnwrap(t *testing.T) {
	original := ErrNotFound
	wrapped := Wrap(original, "Store.Get", "user not found")

	// errors.Is 能通过 Wrap 找到哨兵错误
	if !errors.Is(wrapped, ErrNotFound) {
		t.Errorf("errors.Is(wrapped, ErrNotFound) = false, want true")
	}

	// errors.Is 对不相关错误返回 false
	if errors.Is(wrapped, ErrTimeout) {
		t.Errorf("errors.Is(wrapped, ErrTimeout) = true, want false")
	}

	// errors.As 能提取 AppError
	var appErr *AppError
	if !errors.As(wrapped, &appErr) {
		t.Fatalf("errors.As failed to extract *AppError")
	}
	if appErr.Op != "Store.Get" {
		t.Errorf("Op = %q, want %q", appErr.Op, "Store.Get")
	}
	if appErr.Message != "user not found" {
		t.Errorf("Message = %q, want %q", appErr.Message, "user not found")
	}
}

// TestWrapErrorString 验证 Error() 输出包含 op、message 和 cause。
func TestWrapErrorString(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	wrapped := Wrap(cause, "Service.Read", "read failed")

	s := wrapped.Error()
	for _, want := range []string{"Service.Read", "read failed", "unexpected EOF"} {
		if !strings.Contains(s, want) {
			t.Errorf("Error() = %q, missing %q", s, want)
		}
	}
}

// TestWrapfFormat 验证 Wrapf 格式化消息。
func TestWrapfFormat(t *testing.T) {
	cause := ErrInvalidInput
	wrapped := Wrapf(cause, "API.Validate", "field %s invalid: %d", "age", -1)

	var appErr *AppError
	if !errors.As(wrapped, &appErr) {
		t.Fatal("errors.As failed")
	}
	if !strings.Contains(appErr.Message, "field age invalid: -1") {
		t.Errorf("Message = %q, want to contain 'field age invalid: -1'", appErr.Message)
	}
}

// TestNewWithoutCause 验证 New 创建无 cause 的错误。
func TestNewWithoutCause(t *testing.T) {
	err := New("Init", "failed to start")
	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatal("errors.As failed")
	}
	if appErr.Err != nil {
		t.Errorf("Err = %v, want nil", appErr.Err)
	}
	// Unwrap 返回 nil
	if errors.Unwrap(err) != nil {
		t.Errorf("Unwrap = %v, want nil", errors.Unwrap(err))
	}
}

// TestDoubleWrap 验证二次包装时 errors.Is 仍能找到最深层哨兵。
func TestDoubleWrap(t *testing.T) {
	inner := Wrap(ErrNotFound, "Store.Get", "row missing")
	outer := Wrap(inner, "Service.FindUser", "user lookup failed")

	if !errors.Is(outer, ErrNotFound) {
		t.Error("errors.Is(outer, ErrNotFound) = false after double wrap")
	}

	var appErr *AppError
	if !errors.As(outer, &appErr) {
		t.Fatal("errors.As failed on outer")
	}
	if appErr.Op != "Service.FindUser" {
		t.Errorf("Op = %q, want Service.FindUser", appErr.Op)
	}
}

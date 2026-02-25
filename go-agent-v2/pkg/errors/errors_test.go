package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestNew(t *testing.T) {
	err := New("Store.Create", "record already exists")
	if err == nil {
		t.Fatal("New() returned nil")
	}
	if err.Error() != "Store.Create: record already exists" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestNewf(t *testing.T) {
	err := Newf("Manager.Launch", "agent %s already exists", "a-1")
	want := "Manager.Launch: agent a-1 already exists"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestWrap(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := Wrap(cause, "DB.Connect", "failed to connect")
	want := "DB.Connect: failed to connect: connection refused"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestWrapf(t *testing.T) {
	cause := fmt.Errorf("timeout")
	err := Wrapf(cause, "API.Call", "request to %s failed", "example.com")
	want := "API.Call: request to example.com failed: timeout"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestUnwrap(t *testing.T) {
	cause := ErrNotFound
	err := Wrap(cause, "Store.Get", "user not found")

	if !errors.Is(err, ErrNotFound) {
		t.Fatal("errors.Is(err, ErrNotFound) should be true")
	}

	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatal("errors.As(err, &AppError) should succeed")
	}
	if appErr.Op != "Store.Get" {
		t.Fatalf("Op = %q, want %q", appErr.Op, "Store.Get")
	}
}

func TestSentinelErrors(t *testing.T) {
	sentinels := []error{
		ErrNotFound,
		ErrInvalidInput,
		ErrUnauthorized,
		ErrInternal,
		ErrTimeout,
		ErrRowMissing,
		ErrReadOnly,
	}
	for _, s := range sentinels {
		if s == nil {
			t.Fatal("sentinel error is nil")
		}
		if s.Error() == "" {
			t.Fatal("sentinel error has empty message")
		}
	}
}

func TestNewWithoutCause(t *testing.T) {
	err := New("Op", "msg")
	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatal("should be AppError")
	}
	if appErr.Unwrap() != nil {
		t.Fatal("Unwrap() should be nil for New() errors")
	}
}

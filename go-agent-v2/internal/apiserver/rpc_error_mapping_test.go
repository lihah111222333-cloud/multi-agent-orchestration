package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

func TestNewErrorCodeNormalization(t *testing.T) {
	invalidParams := newError(1, CodeInternalError, "TypedHandler: invalid params: bad json")
	if invalidParams == nil || invalidParams.Error == nil {
		t.Fatalf("newError() returned nil error payload")
	}
	if invalidParams.Error.Code != CodeInvalidParams {
		t.Fatalf("invalid params code = %d, want %d", invalidParams.Error.Code, CodeInvalidParams)
	}

	invalidRequest := newError(1, CodeInternalError, "Server.turnSteer: expectedTurnId must not be empty")
	if invalidRequest.Error.Code != CodeInvalidParams {
		t.Fatalf("invalid params code = %d, want %d", invalidRequest.Error.Code, CodeInvalidParams)
	}

	internal := newError(1, CodeInternalError, "database unavailable")
	if internal.Error.Code != CodeInternalError {
		t.Fatalf("internal code = %d, want %d", internal.Error.Code, CodeInternalError)
	}
}

func TestDispatchRequestErrorCodeMapping(t *testing.T) {
	s := &Server{methods: map[string]Handler{
		"invalid_params": func(context.Context, json.RawMessage) (any, error) {
			return nil, pkgerr.Wrap(errors.New("bad type"), "TypedHandler", "invalid params")
		},
		"invalid_request": func(context.Context, json.RawMessage) (any, error) {
			return nil, pkgerr.New("Server.turnSteer", "expectedTurnId must not be empty")
		},
		"internal": func(context.Context, json.RawMessage) (any, error) {
			return nil, errors.New("boom")
		},
	}}

	resp := dispatchRequest(s, context.Background(), int64(1), "invalid_params", nil)
	if resp == nil || resp.Error == nil {
		t.Fatalf("dispatchRequest(invalid_params) missing error response")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Fatalf("dispatchRequest(invalid_params) code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}

	resp = dispatchRequest(s, context.Background(), int64(2), "invalid_request", nil)
	if resp == nil || resp.Error == nil {
		t.Fatalf("dispatchRequest(invalid_request) missing error response")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Fatalf("dispatchRequest(invalid_request) code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}

	resp = dispatchRequest(s, context.Background(), int64(3), "internal", nil)
	if resp == nil || resp.Error == nil {
		t.Fatalf("dispatchRequest(internal) missing error response")
	}
	if resp.Error.Code != CodeInternalError {
		t.Fatalf("dispatchRequest(internal) code = %d, want %d", resp.Error.Code, CodeInternalError)
	}
}

func TestDispatchRequestUnknownMethodUsesMethodNotFoundCode(t *testing.T) {
	s := &Server{methods: map[string]Handler{}}
	resp := dispatchRequest(s, context.Background(), int64(99), "unknown/method", nil)
	if resp == nil || resp.Error == nil {
		t.Fatalf("dispatchRequest(unknown) missing error response")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("dispatchRequest(unknown) code = %d, want %d", resp.Error.Code, CodeMethodNotFound)
	}
}

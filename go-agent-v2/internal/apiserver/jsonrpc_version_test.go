package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidIncomingJSONRPCVersion(t *testing.T) {
	if !validIncomingJSONRPCVersion("2.0") {
		t.Fatalf("validIncomingJSONRPCVersion(2.0) = false, want true")
	}
	if validIncomingJSONRPCVersion("") {
		t.Fatalf("validIncomingJSONRPCVersion(empty) = true, want false")
	}
	if validIncomingJSONRPCVersion("1.0") {
		t.Fatalf("validIncomingJSONRPCVersion(1.0) = true, want false")
	}
}

func TestHandleHTTPRPCRejectsInvalidJSONRPCVersion(t *testing.T) {
	s := &Server{methods: map[string]Handler{
		"ping": func(context.Context, json.RawMessage) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	}}

	body := []byte(`{"jsonrpc":"1.0","id":1,"method":"ping","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleHTTPRPC(s, rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	errPayload, _ := payload["error"].(map[string]any)
	if got := int(errPayload["code"].(float64)); got != CodeInvalidRequest {
		t.Fatalf("error.code = %d, want %d", got, CodeInvalidRequest)
	}
}

func TestHandleHTTPRPCAcceptsJSONRPC20(t *testing.T) {
	s := &Server{methods: map[string]Handler{
		"ping": func(context.Context, json.RawMessage) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	}}

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleHTTPRPC(s, rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error"] != nil {
		t.Fatalf("unexpected error payload: %#v", payload["error"])
	}
}

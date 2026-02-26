package lsp

import (
	"encoding/json"
	"testing"
)

func TestLSPToolCallMetaAttrs_ExtractsRuntimeFields(t *testing.T) {
	raw := json.RawMessage(`{
		"file_path":"main.go",
		"__tool_call_meta":{
			"agent_id":"agent-1",
			"call_id":"call-2",
			"thread_id":"thread-3",
			"request_id":42
		}
	}`)

	attrs := lspToolCallMetaAttrs(raw)
	if len(attrs) == 0 {
		t.Fatalf("expected attrs, got none")
	}

	got := make(map[string]any)
	for i := 0; i+1 < len(attrs); i += 2 {
		key, ok := attrs[i].(string)
		if !ok {
			continue
		}
		got[key] = attrs[i+1]
	}

	if got["agent_id"] != "agent-1" {
		t.Fatalf("agent_id = %#v, want agent-1", got["agent_id"])
	}
	if got["call_id"] != "call-2" {
		t.Fatalf("call_id = %#v, want call-2", got["call_id"])
	}
	if got["thread_id"] != "thread-3" {
		t.Fatalf("thread_id = %#v, want thread-3", got["thread_id"])
	}
	if got["req_id"] != "42" {
		t.Fatalf("req_id = %#v, want 42", got["req_id"])
	}
}

func TestLSPToolCallMetaAttrs_MissingMetaReturnsEmpty(t *testing.T) {
	attrs := lspToolCallMetaAttrs(json.RawMessage(`{"file_path":"main.go"}`))
	if len(attrs) != 0 {
		t.Fatalf("expected empty attrs, got %#v", attrs)
	}
}

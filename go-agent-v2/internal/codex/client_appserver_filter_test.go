package codex

import (
	"encoding/json"
	"testing"
)

func TestShouldDropLegacyMirrorNotification_DropsMirror(t *testing.T) {
	msg := jsonRPCMessage{
		Method: "codex/event/agent_message_delta",
		Params: json.RawMessage(`{"id":"","msg":{"delta":"你"},"conversationId":"thread-abc"}`),
	}
	drop, preview, conversationID := shouldDropLegacyMirrorNotification(msg)
	if !drop {
		t.Fatal("expected legacy mirror notification to be dropped")
	}
	if preview != "你" {
		t.Fatalf("preview = %q, want %q", preview, "你")
	}
	if conversationID != "thread-abc" {
		t.Fatalf("conversationID = %q, want %q", conversationID, "thread-abc")
	}
}

func TestShouldDropLegacyMirrorNotification_DropsLegacyEnvelopeOnV2Method(t *testing.T) {
	msg := jsonRPCMessage{
		Method: "item/agentMessage/delta",
		Params: json.RawMessage(`{"id":"legacy-1","msg":{"delta":"你"},"conversationId":"thread-abc"}`),
	}
	drop, preview, conversationID := shouldDropLegacyMirrorNotification(msg)
	if !drop {
		t.Fatal("expected legacy mirror envelope to be dropped")
	}
	if preview != "你" {
		t.Fatalf("preview = %q, want %q", preview, "你")
	}
	if conversationID != "thread-abc" {
		t.Fatalf("conversationID = %q, want %q", conversationID, "thread-abc")
	}
}

func TestShouldDropLegacyMirrorNotification_KeepsStructuredV2(t *testing.T) {
	msg := jsonRPCMessage{
		Method: "item/agentMessage/delta",
		Params: json.RawMessage(`{"delta":"你","itemId":"x","turnId":"y","threadId":"z"}`),
	}
	drop, _, _ := shouldDropLegacyMirrorNotification(msg)
	if drop {
		t.Fatal("did not expect v2 structured notification to be dropped")
	}
}

func TestShouldDropLegacyMirrorNotification_KeepsUnrelatedNotification(t *testing.T) {
	msg := jsonRPCMessage{
		Method: "codex/event/mcp_startup_update",
		Params: json.RawMessage(`{"message":"ok","conversationId":"thread-abc"}`),
	}
	drop, _, _ := shouldDropLegacyMirrorNotification(msg)
	if drop {
		t.Fatal("did not expect unrelated notification to be dropped")
	}
}

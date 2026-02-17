package apiserver

import (
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

func TestClassifyEventRole_ItemAgentMessageDelta(t *testing.T) {
	got := classifyEventRole("item/agentMessage/delta")
	if got != "assistant" {
		t.Fatalf("classifyEventRole(item/agentMessage/delta) = %q, want assistant", got)
	}
}

func TestExtractEventContent_FromNestedMsg(t *testing.T) {
	event := codex.Event{
		Type: "codex/event/agent_message",
		Data: []byte(`{"msg":{"type":"agent_message","message":"完整回复"}}`),
	}

	got := extractEventContent(event)
	if got != "完整回复" {
		t.Fatalf("extractEventContent() = %q, want %q", got, "完整回复")
	}
}

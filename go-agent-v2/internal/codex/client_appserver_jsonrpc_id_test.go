package codex

import "testing"

func TestJSONRPCIDUnmarshalInteger(t *testing.T) {
	var id jsonRPCID
	if err := id.UnmarshalJSON([]byte("123")); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got, ok := id.asInt64(); !ok || got != 123 {
		t.Fatalf("asInt64()=(%d,%v), want (123,true)", got, ok)
	}
	if key := id.pendingKey(); key != "i:123" {
		t.Fatalf("pendingKey()=%q, want %q", key, "i:123")
	}
}

func TestJSONRPCIDUnmarshalString(t *testing.T) {
	var id jsonRPCID
	if err := id.UnmarshalJSON([]byte(`"req-abc"`)); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got, ok := id.asString(); !ok || got != "req-abc" {
		t.Fatalf("asString()=(%q,%v), want (%q,true)", got, ok, "req-abc")
	}
	if key := id.pendingKey(); key != "s:req-abc" {
		t.Fatalf("pendingKey()=%q, want %q", key, "s:req-abc")
	}
	if id.int64Ptr() != nil {
		t.Fatalf("expected int64Ptr() to be nil for string id")
	}
}

func TestJSONRPCIDUnmarshalRejectsInvalidType(t *testing.T) {
	var id jsonRPCID
	if err := id.UnmarshalJSON([]byte("{}")); err == nil {
		t.Fatalf("expected error for invalid id type")
	}
}

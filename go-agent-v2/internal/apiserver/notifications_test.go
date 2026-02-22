package apiserver

import "testing"

func TestMapEventToMethod_TurnAborted(t *testing.T) {
	got := mapEventToMethod("turn_aborted")
	if got != "turn/completed" {
		t.Fatalf("mapEventToMethod(turn_aborted) = %q, want turn/completed", got)
	}
}

func TestMapEventToMethod_StreamErrorUsesErrorChannel(t *testing.T) {
	got := mapEventToMethod("stream_error")
	if got != "error" {
		t.Fatalf("mapEventToMethod(stream_error) = %q, want error", got)
	}
}

func TestMapEventToMethod_TurnPlanUpdated(t *testing.T) {
	got := mapEventToMethod("turn_plan")
	if got != "turn/plan/updated" {
		t.Fatalf("mapEventToMethod(turn_plan) = %q, want turn/plan/updated", got)
	}
}

package apiserver

import "testing"

func TestMapEventToMethod_TurnAborted(t *testing.T) {
	got := mapEventToMethod("turn_aborted")
	if got != "turn/completed" {
		t.Fatalf("mapEventToMethod(turn_aborted) = %q, want turn/completed", got)
	}
}

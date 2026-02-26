package codexadapter

import (
	"reflect"
	"testing"
	"time"
)

func TestTrackedTurnContractShape(t *testing.T) {
	t.Parallel()

	got := reflect.TypeOf(trackedTurn{})
	want := []struct {
		name string
		typ  reflect.Type
	}{
		{name: "ID", typ: reflect.TypeOf("")},
		{name: "ThreadID", typ: reflect.TypeOf("")},
		{name: "StartedAt", typ: reflect.TypeOf(time.Time{})},
		{name: "LastEventAt", typ: reflect.TypeOf(time.Time{})},
		{name: "InterruptRequested", typ: reflect.TypeOf(false)},
		{name: "InterruptRequestedAt", typ: reflect.TypeOf(time.Time{})},
		{name: "StallHintLogged", typ: reflect.TypeOf(false)},
		{name: "StallGraceStarted", typ: reflect.TypeOf(false)},
		{name: "StallAutoInterrupted", typ: reflect.TypeOf(false)},
		{name: "Done", typ: reflect.TypeOf((chan string)(nil))},
		{name: "Timer", typ: reflect.TypeOf((*time.Timer)(nil))},
		{name: "StallTimer", typ: reflect.TypeOf((*time.Timer)(nil))},
	}

	if got.NumField() != len(want) {
		t.Fatalf("trackedTurn field count = %d, want %d", got.NumField(), len(want))
	}
	for i, expected := range want {
		field := got.Field(i)
		if field.Name != expected.name {
			t.Fatalf("field[%d] name = %s, want %s", i, field.Name, expected.name)
		}
		if field.Type != expected.typ {
			t.Fatalf("field[%d] %s type = %s, want %s", i, field.Name, field.Type, expected.typ)
		}
	}
}

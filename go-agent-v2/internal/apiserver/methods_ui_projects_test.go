package apiserver

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func TestUIProjectsCRUD(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
	}
	ctx := context.Background()

	if _, err := srv.uiProjectsAdd(ctx, uiProjectsAddParams{Path: "/tmp/demo/"}); err != nil {
		t.Fatalf("uiProjectsAdd first: %v", err)
	}
	if _, err := srv.uiProjectsAdd(ctx, uiProjectsAddParams{Path: "/tmp/demo"}); err != nil {
		t.Fatalf("uiProjectsAdd dedup: %v", err)
	}

	raw, err := srv.uiProjectsGet(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("uiProjectsGet: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("uiProjectsGet type=%T, want map[string]any", raw)
	}
	if got := asString(resp["active"]); got != "/tmp/demo" {
		t.Fatalf("active=%q, want /tmp/demo", got)
	}
	if !reflect.DeepEqual(resp["projects"], []string{"/tmp/demo"}) {
		t.Fatalf("projects=%#v, want [/tmp/demo]", resp["projects"])
	}

	if _, err := srv.uiProjectsRemove(ctx, uiProjectsRemoveParams{Path: "/tmp/demo"}); err != nil {
		t.Fatalf("uiProjectsRemove: %v", err)
	}
	raw, err = srv.uiProjectsGet(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("uiProjectsGet after remove: %v", err)
	}
	resp, ok = raw.(map[string]any)
	if !ok {
		t.Fatalf("uiProjectsGet after remove type=%T, want map[string]any", raw)
	}
	if got := asString(resp["active"]); got != "." {
		t.Fatalf("active after remove=%q, want .", got)
	}
	if !reflect.DeepEqual(resp["projects"], []string{}) {
		t.Fatalf("projects after remove=%#v, want []", resp["projects"])
	}
}

func TestUIProjectsSetActiveFallback(t *testing.T) {
	srv := &Server{
		prefManager: uistate.NewPreferenceManager(nil),
	}
	ctx := context.Background()

	if _, err := srv.uiProjectsAdd(ctx, uiProjectsAddParams{Path: "/repo/a"}); err != nil {
		t.Fatalf("uiProjectsAdd /repo/a: %v", err)
	}
	if _, err := srv.uiProjectsSetActive(ctx, uiProjectsSetActiveParams{Path: "/repo/missing"}); err != nil {
		t.Fatalf("uiProjectsSetActive missing: %v", err)
	}

	raw, err := srv.uiProjectsGet(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("uiProjectsGet: %v", err)
	}
	resp := raw.(map[string]any)
	if got := asString(resp["active"]); got != "." {
		t.Fatalf("active=%q, want .", got)
	}
}


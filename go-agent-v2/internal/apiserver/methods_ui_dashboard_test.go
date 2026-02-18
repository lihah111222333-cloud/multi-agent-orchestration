package apiserver

import (
	"context"
	"testing"
)

func TestUIDashboardGetReturnsStableShape(t *testing.T) {
	srv := &Server{}

	raw, err := srv.uiDashboardGet(context.Background(), uiDashboardGetParams{Page: "tasks"})
	if err != nil {
		t.Fatalf("uiDashboardGet error: %v", err)
	}
	resp, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("uiDashboardGet type=%T, want map[string]any", raw)
	}

	keys := []string{
		"agents",
		"dags",
		"taskAcks",
		"taskTraces",
		"skills",
		"commandCards",
		"prompts",
		"memory",
	}
	for _, key := range keys {
		value, exists := resp[key]
		if !exists {
			t.Fatalf("missing key %q", key)
		}
		if _, ok := value.([]any); !ok {
			t.Fatalf("key %q type=%T, want []any", key, value)
		}
	}
}


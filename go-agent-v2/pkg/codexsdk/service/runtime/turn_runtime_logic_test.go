package runtime

import "testing"

func TestEnsureTurnSteerResultTurnID(t *testing.T) {
	t.Run("fills missing turn id", func(t *testing.T) {
		result := EnsureTurnSteerResultTurnID(map[string]any{"ok": true}, "turn-123")
		if got, _ := result["turnId"].(string); got != "turn-123" {
			t.Fatalf("turnId = %q, want %q", got, "turn-123")
		}
	})

	t.Run("keeps existing turn id", func(t *testing.T) {
		result := EnsureTurnSteerResultTurnID(map[string]any{"turnId": "already"}, "turn-123")
		if got, _ := result["turnId"].(string); got != "already" {
			t.Fatalf("turnId = %q, want %q", got, "already")
		}
	})
}

func TestTurnSteerFromInputAligned(t *testing.T) {
	resolve := func(TurnSteerRequest) (string, string, error) {
		return "thread-1", "active-1", nil
	}

	t.Run("inject turn id when handler omits it", func(t *testing.T) {
		result, err := TurnSteerFromInputAligned(TurnSteerRequest{ThreadID: "thread-1"}, resolve, func(TurnSteerRequest) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		})
		if err != nil {
			t.Fatalf("TurnSteerFromInputAligned error: %v", err)
		}
		if got, _ := result["turnId"].(string); got != "active-1" {
			t.Fatalf("turnId = %q, want %q", got, "active-1")
		}
	})

	t.Run("preserve turn id from handler", func(t *testing.T) {
		result, err := TurnSteerFromInputAligned(TurnSteerRequest{ThreadID: "thread-1"}, resolve, func(TurnSteerRequest) (map[string]any, error) {
			return map[string]any{"turnId": "handler-turn"}, nil
		})
		if err != nil {
			t.Fatalf("TurnSteerFromInputAligned error: %v", err)
		}
		if got, _ := result["turnId"].(string); got != "handler-turn" {
			t.Fatalf("turnId = %q, want %q", got, "handler-turn")
		}
	})
}

package codexadapter

import (
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// TurnSteerFromInputAligned enforces expectedTurnId semantics before steering.
func (a *Adapter) TurnSteerFromInputAligned(req turnSteerRequest) (map[string]any, error) {
	threadID, err := requireThreadID("Server.turnSteer", req.ThreadID)
	if err != nil {
		return nil, err
	}

	expectedTurnID := strings.TrimSpace(req.ExpectedTurnID)
	if expectedTurnID == "" {
		return nil, apperrors.New("Server.turnSteer", "expectedTurnId must not be empty")
	}

	activeTurnID, hasActiveTurn := a.activeTrackedTurnID(threadID)
	if !hasActiveTurn {
		return nil, apperrors.New("Server.turnSteer", "no active turn to steer")
	}
	if !strings.EqualFold(expectedTurnID, activeTurnID) {
		return nil, apperrors.Newf(
			"Server.turnSteer",
			"expectedTurnId mismatch: expected %s, active %s",
			expectedTurnID,
			activeTurnID,
		)
	}

	result, err := a.TurnSteerFromInput(req)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = map[string]any{}
	}
	if currentID, _ := result["turnId"].(string); strings.TrimSpace(currentID) == "" {
		result["turnId"] = activeTurnID
	}
	return result, nil
}

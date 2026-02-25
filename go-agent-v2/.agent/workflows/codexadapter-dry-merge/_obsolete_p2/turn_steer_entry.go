package codexadapter

import (
	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// TurnSteerRequest carries protocol params for turn/steer.
type TurnSteerRequest = contracts.TurnSteerRequest

// TurnSteerFromInput handles turn/steer with constructor-time dependencies.
func (a *Adapter) TurnSteerFromInput(req TurnSteerRequest) (map[string]any, error) {
	selectedSkills, err := commonadapter.NormalizeSkillNames(req.SelectedSkills)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.turnSteer", "normalize selected skills")
	}
	prepared, err := a.prepareTurnSteerSubmission(req.ThreadID, req.Input, selectedSkills, req.ManualSkillSelection)
	if err != nil {
		return nil, err
	}
	return a.TurnSteer(req.ThreadID, prepared.SubmitPrompt, prepared.Images, prepared.Files)
}

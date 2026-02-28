package runtime

import ac "github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"

func TurnSteerFromInputAlignedByAdapter(a PrepareAdapter, req ac.TurnSteerRequest, turnSteerFromInput func(ac.TurnSteerRequest) (map[string]any, error)) (map[string]any, error) {
	return TurnSteerFromInputAligned(req, func(request ac.TurnSteerRequest) (string, string, error) {
		return ResolveTurnSteerAlignment(a, request)
	}, turnSteerFromInput)
}

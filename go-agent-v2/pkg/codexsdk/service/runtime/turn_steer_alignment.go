package runtime

import "github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"

func TurnSteerFromInputAlignedByAdapter(a PrepareAdapter, req agentcore.TurnSteerRequest, turnSteerFromInput func(agentcore.TurnSteerRequest) (map[string]any, error)) (map[string]any, error) {
	return TurnSteerFromInputAligned(req, func(request agentcore.TurnSteerRequest) (string, string, error) {
		return ResolveTurnSteerAlignment(a, request)
	}, turnSteerFromInput)
}

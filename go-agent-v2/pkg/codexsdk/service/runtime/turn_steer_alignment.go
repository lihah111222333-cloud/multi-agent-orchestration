package runtime

// TurnSteerFromInputAlignedByAdapter enforces expectedTurnId semantics before steering.
func TurnSteerFromInputAlignedByAdapter(
	a PrepareAdapter,
	req TurnSteerRequest,
	turnSteerFromInput func(TurnSteerRequest) (map[string]any, error),
) (map[string]any, error) {
	return TurnSteerFromInputAligned(
		req,
		func(request TurnSteerRequest) (string, string, error) {
			return ResolveTurnSteerAlignment(a, request)
		},
		turnSteerFromInput,
	)
}

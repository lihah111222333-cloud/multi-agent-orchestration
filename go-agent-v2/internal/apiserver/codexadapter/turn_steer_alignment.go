package codexadapter

// TurnSteerFromInputAligned enforces expectedTurnId semantics before steering.
func (a *Adapter) TurnSteerFromInputAligned(req turnSteerRequest) (map[string]any, error) {
	return a.turnSteerFromInputAligned(req)
}

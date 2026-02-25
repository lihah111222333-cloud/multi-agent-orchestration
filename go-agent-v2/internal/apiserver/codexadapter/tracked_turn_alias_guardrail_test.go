package codexadapter

import "github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"

// Compile-time guardrail: codexadapter TrackedTurn must remain an alias-compatible view.
var _ *contracts.TrackedTurn = (*TrackedTurn)(nil)
var _ *TrackedTurn = (*contracts.TrackedTurn)(nil)

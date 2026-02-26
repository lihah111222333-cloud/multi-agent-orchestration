package bus

import (
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
)

// AgentClient abstracts agent communication used by the router.
type AgentClient interface {
	Submit(prompt string, images, files []string, outputSchema json.RawMessage) error
	Running() bool
}

// AgentClientFactory creates one client for threadID + port.
type AgentClientFactory func(threadID string, port int) AgentClient

// AgentEvent is the normalized agent event envelope.
type AgentEvent = agentcore.Event

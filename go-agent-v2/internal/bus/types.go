package bus

import (
	"encoding/json"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

// AgentClient abstracts agent communication used by the router.
type AgentClient interface {
	Submit(prompt string, images, files []string, outputSchema json.RawMessage) error
	Running() bool
}

type AgentClientFactory func(threadID string, port int) AgentClient

type AgentEvent = agentcore.Event

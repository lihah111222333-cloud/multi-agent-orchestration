package discovery

import "context"

// RunningAgent is the minimal routing view needed by cross-agent discovery.
type RunningAgent struct {
	ThreadID string `json:"thread_id"`
	Port     int    `json:"port"`
	PID      int    `json:"pid"`
	Status   string `json:"status"`
}

// Discoverer provides running agent endpoint discovery for routing.
type Discoverer interface {
	ListRunning(ctx context.Context) ([]RunningAgent, error)
}

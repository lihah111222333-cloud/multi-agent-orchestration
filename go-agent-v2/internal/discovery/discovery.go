package discovery

import "context"

type RunningAgent struct {
	ThreadID string `json:"thread_id"`
	Port     int    `json:"port"`
	PID      int    `json:"pid"`
	Status   string `json:"status"`
}

type Discoverer interface {
	ListRunning(ctx context.Context) ([]RunningAgent, error)
}

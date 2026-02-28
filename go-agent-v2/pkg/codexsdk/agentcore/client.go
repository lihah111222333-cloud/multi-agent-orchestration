package agentcore

import (
	"context"
	"encoding/json"
)

// EventHandler handles one normalized event from a CLI client.
type EventHandler func(event Event)

// Client is the CLI-agnostic client contract used by runner/apiserver.
type Client interface {
	GetPort() int
	GetThreadID() string
	SetEventHandler(h EventHandler)
	SpawnAndConnect(ctx context.Context, prompt, cwd, model, instructions string, dynamicTools []DynamicTool) error
	Submit(prompt string, images, files []string, outputSchema json.RawMessage) error
	SendCommand(cmd, args string) error
	SendDynamicToolResult(callID, output string, requestID *int64) error
	RespondError(id int64, code int, message string) error
	ListThreads() ([]ThreadInfo, error)
	ResumeThread(req ResumeThreadRequest) error
	ForkThread(req ForkThreadRequest) (*ForkThreadResponse, error)
	Shutdown() error
	Kill() error
	Running() bool
}

// ClientFactory creates a client by port and agent ID.
type ClientFactory func(port int, agentID string) Client

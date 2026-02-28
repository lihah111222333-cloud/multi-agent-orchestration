package codex

import (
	"fmt"
	"net"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type TransportMode string

const TransportSSE TransportMode = "sse"

type EventHandler = agentcore.EventHandler

type Client struct {
	*AppServerClient
	Transport TransportMode
}

func NewClient(port int, agentID string) *Client {
	return &Client{
		AppServerClient: NewAppServerClient(port, agentID),
		Transport:       TransportSSE,
	}
}

func (c *Client) Health() error {
	if c == nil || !c.Running() { return apperrors.New("Client.Health", "client not running") }
	return nil
}

func (c *Client) CreateThread(req CreateThreadRequest) (*CreateThreadResponse, error) {
	threadID, err := c.ThreadStart(req.Cwd, req.Model, req.Prompt, req.DynamicTools)
	if err != nil {
		return nil, err
	}
	return &CreateThreadResponse{ThreadID: threadID, Port: c.Port}, nil
}

func (c *Client) DeleteThread(threadID string) error {
	return apperrors.New("Client.DeleteThread", "delete thread not supported in app-server transport")
}

func checkPortFree(port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil { _ = l.Close() }
	return err
}

package codex

import (
	"context"
	"fmt"
	"net"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type TransportMode string

const TransportSSE TransportMode = "sse"

type EventHandler = agentcore.EventHandler

// Client keeps the legacy REST-facing API surface while delegating runtime
// lifecycle and transport behavior to AppServerClient.
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

func (c *Client) Spawn(ctx context.Context) error { return c.AppServerClient.Spawn(ctx) }

func (c *Client) Health() error {
	if c == nil || !c.Running() {
		return apperrors.New("Client.Health", "client not running")
	}
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

func restClientUnsupported(method string) error {
	return apperrors.New(method, "not supported in REST client, use AppServerClient")
}

func (c *Client) SendCommand(cmd, args string) error {
	return restClientUnsupported("Client.SendCommand")
}

func (c *Client) SendDynamicToolResult(callID, output string, requestID *int64) error {
	return restClientUnsupported("Client.SendDynamicToolResult")
}

func (c *Client) RespondError(id int64, code int, message string) error {
	return restClientUnsupported("Client.RespondError")
}

func checkPortFree(port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	_ = l.Close()
	return nil
}

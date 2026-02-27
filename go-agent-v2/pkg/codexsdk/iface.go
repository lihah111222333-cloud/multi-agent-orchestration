package codexsdk

import (
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
)

type Event = agentcore.Event
type EventHandler = agentcore.EventHandler
type DynamicTool = agentcore.DynamicTool
type ThreadInfo = agentcore.ThreadInfo
type ResumeThreadRequest = agentcore.ResumeThreadRequest
type ForkThreadRequest = agentcore.ForkThreadRequest
type ForkThreadResponse = agentcore.ForkThreadResponse
type Client = agentcore.Client
type ClientFactory = func(port int, agentID string) Client

type AgentState = runner.AgentState
type AgentInfo = runner.AgentInfo
type AgentProcess = runner.AgentProcess
type AgentManager = runner.AgentManager

const (
	StateIdle     = runner.StateIdle
	StateThinking = runner.StateThinking
	StateRunning  = runner.StateRunning
	StateStopped  = runner.StateStopped
	StateError    = runner.StateError
)

func NewAgentManager(appFactory, restFactory ClientFactory) (*AgentManager, error) {
	var app agentcore.ClientFactory
	if appFactory != nil {
		app = func(port int, agentID string) agentcore.Client {
			return appFactory(port, agentID)
		}
	}

	var rest agentcore.ClientFactory
	if restFactory != nil {
		rest = func(port int, agentID string) agentcore.Client {
			return restFactory(port, agentID)
		}
	}

	return runner.NewAgentManager(app, rest)
}

func NewAgentProcess(client Client) *AgentProcess {
	return &runner.AgentProcess{Client: client}
}

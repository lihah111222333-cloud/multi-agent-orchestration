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
type ClientFactory = agentcore.ClientFactory

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
	return runner.NewAgentManager(appFactory, restFactory)
}

func NewAgentProcess(client Client) *AgentProcess { return &runner.AgentProcess{Client: client} }

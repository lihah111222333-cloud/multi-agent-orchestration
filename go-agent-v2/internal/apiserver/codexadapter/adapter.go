package codexadapter

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

// Deps defines codex adapter runtime dependencies.
type Deps struct {
	Manager                  *runner.AgentManager
	Store                    *uistate.PreferenceManager
	BindingStore             *store.AgentCodexBindingStore
	AgentStatusStore         *store.AgentStatusStore
	UIRuntime                *uistate.RuntimeManager
	AllSchemas               func() []agentcore.DynamicTool
	NowUnixMilli             func() int64
	SetAgentWorkDir          func(agentID, cwd string)
	CancelCodeRuns           func(agentID string) int
	ReadSkillContent         func(skillName string) (string, error)
	ListSkillNames           func() ([]string, error)
	ListSkillMatchCandidates func() ([]SkillMatchCandidate, error)
	GetAgentSkills           func(agentID string) []string
	Notify                   func(method string, params any)
}

func defaultAllSchemaProvider() []agentcore.DynamicTool { return nil }

func defaultNowUnixMilliProvider() int64 { return time.Now().UnixMilli() }

func defaultSetAgentWorkDirProvider(string, string) {}

func defaultCancelCodeRunsProvider(string) int { return 0 }

func defaultGetAgentSkillsProvider(string) []string { return nil }

func defaultNotifyProvider(string, any) {}

func normalizeDeps(deps Deps) *Deps {
	d := deps
	if d.AllSchemas == nil {
		d.AllSchemas = defaultAllSchemaProvider
	}
	if d.NowUnixMilli == nil {
		d.NowUnixMilli = defaultNowUnixMilliProvider
	}
	if d.SetAgentWorkDir == nil {
		d.SetAgentWorkDir = defaultSetAgentWorkDirProvider
	}
	if d.CancelCodeRuns == nil {
		d.CancelCodeRuns = defaultCancelCodeRunsProvider
	}
	if d.GetAgentSkills == nil {
		d.GetAgentSkills = defaultGetAgentSkillsProvider
	}
	if d.Notify == nil {
		d.Notify = defaultNotifyProvider
	}
	return &d
}

// Adapter 封装对 proc.Client 的直接访问。
type Adapter struct {
	ctx *Deps

	tracker                TurnTrackerState
	trackerMu              sync.Mutex
	trackerActiveTurns     map[string]*TrackedTurn
	trackerWatchdogTimeout time.Duration
	trackerSummaryCache    map[string]TrackedTurnSummaryCacheEntry
	trackerSummaryTTL      time.Duration
	trackerStallThreshold  time.Duration
	trackerStallHeartbeat  time.Duration
}

// New 创建 codex 适配器。
func New(deps Deps) *Adapter {
	adapter := &Adapter{ctx: normalizeDeps(deps)}
	adapter.initTrackerState()
	return adapter
}

func (a *Adapter) initTrackerState() {
	if a == nil {
		return
	}
	a.trackerActiveTurns = make(map[string]*TrackedTurn)
	a.trackerWatchdogTimeout = DefaultTurnWatchdogTimeout
	a.trackerSummaryCache = make(map[string]TrackedTurnSummaryCacheEntry)
	a.trackerSummaryTTL = DefaultTrackedTurnSummaryTTL
	a.trackerStallThreshold = DefaultStallThreshold
	a.trackerStallHeartbeat = DefaultStallHeartbeat

	a.tracker = TurnTrackerState{
		Mu:                  &a.trackerMu,
		ActiveTurns:         &a.trackerActiveTurns,
		TurnWatchdogTimeout: &a.trackerWatchdogTimeout,
		TurnSummaryCache:    &a.trackerSummaryCache,
		TurnSummaryTTL:      &a.trackerSummaryTTL,
		StallThreshold:      &a.trackerStallThreshold,
		StallHeartbeat:      &a.trackerStallHeartbeat,
	}
}

// Context returns configured dependency hooks.
func (a *Adapter) Context() *Deps {
	if a == nil {
		return nil
	}
	return a.ctx
}

// ErrNoProcess is returned when the agent process or its client is nil.
var ErrNoProcess = errors.New("codexadapter: agent process not found")

// requireClient returns the underlying client or ErrNoProcess.
func requireClient(proc *runner.AgentProcess) (agentcore.Client, error) {
	if proc == nil || proc.Client == nil {
		return nil, ErrNoProcess
	}
	return proc.Client, nil
}

// Submit 发送用户输入到 codex。
func (a *Adapter) Submit(proc *runner.AgentProcess, prompt string, images, files []string, outputSchema json.RawMessage) error {
	c, err := requireClient(proc)
	if err != nil {
		return err
	}
	return c.Submit(prompt, images, files, outputSchema)
}

// SendCommand 发送 slash 命令到 codex。
func (a *Adapter) SendCommand(proc *runner.AgentProcess, command string, args string) error {
	c, err := requireClient(proc)
	if err != nil {
		return err
	}
	return c.SendCommand(command, args)
}

// GetThreadID 读取当前 codex thread id。
func (a *Adapter) GetThreadID(proc *runner.AgentProcess) string {
	if proc == nil || proc.Client == nil {
		return ""
	}
	return strings.TrimSpace(proc.Client.GetThreadID())
}

// ResumeThread 恢复历史 codex thread。
func (a *Adapter) ResumeThread(proc *runner.AgentProcess, req agentcore.ResumeThreadRequest) error {
	c, err := requireClient(proc)
	if err != nil {
		return err
	}
	return c.ResumeThread(req)
}

// ListThreads 查询 codex 线程列表。
func (a *Adapter) ListThreads(proc *runner.AgentProcess) ([]agentcore.ThreadInfo, error) {
	c, err := requireClient(proc)
	if err != nil {
		return nil, err
	}
	return c.ListThreads()
}

// ForkThread 基于指定源线程创建分叉线程。
func (a *Adapter) ForkThread(proc *runner.AgentProcess, req agentcore.ForkThreadRequest) (*agentcore.ForkThreadResponse, error) {
	c, err := requireClient(proc)
	if err != nil {
		return nil, err
	}
	return c.ForkThread(req)
}

// RespondError 回传 dynamic tool 调用错误。
func (a *Adapter) RespondError(proc *runner.AgentProcess, id int64, code int, message string) error {
	c, err := requireClient(proc)
	if err != nil {
		return err
	}
	return c.RespondError(id, code, message)
}

// SendDynamicToolResult 回传 dynamic tool 调用结果。
func (a *Adapter) SendDynamicToolResult(proc *runner.AgentProcess, callID, output string, requestID *int64) error {
	c, err := requireClient(proc)
	if err != nil {
		return err
	}
	return c.SendDynamicToolResult(callID, output, requestID)
}

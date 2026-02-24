package codexadapter

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ResolveSlashCommandThread resolves thread process for slash command execution.
func ResolveSlashCommandThread(
	ctx context.Context,
	threadID string,
	getProc func(string) *runner.AgentProcess,
	ensureReady func(context.Context, string, string) (*runner.AgentProcess, error),
) (*runner.AgentProcess, error) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New("Server.sendSlashCommand", "threadId is required")
	}
	if getProc != nil {
		if proc := getProc(id); proc != nil {
			return proc, nil
		}
	}
	if ensureReady == nil {
		return nil, apperrors.Newf("Server.sendSlashCommand", "thread %s not found", id)
	}
	proc, err := ensureReady(ctx, id, "")
	if err != nil {
		return nil, err
	}
	if proc == nil {
		return nil, apperrors.Newf("Server.sendSlashCommand", "thread %s not found", id)
	}
	return proc, nil
}

// SendSlashCommandOptions carries slash command execution dependencies.
type SendSlashCommandOptions struct {
	ThreadID  string
	Command   string
	ParamsLen int

	ReadThreadRuntimeState func(string) string
	HasActiveTrackedTurn   func(string) bool
	ResolveThread          func(context.Context, string) (*runner.AgentProcess, error)
	SendCommand            func(*runner.AgentProcess, string, string) error
	GetThreadID            func(*runner.AgentProcess) string
}

// SendSlashCommand executes slash command and emits diagnostic logs.
func SendSlashCommand(ctx context.Context, opt SendSlashCommandOptions) (map[string]any, error) {
	start := time.Now()
	stateBefore := ""
	if opt.ReadThreadRuntimeState != nil {
		stateBefore = opt.ReadThreadRuntimeState(opt.ThreadID)
	}
	activeTrackedBefore := false
	if opt.HasActiveTrackedTurn != nil {
		activeTrackedBefore = opt.HasActiveTrackedTurn(opt.ThreadID)
	}
	activeBefore := IsInterruptActiveState(stateBefore)

	logger.Info("slash/command: request",
		logger.FieldAgentID, opt.ThreadID,
		logger.FieldThreadID, opt.ThreadID,
		logger.FieldCommand, opt.Command,
		logger.FieldParamsLen, opt.ParamsLen,
		"state_before", stateBefore,
		"active_before", activeBefore,
		"active_tracked_before", activeTrackedBefore,
	)

	if strings.EqualFold(strings.TrimSpace(opt.Command), "/compact") && (activeBefore || activeTrackedBefore) {
		logger.Warn("thread/compact/start requested while turn active; compact may be ignored by codex",
			logger.FieldAgentID, opt.ThreadID,
			logger.FieldThreadID, opt.ThreadID,
			"state_before", stateBefore,
			"active_before", activeBefore,
			"active_tracked_before", activeTrackedBefore,
		)
	}

	if opt.ResolveThread == nil {
		return nil, apperrors.New("Server.sendSlashCommand", "thread resolver is not configured")
	}
	proc, err := opt.ResolveThread(ctx, opt.ThreadID)
	if err != nil {
		logger.Warn("slash/command: resolve thread failed",
			logger.FieldAgentID, opt.ThreadID,
			logger.FieldThreadID, opt.ThreadID,
			logger.FieldCommand, opt.Command,
			logger.FieldError, err,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return nil, err
	}
	if opt.SendCommand == nil {
		return nil, apperrors.New("Server.sendSlashCommand", "send command callback is not configured")
	}
	if err := opt.SendCommand(proc, opt.Command, ""); err != nil {
		logger.Warn("slash/command: send failed",
			logger.FieldAgentID, opt.ThreadID,
			logger.FieldThreadID, opt.ThreadID,
			logger.FieldCommand, opt.Command,
			logger.FieldError, err,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return nil, err
	}

	codexThreadID := ""
	if opt.GetThreadID != nil {
		codexThreadID = opt.GetThreadID(proc)
	}
	port := 0
	if proc != nil && proc.Client != nil {
		port = proc.Client.GetPort()
	}
	logger.Info("slash/command: sent",
		logger.FieldAgentID, opt.ThreadID,
		logger.FieldThreadID, opt.ThreadID,
		logger.FieldCommand, opt.Command,
		"codex_thread_id", codexThreadID,
		logger.FieldPort, port,
		logger.FieldDurationMS, time.Since(start).Milliseconds(),
	)
	return map[string]any{}, nil
}

// SendSlashCommandFromParamsOptions carries params + dependencies for slash command execution.
type SendSlashCommandFromParamsOptions struct {
	Params  json.RawMessage
	Command string

	ReadThreadRuntimeState func(string) string
	HasActiveTrackedTurn   func(string) bool
	ResolveThread          func(context.Context, string) (*runner.AgentProcess, error)
}

// SendSlashCommandFromParams parses threadId and dispatches slash command.
func (a *Adapter) SendSlashCommandFromParams(ctx context.Context, opt SendSlashCommandFromParamsOptions) (map[string]any, error) {
	var raw struct {
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(opt.Params, &raw); err != nil {
		return nil, apperrors.Wrap(err, "Server.sendSlashCommand", "unmarshal params")
	}
	return SendSlashCommand(ctx, SendSlashCommandOptions{
		ThreadID:               raw.ThreadID,
		Command:                opt.Command,
		ParamsLen:              len(opt.Params),
		ReadThreadRuntimeState: opt.ReadThreadRuntimeState,
		HasActiveTrackedTurn:   opt.HasActiveTrackedTurn,
		ResolveThread:          opt.ResolveThread,
		SendCommand:            a.SendCommand,
		GetThreadID:            a.GetThreadID,
	})
}

// SendSlashCommandWithArgsOptions carries args-command execution dependencies.
type SendSlashCommandWithArgsOptions struct {
	Params   json.RawMessage
	Command  string
	ArgsField string
	WithThread func(string, func(*runner.AgentProcess) (any, error)) (any, error)
}

// SendSlashCommandWithArgs parses args and executes slash command.
func (a *Adapter) SendSlashCommandWithArgs(opt SendSlashCommandWithArgsOptions) (map[string]any, error) {
	threadID, args, err := ParseSlashCommandWithArgsParams(opt.Params, opt.ArgsField)
	if err != nil {
		return nil, err
	}
	if opt.WithThread == nil {
		return nil, apperrors.New("Server.sendSlashCommandWithArgs", "thread resolver is not configured")
	}
	out, err := opt.WithThread(threadID, func(proc *runner.AgentProcess) (any, error) {
		if sendErr := a.SendCommand(proc, opt.Command, args); sendErr != nil {
			return nil, sendErr
		}
		return map[string]any{}, nil
	})
	if err != nil {
		return nil, err
	}
	result, ok := out.(map[string]any)
	if !ok {
		return nil, apperrors.New("Server.sendSlashCommandWithArgs", "invalid slash command result type")
	}
	return result, nil
}

// ThreadSkillsListOptions carries skill-list dependencies.
type ThreadSkillsListOptions struct {
	ListSkills func() ([]string, error)
}

// ThreadSkillsList normalizes thread/skills/list payload.
func (a *Adapter) ThreadSkillsList(opt ThreadSkillsListOptions) (map[string]any, error) {
	if opt.ListSkills == nil {
		return map[string]any{"skills": []string{}}, nil
	}
	skills, err := opt.ListSkills()
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadSkillsList", "list skills")
	}
	if skills == nil {
		skills = []string{}
	}
	return map[string]any{"skills": skills}, nil
}

// ParseSlashCommandWithArgsParams parses threadId and args from raw params.
func ParseSlashCommandWithArgsParams(params json.RawMessage, argsField string) (threadID string, args string, err error) {
	var raw map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(params, &raw); unmarshalErr != nil {
		return "", "", apperrors.Wrap(unmarshalErr, "Server.sendSlashCommandWithArgs", "unmarshal params")
	}
	if v, ok := raw["threadId"]; ok {
		if unmarshalErr := json.Unmarshal(v, &threadID); unmarshalErr != nil {
			return "", "", apperrors.Wrap(unmarshalErr, "Server.sendSlashCommandWithArgs", "unmarshal threadId")
		}
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", "", apperrors.New("Server.sendSlashCommandWithArgs", "threadId is required")
	}
	if v, ok := raw[argsField]; ok {
		_ = json.Unmarshal(v, &args)
	}
	return threadID, args, nil
}

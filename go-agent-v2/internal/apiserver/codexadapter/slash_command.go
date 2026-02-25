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

// sendSlashCommand executes slash command and emits diagnostic logs.
func (a *Adapter) sendSlashCommand(ctx context.Context, threadID string, command string, paramsLen int) (map[string]any, error) {
	readThreadRuntimeState := a.ReadThreadRuntimeState
	hasActiveTrackedTurn := a.HasActiveTrackedTurn

	start := time.Now()
	stateBefore := readThreadRuntimeState(threadID)
	activeTrackedBefore := hasActiveTrackedTurn(threadID)
	activeBefore := IsInterruptActiveState(stateBefore)

	logger.Info("slash/command: request",
		logger.FieldAgentID, threadID,
		logger.FieldThreadID, threadID,
		logger.FieldCommand, command,
		logger.FieldParamsLen, paramsLen,
		"state_before", stateBefore,
		"active_before", activeBefore,
		"active_tracked_before", activeTrackedBefore,
	)

	if strings.EqualFold(strings.TrimSpace(command), "/compact") && (activeBefore || activeTrackedBefore) {
		logger.Warn("thread/compact/start requested while turn active; compact may be ignored by codex",
			logger.FieldAgentID, threadID,
			logger.FieldThreadID, threadID,
			"state_before", stateBefore,
			"active_before", activeBefore,
			"active_tracked_before", activeTrackedBefore,
		)
	}

	proc, err := a.resolveThreadForSlashCommand(ctx, threadID)
	if err != nil {
		logger.Warn("slash/command: resolve thread failed",
			logger.FieldAgentID, threadID,
			logger.FieldThreadID, threadID,
			logger.FieldCommand, command,
			logger.FieldError, err,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return nil, err
	}
	if err := a.SendCommand(proc, command, ""); err != nil {
		logger.Warn("slash/command: send failed",
			logger.FieldAgentID, threadID,
			logger.FieldThreadID, threadID,
			logger.FieldCommand, command,
			logger.FieldError, err,
			logger.FieldDurationMS, time.Since(start).Milliseconds(),
		)
		return nil, err
	}

	codexThreadID := a.GetThreadID(proc)
	port := 0
	if proc != nil && proc.Client != nil {
		port = proc.Client.GetPort()
	}
	logger.Info("slash/command: sent",
		logger.FieldAgentID, threadID,
		logger.FieldThreadID, threadID,
		logger.FieldCommand, command,
		"codex_thread_id", codexThreadID,
		logger.FieldPort, port,
		logger.FieldDurationMS, time.Since(start).Milliseconds(),
	)
	return map[string]any{}, nil
}

func (a *Adapter) resolveThreadForSlashCommand(ctx context.Context, threadID string) (*runner.AgentProcess, error) {
	if a == nil || a.ctx == nil || a.ctx.Manager() == nil {
		return nil, apperrors.New("Server.sendSlashCommand", "thread manager is not initialized")
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil, apperrors.New("Server.sendSlashCommand", "threadId is required")
	}
	if proc := a.ctx.Manager().Get(id); proc != nil {
		return proc, nil
	}
	proc, err := a.EnsureThreadReadyForTurn(ctx, id, "")
	if err != nil {
		return nil, err
	}
	if proc == nil {
		return nil, apperrors.Newf("Server.sendSlashCommand", "thread %s not found", id)
	}
	return proc, nil
}

// SendSlashCommandFromRawParams parses params and dispatches slash command using constructor-time dependencies.
func (a *Adapter) SendSlashCommandFromRawParams(ctx context.Context, params json.RawMessage, command string) (map[string]any, error) {
	var raw struct {
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(params, &raw); err != nil {
		return nil, apperrors.Wrap(err, "Server.sendSlashCommand", "unmarshal params")
	}
	return a.sendSlashCommand(ctx, raw.ThreadID, command, len(params))
}

// SendSlashCommandWithArgs parses args and executes slash command.
func (a *Adapter) SendSlashCommandWithArgs(params json.RawMessage, command string, argsField string) (map[string]any, error) {
	threadID, args, err := ParseSlashCommandWithArgsParams(params, argsField)
	if err != nil {
		return nil, err
	}
	return withProcess(a, "Server.sendSlashCommandWithArgs", threadID, func(proc *runner.AgentProcess) (map[string]any, error) {
		if sendErr := a.SendCommand(proc, command, args); sendErr != nil {
			return nil, sendErr
		}
		return map[string]any{}, nil
	})
}

// ThreadSkillsList normalizes thread/skills/list payload.
func (a *Adapter) ThreadSkillsList() (map[string]any, error) {
	skills, err := a.listSkillNames()
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

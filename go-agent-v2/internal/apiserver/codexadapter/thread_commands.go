package codexadapter

import (
	"fmt"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// ThreadRollback sends /undo index command.
func (a *Adapter) ThreadRollback(threadID string, turnIndex int) (map[string]any, error) {
	return a.sendThreadCommand("Server.threadRollback", threadID, "/undo", fmt.Sprintf("%d", turnIndex), "send undo command")
}

// ReviewStart dispatches /review command.
func (a *Adapter) ReviewStart(threadID, delivery string) (map[string]any, error) {
	return a.sendThreadCommand("Server.reviewStart", threadID, "/review", delivery, "send review command")
}

// TurnSteer submits steering prompt to existing thread.
func (a *Adapter) TurnSteer(threadID, submitPrompt string, images, files []string) (map[string]any, error) {
	return withProcess(a, "Server.turnSteer", threadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			if submitErr := a.Submit(proc, submitPrompt, images, files, nil); submitErr != nil {
				return nil, submitErr
			}
			return map[string]any{}, nil
		})
}

func (a *Adapter) sendThreadCommand(methodName, threadID, command, args, wrapMsg string) (map[string]any, error) {
	return withProcess(a, methodName, threadID,
		func(proc *runner.AgentProcess) (map[string]any, error) {
			if cmdErr := a.SendCommand(proc, command, args); cmdErr != nil {
				return nil, apperrors.Wrap(cmdErr, methodName, wrapMsg)
			}
			return map[string]any{}, nil
		})
}

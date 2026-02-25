package codexadapter

import "github.com/multi-agent/go-agent-v2/internal/runner"

func (a *Adapter) sendInterruptCommand(proc *runner.AgentProcess) (bool, error) {
	if err := a.SendCommand(proc, "/interrupt", ""); err != nil {
		if IsInterruptNoActiveTurnError(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func (a *Adapter) notifyTurnCompleted(threadID, status, reason string) {
	if completion, ok := a.CompleteTrackedTurnByID(threadID, "", status, reason); ok {
		if a != nil && a.ctx != nil {
			a.ctx.Notify("turn/completed", completion)
		}
		return
	}
	if a != nil && a.ctx != nil {
		a.ctx.Notify("turn/completed", map[string]any{
			"threadId": threadID,
			"status":   status,
			"reason":   reason,
		})
	}
}

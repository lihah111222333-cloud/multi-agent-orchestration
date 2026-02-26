package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type recoverConnectionClient interface {
	RecoverConnection(reason string) error
}

// threadRecoverResult is the normalized thread/recover payload.
type threadRecoverResult struct {
	ThreadID  string
	Status    string
	Recovered bool
	Mode      string
}

// RecoverConnection asks client to force process restart recovery.
func (a *Adapter) RecoverConnection(proc *runner.AgentProcess, reason string) error {
	return withClient(proc, func(c agentcore.Client) error {
		recoverClient, ok := c.(recoverConnectionClient)
		if !ok {
			return apperrors.New("Server.threadRecover", "client does not support connection recovery")
		}
		return recoverClient.RecoverConnection(strings.TrimSpace(reason))
	})
}

// ThreadRecover forces manual connection recovery for current thread process.
func (a *Adapter) ThreadRecover(_ context.Context, threadID string) (threadRecoverResult, error) {
	id, err := requireThreadID("Server.threadRecover", threadID)
	if err != nil {
		return threadRecoverResult{}, err
	}
	return withProcess(a, "Server.threadRecover", id, func(proc *runner.AgentProcess) (threadRecoverResult, error) {
		if recoverErr := a.RecoverConnection(proc, "manual_ui_recover"); recoverErr != nil {
			return threadRecoverResult{}, apperrors.Wrap(recoverErr, "Server.threadRecover", "recover connection")
		}
		logger.Info("thread/recover: manual recovery triggered", threadLogFields(id)...)
		return threadRecoverResult{
			ThreadID:  id,
			Status:    "recovering",
			Recovered: true,
			Mode:      "respawn",
		}, nil
	})
}

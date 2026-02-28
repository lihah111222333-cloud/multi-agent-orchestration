package codexadapter

import (
	"context"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type recoverConnectionClient interface {
	RecoverConnection(reason string) error
}

type threadRecoverResult struct {
	ThreadID  string
	Status    string
	Recovered bool
	Mode      string
}

func (a *Adapter) ThreadRecover(_ context.Context, threadID string) (threadRecoverResult, error) {
	id, err := requireThreadID("Server.threadRecover", threadID)
	if err != nil {
		return threadRecoverResult{}, err
	}
	return withProcess(a, "Server.threadRecover", id, func(proc *codexsdk.AgentProcess) (threadRecoverResult, error) {
		if recoverErr := withClient(proc, func(c codexsdk.Client) error {
			recoverClient, ok := c.(recoverConnectionClient)
			if !ok {
				return apperrors.New("Server.threadRecover", "client does not support connection recovery")
			}
			return recoverClient.RecoverConnection("manual_ui_recover")
		}); recoverErr != nil {
			return threadRecoverResult{}, apperrors.Wrap(recoverErr, "Server.threadRecover", "recover connection")
		}
		logger.Info("thread/recover: manual recovery triggered", threadLogFields(id)...)
		return threadRecoverResult{ThreadID: id, Status: "recovering", Recovered: true, Mode: "respawn"}, nil
	})
}

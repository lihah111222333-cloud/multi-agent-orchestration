package interrupt

import (
	interruptsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/interrupt"
)

var (
	ReadThreadRuntimeStateByHooks = interruptsvc.ReadThreadRuntimeStateByHooks
	WaitInterruptOutcome          = interruptsvc.WaitInterruptOutcome
	SendInterruptCommand          = interruptsvc.SendInterruptCommand
	NotifyTurnCompleted           = interruptsvc.NotifyTurnCompleted
	TurnInterrupt                 = interruptsvc.TurnInterrupt
	TurnForceComplete             = interruptsvc.TurnForceComplete
)

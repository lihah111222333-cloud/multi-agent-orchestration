package interrupt

import (
	"time"

	interruptsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/interrupt"
)

func ReadThreadRuntimeStateByHooks(
	threadID string,
	readRuntimeStatus func(string) string,
	hasActiveTrackedTurn func(string) bool,
) string {
	return interruptsvc.ReadThreadRuntimeStateByHooks(threadID, readRuntimeStatus, hasActiveTrackedTurn)
}

func WaitInterruptOutcome(
	threadID string,
	timeout time.Duration,
	activeHint bool,
	waitTrackedTurnTerminal func(string, time.Duration) (string, bool),
	readThreadRuntimeState func(string) string,
) (bool, string, int64, bool) {
	return interruptsvc.WaitInterruptOutcome(
		threadID,
		timeout,
		activeHint,
		waitTrackedTurnTerminal,
		readThreadRuntimeState,
	)
}

func SendInterruptCommand(proc any, sendCommand func(any, string, string) error) (bool, error) {
	return interruptsvc.SendInterruptCommand(proc, sendCommand)
}

func NotifyTurnCompleted(
	threadID string,
	status string,
	reason string,
	completeTrackedTurnByID func(threadID, turnID, status, reason string) (map[string]any, bool),
	notify func(string, any),
) {
	interruptsvc.NotifyTurnCompleted(threadID, status, reason, completeTrackedTurnByID, notify)
}

func TurnInterrupt(
	threadID string,
	readThreadRuntimeState func(string) string,
	hasActiveTrackedTurn func(string) bool,
	cancelCodeRuns func(string) int,
	sendInterrupt func(any) (bool, error),
	withProcess func(methodName, threadID string, fn func(any) (any, error)) (any, error),
	markTrackedTurnInterruptRequested func(string) bool,
	waitInterruptOutcome func(threadID string, timeout time.Duration, activeHint bool) (bool, string, int64, bool),
	notifyTurnCompleted func(threadID, status, reason string),
) (any, error) {
	return interruptsvc.TurnInterrupt(
		threadID,
		readThreadRuntimeState,
		hasActiveTrackedTurn,
		cancelCodeRuns,
		sendInterrupt,
		withProcess,
		markTrackedTurnInterruptRequested,
		waitInterruptOutcome,
		notifyTurnCompleted,
	)
}

func TurnForceComplete(
	threadID string,
	cancelCodeRuns func(string) int,
	sendInterrupt func(any) (bool, error),
	notifyTurnCompleted func(threadID, status, reason string),
	withProcess func(methodName, threadID string, fn func(any) (any, error)) (any, error),
) (any, error) {
	return interruptsvc.TurnForceComplete(
		threadID,
		cancelCodeRuns,
		sendInterrupt,
		notifyTurnCompleted,
		withProcess,
	)
}

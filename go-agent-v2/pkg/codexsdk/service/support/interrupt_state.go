package support

import "strings"

var (
	interruptStateAliases = map[string]string{
		"completed": "idle", "complete": "idle", "done": "idle", "success": "idle", "succeeded": "idle",
		"ready": "idle", "stopped": "idle", "ended": "idle", "closed": "idle",
		"failed": "error", "fail": "error",
	}
	interruptNoActiveTurnTerms = []string{"no active turn", "nothing to interrupt", "not interruptible"}
	interruptTimeoutTerms      = []string{"timeout", "deadline exceeded"}
	interruptActiveStates      = map[string]struct{}{
		"inprogress": {}, "in_progress": {}, "running": {}, "streaming": {}, "thinking": {},
		"starting": {}, "responding": {}, "editing": {}, "waiting": {}, "syncing": {},
	}
)

func normalizeLower(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func containsAnyFold(text string, terms ...string) bool {
	normalized := normalizeLower(text)
	if normalized == "" {
		return false
	}
	for _, term := range terms {
		pattern := normalizeLower(term)
		if pattern != "" && strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}

func NormalizeInterruptState(raw string) string {
	state := normalizeLower(raw)
	if state == "" {
		return "idle"
	}
	if normalized, ok := interruptStateAliases[state]; ok {
		return normalized
	}
	return state
}

func IsInterruptNoActiveTurnError(err error) bool {
	return err != nil && containsAnyFold(err.Error(), interruptNoActiveTurnTerms...)
}

func IsInterruptTimeoutError(err error) bool {
	return err != nil && containsAnyFold(err.Error(), interruptTimeoutTerms...)
}

func IsInterruptActiveState(state string) bool {
	_, ok := interruptActiveStates[NormalizeInterruptState(state)]
	return ok
}

func InterruptSettleMode(confirmed bool, afterState string) string {
	if confirmed {
		return "interrupt_confirmed"
	}
	switch NormalizeInterruptState(afterState) {
	case "error":
		return "interrupt_terminal_failed"
	case "idle":
		return "interrupt_terminal_completed"
	default:
		return "interrupt_timeout"
	}
}

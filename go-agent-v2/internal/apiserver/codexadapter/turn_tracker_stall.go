package codexadapter

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// ApprovalStallHeartbeatInterval computes heartbeat interval while waiting approvals.
func ApprovalStallHeartbeatInterval(stallThreshold, defaultStallThreshold time.Duration) time.Duration {
	hbInterval := stallThreshold / 6
	if hbInterval <= 0 {
		hbInterval = defaultStallThreshold / 6
	}
	if hbInterval < 10*time.Second {
		hbInterval = 10 * time.Second
	}
	return hbInterval
}

// StartApprovalStallHeartbeat starts periodic turn heartbeat and returns stop func.
func StartApprovalStallHeartbeat(threadID string, stallThreshold, defaultStallThreshold time.Duration, touch func(string)) func() {
	id := strings.TrimSpace(threadID)
	if id == "" || touch == nil {
		return func() {}
	}
	heartbeatDone := make(chan struct{})
	hbInterval := ApprovalStallHeartbeatInterval(stallThreshold, defaultStallThreshold)
	util.SafeGo(func() {
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				touch(id)
			case <-heartbeatDone:
				return
			}
		}
	})
	var once sync.Once
	return func() {
		once.Do(func() { close(heartbeatDone) })
	}
}

// PeekTrackedTurnMeta reads active turn metadata.
func PeekTrackedTurnMeta(activeTurns map[string]*TrackedTurn, turnMu *sync.Mutex, threadID string) (string, time.Time, bool, bool) {
	id := strings.TrimSpace(threadID)
	if id == "" || turnMu == nil {
		return "", time.Time{}, false, false
	}

	turnMu.Lock()
	defer turnMu.Unlock()
	if activeTurns == nil {
		return "", time.Time{}, false, false
	}
	turn, ok := activeTurns[id]
	if !ok || turn == nil {
		return "", time.Time{}, false, false
	}
	return turn.ID, turn.StartedAt, turn.InterruptRequested, true
}

// MarkTrackedTurnStallHint marks whether stall hint was already logged for active turn.
func MarkTrackedTurnStallHint(activeTurns map[string]*TrackedTurn, turnMu *sync.Mutex, threadID, turnID string) bool {
	id := strings.TrimSpace(threadID)
	wantTurnID := strings.TrimSpace(turnID)
	if id == "" || wantTurnID == "" || turnMu == nil {
		return false
	}

	turnMu.Lock()
	defer turnMu.Unlock()
	if activeTurns == nil {
		return false
	}
	turn, ok := activeTurns[id]
	if !ok || turn == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(turn.ID), wantTurnID) {
		return false
	}
	if turn.StallHintLogged {
		return false
	}
	turn.StallHintLogged = true
	return true
}

// ShouldLogTrackedTurnStallHint reports whether current tail event implies a stall hint should be logged.
func ShouldLogTrackedTurnStallHint(eventType, method string, startedAt time.Time) bool {
	if startedAt.IsZero() {
		return false
	}
	age := time.Since(startedAt)
	if age < 60*time.Second {
		return false
	}

	eventKey := strings.ToLower(strings.TrimSpace(eventType))
	methodKey := strings.ToLower(strings.TrimSpace(method))
	switch methodKey {
	case "turn/diff/updated", "turn/plan/updated", "item/completed", "item/plan/delta", "item/agentmessage/delta", "codex/event/turn_diff", "codex/event/plan_delta":
		return true
	}
	switch eventKey {
	case "turn_diff", "plan_delta", "item/completed", "exec_command_end":
		return true
	default:
		return false
	}
}

// TouchTrackedTurnLastEvent updates active-turn heartbeat timestamp.
func TouchTrackedTurnLastEvent(activeTurns map[string]*TrackedTurn, turnMu *sync.Mutex, threadID string) {
	id := strings.TrimSpace(threadID)
	if id == "" || turnMu == nil {
		return
	}
	turnMu.Lock()
	defer turnMu.Unlock()
	if activeTurns == nil {
		logger.Warn("DIAG: touchTrackedTurnLastEvent called but activeTurns map is nil",
			logger.FieldThreadID, id,
		)
		return
	}
	turn, ok := activeTurns[id]
	if !ok || turn == nil {
		logger.Warn("DIAG: touchTrackedTurnLastEvent called but no active turn found",
			logger.FieldThreadID, id,
			"active_turns_count", len(activeTurns),
		)
		return
	}
	turn.LastEventAt = time.Now()
	turn.StallGraceStarted = false
}

// RescheduleStallCheck schedules next stall check before threshold.
func RescheduleStallCheck(turn *TrackedTurn, threadID, turnID string, silent, threshold time.Duration, checkTurnStall func(threadID, turnID string)) {
	if turn == nil || checkTurnStall == nil {
		return
	}
	interval := max(threshold/3, 10*time.Second)
	remaining := interval
	if remaining > threshold-silent {
		remaining = threshold - silent + time.Second
	}
	turn.StallTimer = time.AfterFunc(remaining, func() {
		checkTurnStall(threadID, turnID)
	})
}

// HandleStallGracePeriod starts grace timer before auto interrupt.
func HandleStallGracePeriod(
	activeTurns map[string]*TrackedTurn,
	turnMu *sync.Mutex,
	threadID,
	turnID string,
	silent,
	threshold,
	stallGracePeriod time.Duration,
	pushAlert func(threadID, category, message string),
	checkTurnStall func(threadID, turnID string),
) {
	if stallGracePeriod <= 0 {
		stallGracePeriod = 30 * time.Second
	}
	logger.Warn("turn tracker: thinking stall detected — grace period started",
		logger.FieldThreadID, threadID,
		logger.FieldTurnID, turnID,
		"silent_ms", silent.Milliseconds(),
		"threshold_ms", threshold.Milliseconds(),
		"grace_period_ms", stallGracePeriod.Milliseconds(),
	)

	if pushAlert != nil {
		pushAlert(threadID, "stall_warning",
			fmt.Sprintf("思考已 %ds 未响应，将在 %ds 后自动中断",
				int(silent.Seconds()), int(stallGracePeriod.Seconds())))
	}

	if turnMu == nil {
		return
	}
	turnMu.Lock()
	turn, ok := activeTurns[threadID]
	if ok && turn != nil && turn.ID == turnID {
		turn.StallTimer = time.AfterFunc(stallGracePeriod, func() {
			if checkTurnStall != nil {
				checkTurnStall(threadID, turnID)
			}
		})
	}
	turnMu.Unlock()
}

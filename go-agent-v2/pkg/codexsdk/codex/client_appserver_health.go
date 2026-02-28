package codex

import "time"

type appServerConnectionHealth struct {
	readFailureTimes []time.Time
	circuitOpenUntil time.Time

	consecutiveReadFailures      int
	consecutiveReconnectFailures int
	consecutiveNotInitializedRPC int

	totalReadFailures     int64
	totalReconnectAttempt int64
	totalReconnectSuccess int64
	totalReconnectFailure int64
	totalRespawnSuccess   int64
	totalRespawnFailure   int64
	totalCircuitTrips     int64
	totalNotInitialized   int64

	notInitializedTimes []time.Time

	lastDisconnectAt time.Time
	lastReconnectAt  time.Time
}

type appServerHealthSnapshot struct {
	ReadFailureStreak      int
	ReadErrorsWindow       int
	ReconnectFailureStreak int
	NotInitializedStreak   int

	CircuitOpen        bool
	CircuitRemainingMs int64

	TotalReadFailures     int64
	TotalReconnectAttempt int64
	TotalReconnectSuccess int64
	TotalReconnectFailure int64
	TotalRespawnSuccess   int64
	TotalRespawnFailure   int64
	TotalCircuitTrips     int64
	TotalNotInitialized   int64
}

func (h *appServerConnectionHealth) resetAfterRecovery(now time.Time) {
	h.consecutiveReconnectFailures = 0
	h.consecutiveReadFailures = 0
	h.consecutiveNotInitializedRPC = 0
	h.readFailureTimes = nil
	h.notInitializedTimes = nil
	h.lastReconnectAt = now
}

func (s appServerHealthSnapshot) asDetailsMap() map[string]any {
	return map[string]any{
		"read_failure_streak":       s.ReadFailureStreak,
		"read_errors_window":        s.ReadErrorsWindow,
		"reconnect_failure_streak":  s.ReconnectFailureStreak,
		"not_initialized_streak":    s.NotInitializedStreak,
		"circuit_open":              s.CircuitOpen,
		"circuit_remaining_ms":      s.CircuitRemainingMs,
		"total_read_failures":       s.TotalReadFailures,
		"total_reconnect_attempts":  s.TotalReconnectAttempt,
		"total_reconnect_successes": s.TotalReconnectSuccess,
		"total_reconnect_failures":  s.TotalReconnectFailure,
		"total_respawn_successes":   s.TotalRespawnSuccess,
		"total_respawn_failures":    s.TotalRespawnFailure,
		"total_circuit_trips":       s.TotalCircuitTrips,
		"total_not_initialized":     s.TotalNotInitialized,
	}
}

func recentTimesStartIndex(values []time.Time, cutoff time.Time) int {
	idx := 0
	for idx < len(values) && values[idx].Before(cutoff) {
		idx++
	}
	return idx
}

func filterRecentTimes(values []time.Time, now time.Time, window time.Duration) []time.Time {
	if len(values) == 0 {
		return values
	}
	idx := recentTimesStartIndex(values, now.Add(-window))
	if idx == 0 {
		return values
	}
	return append([]time.Time(nil), values[idx:]...)
}

func countRecentTimes(values []time.Time, now time.Time, window time.Duration) int {
	if len(values) == 0 {
		return 0
	}
	return len(values) - recentTimesStartIndex(values, now.Add(-window))
}

func (c *AppServerClient) healthSnapshotLocked(now time.Time) appServerHealthSnapshot {
	readErrorsWindow := countRecentTimes(c.health.readFailureTimes, now, appServerCircuitBreakerWindow)
	remaining := c.health.circuitOpenUntil.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	return appServerHealthSnapshot{
		ReadFailureStreak:      c.health.consecutiveReadFailures,
		ReadErrorsWindow:       readErrorsWindow,
		ReconnectFailureStreak: c.health.consecutiveReconnectFailures,
		NotInitializedStreak:   c.health.consecutiveNotInitializedRPC,
		CircuitOpen:            remaining > 0,
		CircuitRemainingMs:     remaining.Milliseconds(),
		TotalReadFailures:      c.health.totalReadFailures,
		TotalReconnectAttempt:  c.health.totalReconnectAttempt,
		TotalReconnectSuccess:  c.health.totalReconnectSuccess,
		TotalReconnectFailure:  c.health.totalReconnectFailure,
		TotalRespawnSuccess:    c.health.totalRespawnSuccess,
		TotalRespawnFailure:    c.health.totalRespawnFailure,
		TotalCircuitTrips:      c.health.totalCircuitTrips,
		TotalNotInitialized:    c.health.totalNotInitialized,
	}
}

func (c *AppServerClient) noteReadFailure(now time.Time) (appServerHealthSnapshot, bool, bool) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()

	maxWindow := appServerCircuitBreakerWindow
	if appServerRespawnEscalationWindow > maxWindow {
		maxWindow = appServerRespawnEscalationWindow
	}
	c.health.readFailureTimes = append(c.health.readFailureTimes, now)
	c.health.readFailureTimes = filterRecentTimes(c.health.readFailureTimes, now, maxWindow)
	c.health.totalReadFailures++
	c.health.consecutiveReadFailures++
	c.health.consecutiveNotInitializedRPC = 0
	c.health.lastDisconnectAt = now

	recentForCircuit := countRecentTimes(c.health.readFailureTimes, now, appServerCircuitBreakerWindow)
	recentForRespawn := countRecentTimes(c.health.readFailureTimes, now, appServerRespawnEscalationWindow)
	openedCircuit := false
	if recentForCircuit >= appServerCircuitBreakerThreshold && now.After(c.health.circuitOpenUntil) {
		c.health.circuitOpenUntil = now.Add(appServerCircuitBreakerCooldown)
		c.health.totalCircuitTrips++
		openedCircuit = true
	}
	snapshot := c.healthSnapshotLocked(now)
	return snapshot, recentForRespawn >= appServerRespawnEscalationThreshold, openedCircuit
}

func (c *AppServerClient) noteReconnectAttempt() {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	c.health.totalReconnectAttempt++
}

func (c *AppServerClient) noteReconnectSuccess(now time.Time) appServerHealthSnapshot {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	c.health.totalReconnectSuccess++
	c.health.resetAfterRecovery(now)
	return c.healthSnapshotLocked(now)
}

func (c *AppServerClient) noteReconnectFailure(now time.Time) appServerHealthSnapshot {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	c.health.totalReconnectFailure++
	c.health.consecutiveReconnectFailures++
	return c.healthSnapshotLocked(now)
}

func (c *AppServerClient) noteRespawnResult(now time.Time, success bool) appServerHealthSnapshot {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	if success {
		c.health.totalRespawnSuccess++
		c.health.resetAfterRecovery(now)
	} else {
		c.health.totalRespawnFailure++
	}
	return c.healthSnapshotLocked(now)
}

func (c *AppServerClient) circuitRemaining(now time.Time) (time.Duration, appServerHealthSnapshot) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	remaining := c.health.circuitOpenUntil.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, c.healthSnapshotLocked(now)
}

func (c *AppServerClient) shouldPreferRespawn(now time.Time) bool {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	recentForRespawn := countRecentTimes(c.health.readFailureTimes, now, appServerRespawnEscalationWindow)
	recentNotInitialized := countRecentTimes(c.health.notInitializedTimes, now, appServerRespawnEscalationWindow)
	return recentForRespawn >= appServerRespawnEscalationThreshold || recentNotInitialized >= 2
}

func (c *AppServerClient) noteNotInitializedRPC(now time.Time) (appServerHealthSnapshot, bool) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()

	c.health.totalNotInitialized++
	c.health.consecutiveNotInitializedRPC++
	c.health.notInitializedTimes = append(c.health.notInitializedTimes, now)
	c.health.notInitializedTimes = filterRecentTimes(c.health.notInitializedTimes, now, appServerRespawnEscalationWindow)

	recent := countRecentTimes(c.health.notInitializedTimes, now, appServerRespawnEscalationWindow)
	return c.healthSnapshotLocked(now), recent >= 2
}

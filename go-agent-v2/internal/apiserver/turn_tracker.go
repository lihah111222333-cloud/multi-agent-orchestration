package apiserver

import "github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"

const (
	defaultTurnWatchdogTimeout   = codexadapter.DefaultTurnWatchdogTimeout
	defaultTrackedTurnSummaryTTL = codexadapter.DefaultTrackedTurnSummaryTTL
	defaultStallThreshold        = codexadapter.DefaultStallThreshold
	defaultStallHeartbeat        = codexadapter.DefaultStallHeartbeat
)

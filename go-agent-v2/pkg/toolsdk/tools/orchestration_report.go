package tools

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultOrchestrationReportTTL      = 30 * time.Minute
	MaxOrchestrationReportSummaryRunes = 1200
)

func BuildOrchestrationCompletionReport(workerID, status, reason, summary string) string {
	worker := strings.TrimSpace(workerID)
	if worker == "" {
		worker = "unknown-agent"
	}

	st := strings.TrimSpace(status)
	if st == "" {
		st = "completed"
	}

	rs := strings.TrimSpace(reason)
	sm := strings.TrimSpace(summary)
	if sm != "" {
		sm = TruncateOrchestrationSummary(sm, MaxOrchestrationReportSummaryRunes)
	}

	lines := []string{
		fmt.Sprintf("[Auto report] Agent %s finished delegated work.", worker),
		fmt.Sprintf("status: %s", st),
	}
	if sm != "" {
		lines = append(lines, "summary: "+sm)
	}
	if rs != "" && !strings.EqualFold(rs, "turn_complete") {
		lines = append(lines, "reason: "+rs)
	}
	return strings.Join(lines, "\n")
}

func TruncateOrchestrationSummary(value string, limit int) string {
	text := strings.TrimSpace(value)
	if text == "" || limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	if limit <= 3 {
		return "..."
	}
	target := limit - 3
	if target <= 0 {
		return "..."
	}

	var builder strings.Builder
	builder.Grow(len(text))
	used := 0
	for _, r := range text {
		if used >= target {
			break
		}
		builder.WriteRune(r)
		used++
	}
	return builder.String() + "..."
}

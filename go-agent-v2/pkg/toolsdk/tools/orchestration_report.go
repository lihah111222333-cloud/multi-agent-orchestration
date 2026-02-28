package tools

import (
	"strings"
	"time"
)

const (
	DefaultOrchestrationReportTTL      = 30 * time.Minute
	MaxOrchestrationReportSummaryRunes = 1200
)

func BuildOrchestrationCompletionReport(workerID, status, reason, summary string) string {
	worker := strings.TrimSpace(workerID)
	if worker == "" { worker = "unknown-agent" }
	st := strings.TrimSpace(status)
	if st == "" { st = "completed" }

	report := "[Auto report] Agent " + worker + " finished delegated work.\nstatus: " + st
	if sm := TruncateOrchestrationSummary(summary, MaxOrchestrationReportSummaryRunes); sm != "" {
		report += "\nsummary: " + sm
	}
	if rs := strings.TrimSpace(reason); rs != "" && !strings.EqualFold(rs, "turn_complete") {
		report += "\nreason: " + rs
	}
	return report
}

func TruncateOrchestrationSummary(value string, limit int) string {
	text := strings.TrimSpace(value)
	if text == "" || limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 3 {
		return "..."
	}
	return string(runes[:limit-3]) + "..."
}

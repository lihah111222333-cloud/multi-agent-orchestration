// timeline_tokens.go — token 用量提取、计算与更新。
package uistate

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func extractFirstInt(payload map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if number, ok := extractExitCode(value); ok {
			return number, true
		}
		if text, ok := value.(string); ok {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			if number, err := json.Number(text).Int64(); err == nil {
				return int(number), true
			}
		}
	}
	return 0, false
}

func extractIntValue(value any) (int, bool) {
	if number, ok := extractExitCode(value); ok {
		return number, true
	}
	text, ok := value.(string)
	if !ok {
		return 0, false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	if number, err := json.Number(text).Int64(); err == nil {
		return int(number), true
	}
	return 0, false
}

func extractNestedValue(payload map[string]any, path ...string) (any, bool) {
	if payload == nil || len(path) == 0 {
		return nil, false
	}
	current := any(payload)
	for _, key := range path {
		nextMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := nextMap[key]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func extractFirstIntByPaths(payload map[string]any, paths ...[]string) (int, bool) {
	for _, path := range paths {
		if len(path) == 0 {
			continue
		}
		value, ok := extractNestedValue(payload, path...)
		if !ok {
			continue
		}
		if number, ok := extractIntValue(value); ok {
			return number, true
		}
	}
	return 0, false
}

func extractFirstIntDeep(payload map[string]any, keys ...string) (int, bool) {
	if payload == nil {
		return 0, false
	}
	if number, ok := extractFirstInt(payload, keys...); ok {
		return number, true
	}
	for _, key := range []string{"msg", "data", "payload"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if number, ok := extractFirstInt(nested, keys...); ok {
			return number, true
		}
	}
	return 0, false
}

func (m *RuntimeManager) updateTokenUsageLocked(threadID string, payload map[string]any, eventType, method string, ts time.Time) {
	prev := m.snapshot.TokenUsageByThread[threadID]
	next := prev
	allowInfoTotal := strings.EqualFold(strings.TrimSpace(eventType), "context_compacted") || strings.EqualFold(strings.TrimSpace(method), "thread/compacted")

	limit, hasLimit := extractContextWindow(payload)
	if hasLimit {
		next.ContextWindowTokens = limit
	}

	used, hasUsed := extractTotalUsedTokens(payload, allowInfoTotal)
	if hasUsed {
		next.UsedTokens = used
	}

	next.UsedPercent, next.LeftPercent = computeTokenPercent(next.UsedTokens, next.ContextWindowTokens)

	if ts.IsZero() {
		ts = time.Now()
	}
	next.UpdatedAt = ts.UTC().Format(time.RFC3339)
	m.snapshot.TokenUsageByThread[threadID] = next

	// ── compact 链路可观测日志 ──
	if allowInfoTotal {
		logger.Info("uistate: token update [compact]",
			logger.FieldThreadID, threadID,
			"event_type", eventType,
			"method", method,
			"has_limit", hasLimit,
			"has_used", hasUsed,
			"prev_used", prev.UsedTokens,
			"next_used", next.UsedTokens,
			"prev_window", prev.ContextWindowTokens,
			"next_window", next.ContextWindowTokens,
			"prev_pct", prev.UsedPercent,
			"next_pct", next.UsedPercent,
		)
	} else if next.UsedTokens != prev.UsedTokens || next.ContextWindowTokens != prev.ContextWindowTokens {
		logger.Debug("uistate: token update [normal]",
			logger.FieldThreadID, threadID,
			"event_type", eventType,
			"method", method,
			"prev_used", prev.UsedTokens,
			"next_used", next.UsedTokens,
			"prev_window", prev.ContextWindowTokens,
			"next_window", next.ContextWindowTokens,
			"prev_pct", prev.UsedPercent,
			"next_pct", next.UsedPercent,
		)
	}
}

// extractContextWindow looks up the context window size from structured or flat payload keys.
func extractContextWindow(payload map[string]any) (int, bool) {
	if limit, ok := extractFirstIntByPaths(payload,
		[]string{"tokenUsage", "modelContextWindow"},
		[]string{"usage", "modelContextWindow"},
		[]string{"info", "model_context_window"},
		[]string{"info", "modelContextWindow"},
	); ok && limit > 0 {
		return limit, true
	}
	if limit, ok := extractFirstIntDeep(payload, "context_window_tokens", "contextWindowTokens", "context_window", "model_context_window", "modelContextWindow", "max_input_tokens", "maxTokens", "limit_tokens", "token_limit"); ok && limit > 0 {
		return limit, true
	}
	return 0, false
}

// extractTotalUsedTokens resolves used-token count with a 6-level priority chain:
//  1. tokenUsage.last / usage.last → totalTokens
//  2. tokenUsage.total / usage.total → totalTokens
//  3. info.last_token_usage → total_tokens
//  4. [only if allowInfoTotal] info.total_token_usage → total_tokens
//  5. [only if !allowInfoTotal] flat deep search for total_tokens/usedTokens
//  6. [fallback] input + output tokens summed
func extractTotalUsedTokens(payload map[string]any, allowInfoTotal bool) (int, bool) {
	// Priority 1: structured last usage
	if total, ok := extractFirstIntByPaths(payload,
		[]string{"tokenUsage", "last", "totalTokens"},
		[]string{"tokenUsage", "last", "total_tokens"},
		[]string{"usage", "last", "totalTokens"},
		[]string{"usage", "last", "total_tokens"},
	); ok {
		return max(0, total), true
	}
	// Priority 2: structured total usage
	if total, ok := extractFirstIntByPaths(payload,
		[]string{"tokenUsage", "total", "totalTokens"},
		[]string{"tokenUsage", "total", "total_tokens"},
		[]string{"usage", "total", "totalTokens"},
		[]string{"usage", "total", "total_tokens"},
	); ok {
		return max(0, total), true
	}
	// Priority 3: info.last_token_usage
	if total, ok := extractFirstIntByPaths(payload,
		[]string{"info", "last_token_usage", "total_tokens"},
		[]string{"info", "lastTokenUsage", "totalTokens"},
	); ok {
		return max(0, total), true
	}
	// Priority 4/5: conditional gate
	if allowInfoTotal {
		if total, ok := extractFirstIntByPaths(payload,
			[]string{"info", "total_token_usage", "total_tokens"},
			[]string{"info", "totalTokenUsage", "totalTokens"},
		); ok {
			return max(0, total), true
		}
	} else if total, ok := extractFirstIntDeep(payload, "total_tokens", "totalTokens", "used_tokens", "usedTokens"); ok {
		return max(0, total), true
	}
	// Priority 6: input + output fallback
	return extractInputOutputTokens(payload, allowInfoTotal)
}

// extractInputOutputTokens sums input and output tokens as a last-resort fallback.
func extractInputOutputTokens(payload map[string]any, allowInfoTotal bool) (int, bool) {
	input, hasInput := extractFirstIntByPaths(payload,
		[]string{"tokenUsage", "last", "inputTokens"},
		[]string{"tokenUsage", "last", "input_tokens"},
		[]string{"usage", "last", "inputTokens"},
		[]string{"usage", "last", "input_tokens"},
		[]string{"tokenUsage", "total", "inputTokens"},
		[]string{"tokenUsage", "total", "input_tokens"},
		[]string{"usage", "total", "inputTokens"},
		[]string{"usage", "total", "input_tokens"},
	)
	if !hasInput {
		input, hasInput = extractFirstIntByPaths(payload,
			[]string{"info", "last_token_usage", "input_tokens"},
			[]string{"info", "lastTokenUsage", "inputTokens"},
		)
	}
	if !hasInput {
		input, hasInput = extractFirstIntDeep(payload, "input", "input_tokens", "inputTokens", "prompt_tokens")
	}
	output, hasOutput := extractFirstIntByPaths(payload,
		[]string{"tokenUsage", "last", "outputTokens"},
		[]string{"tokenUsage", "last", "output_tokens"},
		[]string{"usage", "last", "outputTokens"},
		[]string{"usage", "last", "output_tokens"},
		[]string{"tokenUsage", "total", "outputTokens"},
		[]string{"tokenUsage", "total", "output_tokens"},
		[]string{"usage", "total", "outputTokens"},
		[]string{"usage", "total", "output_tokens"},
	)
	if !hasOutput {
		output, hasOutput = extractFirstIntByPaths(payload,
			[]string{"info", "last_token_usage", "output_tokens"},
			[]string{"info", "lastTokenUsage", "outputTokens"},
		)
	}
	if (!hasInput || !hasOutput) && allowInfoTotal {
		if !hasInput {
			input, hasInput = extractFirstIntByPaths(payload,
				[]string{"info", "total_token_usage", "input_tokens"},
				[]string{"info", "totalTokenUsage", "inputTokens"},
			)
		}
		if !hasOutput {
			output, hasOutput = extractFirstIntByPaths(payload,
				[]string{"info", "total_token_usage", "output_tokens"},
				[]string{"info", "totalTokenUsage", "outputTokens"},
			)
		}
	}
	if !hasOutput {
		output, hasOutput = extractFirstIntDeep(payload, "output", "output_tokens", "outputTokens", "completion_tokens")
	}
	if hasInput || hasOutput {
		return max(0, input+output), true
	}
	return 0, false
}

// computeTokenPercent calculates used/left percentages, clamped to [0, 100].
func computeTokenPercent(usedTokens, contextWindowTokens int) (usedPct, leftPct float64) {
	if contextWindowTokens <= 0 {
		return 0, 0
	}
	usedPct = (float64(usedTokens) / float64(contextWindowTokens)) * 100
	if usedPct < 0 {
		usedPct = 0
	}
	if usedPct > 100 {
		usedPct = 100
	}
	leftPct = 100 - usedPct
	return usedPct, leftPct
}

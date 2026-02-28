package orchestrator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑 (对应 Python master.py 常量)
const (
	defaultTaskMaxChars       = 2000
	defaultArchMaxChars       = 6000
	defaultAggregatorMaxWords = 4000
	minQualityScore           = 25
)

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
var summaryUnitRe = regexp.MustCompile(`[A-Za-z0-9_]+|[\x{4e00}-\x{9fff}]`)

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
var assignmentListPrefixRe = regexp.MustCompile(`^\s*(?:[-*+]|(?:\d+)[.\)])\s*`)

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func trimTaskText(task string, maxChars int) string {
	text := strings.TrimSpace(task)
	if maxChars <= 0 {
		maxChars = defaultTaskMaxChars
	}
	if len([]rune(text)) <= maxChars {
		return text
	}
	return string([]rune(text)[:maxChars]) + "\n...(任务文本已截断)"
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func extractJSON(text string) map[string]any {
	src := strings.TrimSpace(text)
	if src == "" {
		return nil
	}

	runes := []rune(src)
	for start := 0; start < len(runes); start++ {
		if runes[start] != '{' {
			continue
		}

		stack := []rune{'}'}
		inString := false
		escaped := false

		for idx := start + 1; idx < len(runes); idx++ {
			ch := runes[idx]

			if inString {
				if escaped {
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					inString = false
				}
				continue
			}

			if ch == '"' {
				inString = true
				continue
			}
			if ch == '{' {
				stack = append(stack, '}')
				continue
			}
			if ch == '[' {
				stack = append(stack, ']')
				continue
			}
			if ch != '}' && ch != ']' {
				continue
			}

			if len(stack) == 0 {
				break
			}
			expected := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if ch != expected {
				break
			}

			if len(stack) > 0 {
				continue
			}

			candidate := string(runes[start : idx+1])
			var parsed map[string]any
			if err := json.Unmarshal([]byte(candidate), &parsed); err != nil {
				break
			}
			return parsed
		}
	}
	return nil
}

func sanitizeTopology(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	gatewaysRaw, ok := raw["gateways"]
	if !ok {
		return nil
	}
	gateways, ok := gatewaysRaw.([]any)
	if !ok || len(gateways) == 0 {
		return nil
	}

	var resultGateways []map[string]any
	gwIDs := map[string]bool{}

	for idx, gwRaw := range gateways {
		normalizedGateway, ok := sanitizeGateway(gwRaw, idx, gwIDs)
		if !ok {
			continue
		}
		agentsRaw, ok := normalizedGateway["agents_raw"].([]any)
		if !ok {
			continue
		}
		gwID, ok := normalizedGateway["id"].(string)
		if !ok {
			continue
		}

		var normalizedAgents []map[string]any
		agentIDs := map[string]bool{}
		for j, agentRaw := range agentsRaw {
			normalizedAgent, ok := sanitizeAgent(agentRaw, gwID, j, agentIDs)
			if ok {
				normalizedAgents = append(normalizedAgents, normalizedAgent)
			}
		}

		if len(normalizedAgents) == 0 {
			continue
		}

		delete(normalizedGateway, "agents_raw")
		normalizedGateway["agents"] = normalizedAgents
		resultGateways = append(resultGateways, normalizedGateway)
	}

	if len(resultGateways) == 0 {
		return nil
	}
	return map[string]any{"gateways": resultGateways}
}

func sanitizeGateway(raw any, idx int, seen map[string]bool) (map[string]any, bool) {
	gateway, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	agentsRaw, ok := gateway["agents"].([]any)
	if !ok || len(agentsRaw) == 0 {
		return nil, false
	}
	gatewayID := normalizeOptionalText(gateway["id"])
	if gatewayID == "" {
		gatewayID = fmt.Sprintf("gateway_%d", idx+1)
	}
	if seen[gatewayID] {
		return nil, false
	}
	seen[gatewayID] = true
	gatewayName := normalizeOptionalText(gateway["name"])
	if gatewayName == "" {
		gatewayName = gatewayID
	}
	gatewayDesc := normalizeOptionalText(gateway["description"])
	return map[string]any{
		"id":           gatewayID,
		"name":         gatewayName,
		"description":  gatewayDesc,
		"capabilities": extractStringSlice(gateway["capabilities"]),
		"agents_raw":   agentsRaw,
	}, true
}

func sanitizeAgent(raw any, gwID string, idx int, seen map[string]bool) (map[string]any, bool) {
	agent, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	agentID := normalizeOptionalText(agent["id"])
	if agentID == "" {
		agentID = fmt.Sprintf("%s_agent_%d", gwID, idx+1)
	}
	if seen[agentID] {
		return nil, false
	}
	seen[agentID] = true
	agentName := normalizeOptionalText(agent["name"])
	if agentName == "" {
		agentName = agentID
	}
	return map[string]any{
		"id":           agentID,
		"name":         agentName,
		"capabilities": extractStringSlice(agent["capabilities"]),
		"depends_on":   extractStringSlice(agent["depends_on"]),
	}, true
}

// extractStringSlice 安全提取 []string。
func extractStringSlice(v any) []string {
	var out []string
	switch arr := v.(type) {
	case []string:
		for _, item := range arr {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
	case []any:
		for _, item := range arr {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
	}
	return out
}

func normalizeOptionalText(v any) string {
	text := strings.TrimSpace(fmt.Sprint(v))
	if text == "<nil>" {
		return ""
	}
	return text
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func scoreOutputQuality(text string) int {
	value := strings.TrimSpace(text)
	if value == "" {
		return 0
	}
	score := scoreLengthDim(value)
	lineScore, lines := scoreLineDim(value)
	score += lineScore
	score += penalizeErrorKeywords(strings.ToLower(value))
	score += scoreDiversityDim(value)
	score += penalizeLineRepetition(lines)

	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func scoreLengthDim(value string) int {
	score := len([]rune(value)) / 20
	if score > 60 {
		return 60
	}
	return score
}

func scoreLineDim(value string) (int, []string) {
	lines := make([]string, 0, 8)
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	score := len(lines) * 2
	if score > 20 {
		score = 20
	}
	return score, lines
}

func penalizeErrorKeywords(lower string) int {
	for _, token := range []string{"超时", "失败", "error", "exception", "无法", "unknown"} {
		if strings.Contains(lower, token) {
			return -20
		}
	}
	return 0
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func scoreDiversityDim(value string) int {
	tokens := summaryUnitRe.FindAllString(value, -1)
	uniqueTokens := map[string]bool{}
	for _, token := range tokens {
		uniqueTokens[strings.ToLower(token)] = true
	}
	score := 0
	if len(uniqueTokens) >= 20 {
		score += 10
	}
	if len(tokens) < 20 {
		return score
	}
	ratio := float64(len(uniqueTokens)) / float64(len(tokens))
	if ratio < 0.30 {
		return score - 20
	}
	if ratio < 0.45 {
		return score - 10
	}
	return score
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func penalizeLineRepetition(lines []string) int {
	if len(lines) < 4 {
		return 0
	}
	uniqueLines := map[string]bool{}
	for _, line := range lines {
		normalized := normalizeWhitespace(strings.ToLower(line))
		uniqueLines[normalized] = true
	}
	lineRatio := float64(len(uniqueLines)) / float64(len(lines))
	if lineRatio < 0.50 {
		return -20
	}
	if lineRatio < 0.70 {
		return -10
	}
	return 0
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func normalizeWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func normalizeAssignmentLine(line string) string {
	text := strings.TrimSpace(line)
	if text == "" || strings.HasPrefix(text, "```") {
		return ""
	}

	text = assignmentListPrefixRe.ReplaceAllString(text, "")
	text = strings.TrimSpace(text)

	if strings.HasPrefix(text, ">") {
		text = strings.TrimSpace(text[1:])
		text = assignmentListPrefixRe.ReplaceAllString(text, "")
		text = strings.TrimSpace(text)
	}

	if len(text) >= 2 && text[0] == '`' && text[len(text)-1] == '`' {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}

	return text
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func parseAssignments(text string, gateways map[string]bool) map[string]string {
	assignments := map[string]string{}
	for _, rawLine := range strings.Split(text, "\n") {
		line := normalizeAssignmentLine(rawLine)
		if line == "" || !strings.Contains(line, "|") {
			continue
		}

		parts := strings.SplitN(line, "|", 2)
		gwID := strings.Trim(strings.TrimSpace(parts[0]), "`")
		subTask := strings.Trim(strings.TrimSpace(parts[1]), "`")

		if strings.HasSuffix(gwID, ":") {
			gwID = strings.TrimSpace(gwID[:len(gwID)-1])
		}

		if gateways[gwID] && subTask != "" {
			assignments[gwID] = subTask
		}
	}
	return assignments
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func truncateSummaryText(text string, maxUnits int) string {
	normalized := strings.TrimSpace(text)
	if normalized == "" || maxUnits <= 0 {
		return ""
	}

	matches := summaryUnitRe.FindAllStringIndex(normalized, -1)
	if len(matches) <= maxUnits {
		return normalized
	}

	cutoff := matches[maxUnits-1][1]
	clipped := strings.TrimRightFunc(normalized[:cutoff], unicode.IsSpace)
	return fmt.Sprintf("%s\n...(内容已截断，已限制在 %d 字/词以内)", clipped, maxUnits)
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func degradedTask(task string) string {
	return task + "\n\n[降级模式] Dispatcher 失败，请尽量给出互补信息并避免重复结论。"
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func fallbackAssignments(task string, gateways map[string]bool) map[string]string {
	assignments := map[string]string{}
	for gwID := range gateways {
		assignments[gwID] = degradedTask(task)
	}
	return assignments
}

//nolint:unused // DELETE_CANDIDATE[2026-02-22]: 预留给 Master 编排器调度逻辑
func gatewayPromptBrief(gwID string, gw map[string]any) string {
	joinLimited := func(items []string, limit int, sep, empty string) string {
		if len(items) == 0 {
			return empty
		}
		if len(items) > limit {
			items = items[:limit]
		}
		return strings.Join(items, sep)
	}

	desc := normalizeOptionalText(gw["description"])
	capText := joinLimited(extractStringSlice(gw["capabilities"]), 8, ", ", "未声明")

	agentMeta, _ := gw["agent_meta"].(map[string]any)
	var depRows []string
	for agentID, metaRaw := range agentMeta {
		meta, ok := metaRaw.(map[string]any)
		if !ok {
			continue
		}
		deps := extractStringSlice(meta["depends_on"])
		if len(deps) > 0 {
			depRows = append(depRows, fmt.Sprintf("%s->%s", agentID, strings.Join(deps, "+")))
		}
	}
	depText := joinLimited(depRows, 6, "; ", "无")

	name := fmt.Sprint(gw["name"])
	return fmt.Sprintf("- %s: %s (%s) | capabilities=%s | depends=%s", gwID, name, desc, capText, depText)
}

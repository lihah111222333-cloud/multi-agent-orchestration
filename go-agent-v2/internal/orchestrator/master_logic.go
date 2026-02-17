// master_logic.go — Master 编排器纯逻辑函数 (对应 Python master.py 的 9 个纯函数)。
//
// 所有函数都是无状态纯函数，可独立测试，无 LLM / DB / 网络依赖。
package orchestrator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ========================================
// 常量 & 编译时正则
// ========================================

const (
	defaultTaskMaxChars       = 2000
	defaultArchMaxChars       = 6000
	defaultAggregatorMaxWords = 4000
	minQualityScore           = 25
)

// summaryUnitRe 匹配中文字符 / 英文单词 (与 Python _SUMMARY_UNIT_RE 等价)。
var summaryUnitRe = regexp.MustCompile(`[A-Za-z0-9_]+|[\x{4e00}-\x{9fff}]`)

// assignmentListPrefixRe 去掉列表前缀 (与 Python _ASSIGNMENT_LIST_PREFIX_RE 等价)。
var assignmentListPrefixRe = regexp.MustCompile(`^\s*(?:[-*+]|(?:\d+)[.\)])\s*`)

// ========================================
// trimTaskText (对应 Python _trim_task_text)
// ========================================

// trimTaskText 截断任务文本到 maxChars。
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

// ========================================
// extractJSON (对应 Python _extract_json)
// ========================================

// extractJSON 从任意文本提取首个合法 JSON 对象 (括号匹配算法)。
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

// ========================================
// sanitizeTopology (对应 Python _sanitize_topology)
// ========================================

// sanitizeTopology 清洗 LLM 返回的拓扑提案。
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
		gw, ok := gwRaw.(map[string]any)
		if !ok {
			continue
		}

		gwID := strings.TrimSpace(fmt.Sprint(gw["id"]))
		if gwID == "" || gwID == "<nil>" {
			gwID = fmt.Sprintf("gateway_%d", idx+1)
		}
		if gwIDs[gwID] {
			continue
		}
		gwIDs[gwID] = true

		gwName := strings.TrimSpace(fmt.Sprint(gw["name"]))
		if gwName == "" || gwName == "<nil>" {
			gwName = gwID
		}
		gwDesc := strings.TrimSpace(fmt.Sprint(gw["description"]))
		if gwDesc == "<nil>" {
			gwDesc = ""
		}
		gwCaps := extractStringSlice(gw["capabilities"])

		agentsRaw, ok := gw["agents"].([]any)
		if !ok || len(agentsRaw) == 0 {
			continue
		}

		var normalizedAgents []map[string]any
		agentIDs := map[string]bool{}
		for j, agentRaw := range agentsRaw {
			agent, ok := agentRaw.(map[string]any)
			if !ok {
				continue
			}
			agentID := strings.TrimSpace(fmt.Sprint(agent["id"]))
			if agentID == "" || agentID == "<nil>" {
				agentID = fmt.Sprintf("%s_agent_%d", gwID, j+1)
			}
			if agentIDs[agentID] {
				continue
			}
			agentIDs[agentID] = true

			agentName := strings.TrimSpace(fmt.Sprint(agent["name"]))
			if agentName == "" || agentName == "<nil>" {
				agentName = agentID
			}
			normalizedAgents = append(normalizedAgents, map[string]any{
				"id":           agentID,
				"name":         agentName,
				"capabilities": extractStringSlice(agent["capabilities"]),
				"depends_on":   extractStringSlice(agent["depends_on"]),
			})
		}

		if len(normalizedAgents) == 0 {
			continue
		}

		resultGateways = append(resultGateways, map[string]any{
			"id":           gwID,
			"name":         gwName,
			"description":  gwDesc,
			"capabilities": gwCaps,
			"agents":       normalizedAgents,
		})
	}

	if len(resultGateways) == 0 {
		return nil
	}
	return map[string]any{"gateways": resultGateways}
}

// extractStringSlice 安全提取 []string。
func extractStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return []string{}
	}
	var out []string
	for _, item := range arr {
		s := strings.TrimSpace(fmt.Sprint(item))
		if s != "" && s != "<nil>" {
			out = append(out, s)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// ========================================
// scoreOutputQuality (对应 Python _score_output_quality)
// ========================================

// scoreOutputQuality 对网关输出质量评分 0–100。
func scoreOutputQuality(text string) int {
	value := strings.TrimSpace(text)
	if value == "" {
		return 0
	}

	// 长度分 (最多 60)
	score := len([]rune(value)) / 20
	if score > 60 {
		score = 60
	}

	// 行数分 (最多 20)
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	lineScore := len(lines) * 2
	if lineScore > 20 {
		lineScore = 20
	}
	score += lineScore

	// 错误关键词扣分
	lower := strings.ToLower(value)
	for _, token := range []string{"超时", "失败", "error", "exception", "无法", "unknown"} {
		if strings.Contains(lower, token) {
			score -= 20
			break
		}
	}

	// 词元多样性
	tokens := summaryUnitRe.FindAllString(value, -1)
	lowerTokens := make([]string, len(tokens))
	uniqueTokens := map[string]bool{}
	for i, t := range tokens {
		lt := strings.ToLower(t)
		lowerTokens[i] = lt
		uniqueTokens[lt] = true
	}

	if len(uniqueTokens) >= 20 {
		score += 10
	}

	if len(tokens) >= 20 {
		ratio := float64(len(uniqueTokens)) / float64(len(tokens))
		if ratio < 0.30 {
			score -= 20
		} else if ratio < 0.45 {
			score -= 10
		}
	}

	// 行重复度
	if len(lines) >= 4 {
		normalizedLines := make([]string, len(lines))
		uniqueLines := map[string]bool{}
		for i, l := range lines {
			n := normalizeWhitespace(strings.ToLower(l))
			normalizedLines[i] = n
			uniqueLines[n] = true
		}
		lineRatio := float64(len(uniqueLines)) / float64(len(normalizedLines))
		if lineRatio < 0.50 {
			score -= 20
		} else if lineRatio < 0.70 {
			score -= 10
		}
	}

	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// normalizeWhitespace 合并连续空白为单个空格。
func normalizeWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// ========================================
// normalizeAssignmentLine (对应 Python _normalize_assignment_line)
// ========================================

// normalizeAssignmentLine 标准化任务分配行。
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

// ========================================
// parseAssignments (对应 Python _parse_assignments)
// ========================================

// parseAssignments 从 LLM 文本解析 gateway→subtask 映射。
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

// ========================================
// truncateSummaryText (对应 Python _truncate_summary_text)
// ========================================

// truncateSummaryText 按词元数截断摘要文本。
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

// ========================================
// degradedTask (对应 Python _degraded_task)
// ========================================

// degradedTask 生成降级模式任务描述。
func degradedTask(task string) string {
	return task + "\n\n[降级模式] Dispatcher 失败，请尽量给出互补信息并避免重复结论。"
}

// ========================================
// fallbackAssignments (对应 Python _fallback_assignments)
// ========================================

// fallbackAssignments 降级分配: 所有 gateway 都收到降级任务。
func fallbackAssignments(task string, gateways map[string]bool) map[string]string {
	assignments := map[string]string{}
	for gwID := range gateways {
		assignments[gwID] = degradedTask(task)
	}
	return assignments
}

// ========================================
// gatewayPromptBrief (对应 Python _gateway_prompt_brief)
// ========================================

// gatewayPromptBrief 生成 gateway 摘要行。
func gatewayPromptBrief(gwID string, gw map[string]any) string {
	desc := fmt.Sprint(gw["description"])
	if desc == "<nil>" {
		desc = ""
	}

	capsRaw := extractStringSlice(gw["capabilities"])
	capText := "未声明"
	if len(capsRaw) > 0 {
		limit := 8
		if len(capsRaw) < limit {
			limit = len(capsRaw)
		}
		capText = strings.Join(capsRaw[:limit], ", ")
	}

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
	depText := "无"
	if len(depRows) > 0 {
		limit := 6
		if len(depRows) < limit {
			limit = len(depRows)
		}
		depText = strings.Join(depRows[:limit], "; ")
	}

	name := fmt.Sprint(gw["name"])
	return fmt.Sprintf("- %s: %s (%s) | capabilities=%s | depends=%s", gwID, name, desc, capText, depText)
}

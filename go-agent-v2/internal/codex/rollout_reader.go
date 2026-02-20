// rollout_reader.go — 从 codex rollout JSONL 文件读取对话历史。
package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RolloutMessage 从 rollout 文件提取的消息。
type RolloutMessage struct {
	Role      string `json:"role"`      // "user" / "assistant"
	Content   string `json:"content"`   // 纯文本内容
	Timestamp string `json:"timestamp"` // ISO8601
}

// rolloutLine rollout JSONL 单行结构。
type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// rolloutPayload response_item 的 payload。
type rolloutPayload struct {
	Type    string               `json:"type"`
	Role    string               `json:"role"`
	Content []rolloutContentItem `json:"content"`
}

// rolloutContentItem content 数组元素。
type rolloutContentItem struct {
	Type string `json:"type"` // "input_text" / "output_text"
	Text string `json:"text"`
}

// ReadRolloutMessages 从 rollout JSONL 文件提取 user/assistant 消息。
func ReadRolloutMessages(rolloutPath string) ([]RolloutMessage, error) {
	f, err := os.Open(rolloutPath)
	if err != nil {
		return nil, fmt.Errorf("open rollout file: %w", err)
	}
	defer f.Close()

	var messages []RolloutMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var line rolloutLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type != "response_item" {
			continue
		}

		var payload rolloutPayload
		if err := json.Unmarshal(line.Payload, &payload); err != nil {
			continue
		}
		if payload.Type != "message" {
			continue
		}
		if payload.Role != "user" && payload.Role != "assistant" {
			continue
		}

		text := extractRolloutText(payload.Content)
		if text == "" {
			continue
		}

		if payload.Role == "user" {
			if isSystemNoise(text) {
				continue
			}
			text = trimLSPInjection(text)
			if strings.TrimSpace(text) == "" {
				continue
			}
		}

		messages = append(messages, RolloutMessage{
			Role:      payload.Role,
			Content:   text,
			Timestamp: line.Timestamp,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan rollout file: %w", err)
	}
	return messages, nil
}

// FindRolloutPath 根据 codexThreadID 查找 rollout 文件。
//
// 分层搜索: 今天 → 近 7 天 → 全量 (兜底)。
func FindRolloutPath(codexThreadID string) (string, error) {
	if codexThreadID == "" {
		return "", fmt.Errorf("empty codex thread id")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	sessionsDir := filepath.Join(homeDir, ".codex", "sessions")
	suffix := "rollout-*-" + codexThreadID + ".jsonl"

	now := time.Now()
	todayDir := filepath.Join(sessionsDir, now.Format("2006"), now.Format("01"), now.Format("02"))
	if matches, _ := filepath.Glob(filepath.Join(todayDir, suffix)); len(matches) > 0 {
		sort.Strings(matches)
		return matches[len(matches)-1], nil
	}

	for i := 1; i <= 7; i++ {
		d := now.AddDate(0, 0, -i)
		dir := filepath.Join(sessionsDir, d.Format("2006"), d.Format("01"), d.Format("02"))
		if matches, _ := filepath.Glob(filepath.Join(dir, suffix)); len(matches) > 0 {
			sort.Strings(matches)
			return matches[len(matches)-1], nil
		}
	}

	pattern := filepath.Join(sessionsDir, "*", "*", "*", suffix)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob rollout files: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no rollout file found for thread %s", codexThreadID)
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

func extractRolloutText(content []rolloutContentItem) string {
	if len(content) == 0 {
		return ""
	}
	if len(content) == 1 {
		return content[0].Text
	}
	var sb strings.Builder
	for _, item := range content {
		sb.WriteString(item.Text)
	}
	return sb.String()
}

func isSystemNoise(text string) bool {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "# AGENTS.md"):
		return true
	case strings.HasPrefix(t, "<environment_context>"):
		return true
	case strings.HasPrefix(t, "<INSTRUCTIONS>"):
		return true
	case strings.HasPrefix(t, "<permissions instructions>"):
		return true
	default:
		return false
	}
}

func trimLSPInjection(text string) string {
	const marker = "\n已注入"
	if idx := strings.Index(text, marker); idx >= 0 {
		return text[:idx]
	}
	return text
}

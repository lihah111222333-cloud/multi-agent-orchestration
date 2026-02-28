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

	"github.com/multi-agent/go-agent-v2/pkg/util"
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
// 默认会裁剪 user 消息中的自动注入段（skill 摘要 / LSP 提示块）。
func ReadRolloutMessages(rolloutPath string) ([]RolloutMessage, error) {
	return ReadRolloutMessagesWithTrim(rolloutPath, true)
}

// ReadRolloutMessagesWithTrim 从 rollout JSONL 文件提取 user/assistant 消息。
// trimInjectedUserContent=false 时保留 user 消息中的自动注入段，便于调试。
func ReadRolloutMessagesWithTrim(rolloutPath string, trimInjectedUserContent bool) ([]RolloutMessage, error) {
	f, err := os.Open(rolloutPath)
	if err != nil {
		return nil, fmt.Errorf("open rollout file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var messages []RolloutMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 100*1024*1024) // 100 MB max — rollout 行可能含 base64 图片或大 diff

	for scanner.Scan() {
		msg, ok := parseRolloutLine(scanner.Bytes(), trimInjectedUserContent)
		if ok {
			messages = append(messages, msg)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan rollout file: %w", err)
	}
	return messages, nil
}

func parseRolloutLine(raw []byte, trimInjectedUserContent bool) (RolloutMessage, bool) {
	var line rolloutLine
	if err := json.Unmarshal(raw, &line); err != nil || line.Type != "response_item" {
		return RolloutMessage{}, false
	}

	var payload rolloutPayload
	if err := json.Unmarshal(line.Payload, &payload); err != nil {
		return RolloutMessage{}, false
	}
	if payload.Type != "message" || (payload.Role != "user" && payload.Role != "assistant") {
		return RolloutMessage{}, false
	}

	text := extractRolloutText(payload.Content)
	if text == "" {
		return RolloutMessage{}, false
	}
	if payload.Role == "user" {
		var ok bool
		text, ok = normalizeRolloutUserText(text, trimInjectedUserContent)
		if !ok {
			return RolloutMessage{}, false
		}
	}

	return RolloutMessage{
		Role:      payload.Role,
		Content:   text,
		Timestamp: line.Timestamp,
	}, true
}

func normalizeRolloutUserText(text string, trimInjectedUserContent bool) (string, bool) {
	text = util.StripLeadingSystemNoise(text)
	if strings.TrimSpace(text) == "" || util.IsSystemNoiseText(text) {
		return "", false
	}
	if trimInjectedUserContent {
		text = util.TrimInjectedLSPHint(util.TrimInjectedSkillBlock(text))
	}
	return text, strings.TrimSpace(text) != ""
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
	for i := 0; i <= 7; i++ {
		d := now.AddDate(0, 0, -i)
		dir := filepath.Join(sessionsDir, d.Format("2006"), d.Format("01"), d.Format("02"))
		if match, found, err := latestGlobMatch(filepath.Join(dir, suffix)); err == nil && found {
			return match, nil
		}
	}

	pattern := filepath.Join(sessionsDir, "*", "*", "*", suffix)
	match, found, err := latestGlobMatch(pattern)
	if err != nil {
		return "", fmt.Errorf("glob rollout files: %w", err)
	}
	if !found {
		return "", fmt.Errorf("no rollout file found for thread %s", codexThreadID)
	}
	return match, nil
}

func latestGlobMatch(pattern string) (string, bool, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", false, err
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	sort.Strings(matches)
	return matches[len(matches)-1], true, nil
}

func extractRolloutText(content []rolloutContentItem) string {
	if len(content) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, item := range content {
		sb.WriteString(item.Text)
	}
	return sb.String()
}

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

type RolloutMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type rolloutPayload struct {
	Type    string               `json:"type"`
	Role    string               `json:"role"`
	Content []rolloutContentItem `json:"content"`
}

type rolloutContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func ReadRolloutMessages(rolloutPath string) ([]RolloutMessage, error) {
	return ReadRolloutMessagesWithTrim(rolloutPath, true)
}

func ReadRolloutMessagesWithTrim(rolloutPath string, trimInjectedUserContent bool) ([]RolloutMessage, error) {
	f, err := os.Open(rolloutPath)
	if err != nil {
		return nil, fmt.Errorf("open rollout file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var messages []RolloutMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 100*1024*1024)

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
		ok := false
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
	if !trimInjectedUserContent {
		return text, true
	}
	text = util.TrimInjectedLSPHint(util.TrimInjectedSkillBlock(text))
	return text, strings.TrimSpace(text) != ""
}

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
	var sb strings.Builder
	for _, item := range content {
		sb.WriteString(item.Text)
	}
	return sb.String()
}

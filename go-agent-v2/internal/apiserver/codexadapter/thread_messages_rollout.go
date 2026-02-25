package codexadapter

import (
	"context"
	"os"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/codex"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

// LoadAllThreadMessagesFromCodexRollout loads and normalizes rollout history.
func LoadAllThreadMessagesFromCodexRollout(
	ctx context.Context,
	threadID string,
	resolveRolloutHistorySource func(context.Context, string) (string, string),
	normalizeCodexThreadID func(string) string,
	findRolloutPath func(string) (string, error),
	readRolloutMessagesWithTrim func(path string, trimInjected bool) ([]codex.RolloutMessage, error),
	showInjectedPromptInChat bool,
) ([]ThreadHistoryMessage, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return []ThreadHistoryMessage{}, nil
	}
	resolve := resolveRolloutHistorySource
	if resolve == nil {
		return []ThreadHistoryMessage{}, nil
	}
	normalize := normalizeCodexThreadID
	if normalize == nil {
		normalize = strings.TrimSpace
	}
	findRollout := findRolloutPath
	if findRollout == nil {
		findRollout = codex.FindRolloutPath
	}
	readRollout := readRolloutMessagesWithTrim
	if readRollout == nil {
		readRollout = codex.ReadRolloutMessagesWithTrim
	}

	codexThreadID, rolloutPath := resolve(ctx, threadID)
	codexThreadID = normalize(codexThreadID)
	if codexThreadID == "" {
		return []ThreadHistoryMessage{}, nil
	}

	path := strings.TrimSpace(rolloutPath)
	if path == "" {
		resolvedPath, err := findRollout(codexThreadID)
		if err != nil {
			return []ThreadHistoryMessage{}, nil
		}
		path = resolvedPath
	}
	if path == "" {
		return []ThreadHistoryMessage{}, nil
	}
	if _, err := os.Stat(path); err != nil {
		return []ThreadHistoryMessage{}, nil
	}

	trimInjected := !showInjectedPromptInChat
	rolloutMsgs, err := readRollout(path, trimInjected)
	if err != nil {
		return nil, err
	}
	if len(rolloutMsgs) == 0 {
		return []ThreadHistoryMessage{}, nil
	}

	all := make([]ThreadHistoryMessage, 0, len(rolloutMsgs))
	for i, item := range rolloutMsgs {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		createdAt := ParseRolloutTimestamp(item.Timestamp)
		eventType := ""
		if role == "assistant" {
			eventType = agentcore.EventAgentMessage
		}
		all = append(all, ThreadHistoryMessage{
			ID:        int64(i + 1),
			AgentID:   threadID,
			Role:      role,
			EventType: eventType,
			Method:    "",
			Content:   item.Content,
			Metadata:  nil,
			CreatedAt: createdAt,
		})
	}
	if len(all) == 0 {
		return []ThreadHistoryMessage{}, nil
	}
	return all, nil
}

// HistoryMessagesToRecords converts thread messages to UI history records.
func HistoryMessagesToRecords(msgs []ThreadHistoryMessage) []uistate.HistoryRecord {
	records := make([]uistate.HistoryRecord, 0, len(msgs))
	for _, msg := range msgs {
		records = append(records, uistate.HistoryRecord{
			ID:        msg.ID,
			Role:      msg.Role,
			EventType: msg.EventType,
			Method:    msg.Method,
			Content:   msg.Content,
			Metadata:  msg.Metadata,
			CreatedAt: msg.CreatedAt,
		})
	}
	return records
}

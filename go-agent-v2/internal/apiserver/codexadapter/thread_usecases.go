package codexadapter

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type ThreadHistoryMessage struct {
	ID        int64           `json:"id"`
	AgentID   string          `json:"agentId"`
	Role      string          `json:"role"`
	EventType string          `json:"eventType"`
	Method    string          `json:"method"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

// ThreadMessages 负责消息分页主流程；具体 hydration 细节由回调实现。
func (a *Adapter) ThreadMessages(ctx context.Context, threadID string, limit int, before int64) (map[string]any, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadMessages", "threadId is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	allMsgs, err := a.LoadAllThreadMessagesFromRollout(ctx, threadID)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadMessages", "load codex rollout messages")
	}
	total := int64(len(allMsgs))
	msgs := PaginateRolloutMessages(allMsgs, limit, before)
	logger.Info("thread/messages: page selected",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		"before", before,
		"limit", limit,
		"page_count", len(msgs),
		"total", total,
	)

	a.handleThreadMessagesHydration(threadID, allMsgs, msgs, before, limit)

	diffLen, timelineLen := a.threadMessagesStats(threadID)
	logger.Info("thread/messages: response prepared",
		logger.FieldAgentID, threadID, logger.FieldThreadID, threadID,
		"page_count", len(msgs),
		"total", total,
		"timeline_len", timelineLen,
		"diff_len", diffLen,
	)
	return map[string]any{
		"messages": msgs,
		"total":    total,
	}, nil
}

// ThreadArchive validates archive eligibility, archives artifacts, and persists archive state.
func (a *Adapter) ThreadArchive(ctx context.Context, threadID string) (map[string]any, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadArchive", "threadId is required")
	}
	if !a.threadExistsForArchive(ctx, threadID) {
		return nil, apperrors.Newf("Server.threadArchive", "thread %s not found", threadID)
	}
	manifest, err := a.ArchiveThreadArtifacts(ctx, threadID)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadArchive", "archive codex artifacts")
	}
	archivedAt := time.Now().UnixMilli()
	if err := a.PersistThreadArchivedState(ctx, threadID, archivedAt); err != nil {
		return nil, apperrors.Wrap(err, "Server.threadArchive", "persist archive state")
	}

	return map[string]any{
		"ok":            true,
		"threadId":      threadID,
		"archivedAt":    archivedAt,
		"codexThreadId": manifest.CodexThreadID,
		"archiveDir":    manifest.ArchiveDir,
		"rolloutPath":   manifest.RolloutPath,
		"files":         manifest.Files,
	}, nil
}

func (a *Adapter) threadExistsInRuntime(threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" || a == nil || a.ctx == nil || a.ctx.UIRuntime == nil {
		return false
	}
	for _, item := range a.ctx.UIRuntime.SnapshotLight().Threads {
		if strings.TrimSpace(item.ID) == id {
			return true
		}
	}
	return false
}

func (a *Adapter) threadExistsForArchive(ctx context.Context, threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	if a != nil && a.ctx != nil {
		if mgr := a.ctx.Manager; mgr != nil && mgr.Get(id) != nil {
			return true
		}
		if a.threadExistsInRuntime(id) {
			return true
		}
	}
	return a.ThreadExistsInHistory(ctx, id)
}

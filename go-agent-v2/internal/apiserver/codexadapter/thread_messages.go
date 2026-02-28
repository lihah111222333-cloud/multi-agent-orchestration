package codexadapter

import (
	"context"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	lifecyclesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/lifecycle"
	messagessvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/messages"
	rolloutsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/rollout"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const prefKeyShowInjectedPromptInChat = "settings.showInjectedPromptInChat"

func (a *Adapter) ThreadMessages(ctx context.Context, threadID string, limit int, before int64) (map[string]any, error) {
	id, err := requireThreadID("Server.threadMessages", threadID)
	if err != nil { return nil, err }
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	allMsgs, err := messagessvc.LoadAllThreadMessagesFromCodexRollout(
		ctx,
		id,
		a.resolveRolloutHistorySource,
		lifecyclesvc.NormalizeCodexThreadID,
		a.showInjectedPromptInChat(ctx),
	)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadMessages", "load codex rollout messages")
	}
	total := int64(len(allMsgs))
	msgs := messagessvc.PaginateRolloutMessages(allMsgs, limit, before)
	logger.Info("thread/messages: page selected", append(threadLogFields(id), "before", before, "limit", limit, "page_count", len(msgs), "total", total)...)

	diffLen, timelineLen := 0, 0
	if runtime := a.uiRuntime(); runtime != nil {
		messagessvc.HandleThreadMessagesHydration(
			id,
			allMsgs,
			msgs,
			before,
			messagessvc.CalculateHydrationLoadLimit,
			func(threadID string, records []messagessvc.ThreadHistoryMessage) bool {
				return runtime.HydrateHistory(threadID, toHistoryRecords(records))
			},
			func(threadID string, all []messagessvc.ThreadHistoryMessage, firstPage []messagessvc.ThreadHistoryMessage, loadLimit int) {
				messagessvc.StreamRemainingHistory(
					threadID,
					all,
					firstPage,
					loadLimit,
					messagessvc.ThreadMessageHydrationPageSize,
					messagessvc.PaginateRolloutMessages,
					func(threadID string, records []messagessvc.ThreadHistoryMessage) {
						runtime.AppendHistory(threadID, toHistoryRecords(records))
					},
					func(id string) int { return len(runtime.ThreadDiff(id)) },
					func(id string) int { return len(runtime.ThreadTimeline(id)) },
					func(id string, totalLoaded int, pages int) {
						a.notify("thread/messages/page", messagessvc.BuildThreadMessagesPagePayload(id, totalLoaded, pages))
					},
				)
			},
			util.SafeGo,
		)
		diffLen, timelineLen = len(runtime.ThreadDiff(id)), len(runtime.ThreadTimeline(id))
	}
	logger.Info("thread/messages: response prepared", append(threadLogFields(id), "page_count", len(msgs), "total", total, "timeline_len", timelineLen, "diff_len", diffLen)...)
	return messagessvc.BuildThreadMessagesResponse(msgs, total), nil
}

func (a *Adapter) showInjectedPromptInChat(ctx context.Context) bool {
	store := a.store()
	if store == nil { return false }
	value, err := store.Get(ctx, prefKeyShowInjectedPromptInChat)
	if err != nil { logger.Warn("ui preferences: load injected prompt visibility failed", logger.FieldError, err); return false }
	return messagessvc.ParsePreferenceBool(value, false)
}

func (a *Adapter) resolveRolloutHistorySource(ctx context.Context, threadID string) (string, string) {
	return rolloutsvc.ResolveRolloutHistorySource(
		ctx,
		threadID,
		func(threadID string) string {
			return rolloutsvc.RunningCodexThreadIDFromManager(threadID, a.managerProcess, func(proc any) string {
				typed, _ := proc.(*codexsdk.AgentProcess)
				return a.GetThreadID(typed)
			})
		},
		func(ctx context.Context, agentID string) (string, string, error) {
			binding, err := a.findBindingByAgentID(ctx, agentID)
			if err != nil || binding == nil { return "", "", err }
			return binding.CodexThreadID, binding.RolloutPath, nil
		},
		func(ctx context.Context, agentID string) (string, error) {
			status, err := a.findStatusByAgentID(ctx, agentID)
			if err != nil || status == nil { return "", err }
			return status.SessionID, nil
		},
		lifecyclesvc.NormalizeCodexThreadID,
	)
}

func toHistoryRecords(msgs []messagessvc.ThreadHistoryMessage) []uistate.HistoryRecord { return mapSlice(msgs, func(msg messagessvc.ThreadHistoryMessage) uistate.HistoryRecord { return uistate.HistoryRecord{ID: msg.ID, Role: msg.Role, EventType: msg.EventType, Method: msg.Method, Content: msg.Content, Metadata: msg.Metadata, CreatedAt: msg.CreatedAt} }) }

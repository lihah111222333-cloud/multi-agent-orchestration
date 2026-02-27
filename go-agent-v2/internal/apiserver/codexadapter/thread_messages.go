package codexadapter

import (
	"context"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk"
	historyconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/history"
	messagesconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/messages"
	lifecyclesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/lifecycle"
	rolloutsvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/rollout"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

const (
	prefKeyShowInjectedPromptInChat = "settings.showInjectedPromptInChat"
)

// ThreadMessages handles rollout history paging and runtime hydration.
func (a *Adapter) ThreadMessages(ctx context.Context, threadID string, limit int, before int64) (map[string]any, error) {
	id, err := requireThreadID("Server.threadMessages", threadID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	allMsgs, err := messagesconsumer.LoadAllThreadMessagesFromCodexRollout(
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
	msgs := messagesconsumer.PaginateRolloutMessages(allMsgs, limit, before)
	logger.Info("thread/messages: page selected", append(threadLogFields(id), "before", before, "limit", limit, "page_count", len(msgs), "total", total)...)

	runtime := a.uiRuntime()
	if runtime != nil {
		messagesconsumer.HandleThreadMessagesHydration(
			id,
			allMsgs,
			msgs,
			before,
			messagesconsumer.CalculateHydrationLoadLimit,
			runtime.HydrateHistory,
			func(threadID string, all []messagesconsumer.ThreadHistoryMessage, firstPage []messagesconsumer.ThreadHistoryMessage, loadLimit int) {
				messagesconsumer.StreamRemainingHistory(
					threadID,
					all,
					firstPage,
					loadLimit,
					messagesconsumer.ThreadMessageHydrationPageSize,
					messagesconsumer.PaginateRolloutMessages,
					runtime.AppendHistory,
					func(id string) int { return len(runtime.ThreadDiff(id)) },
					func(id string) int { return len(runtime.ThreadTimeline(id)) },
					func(id string, totalLoaded int, pages int) {
						a.notify("thread/messages/page", messagesconsumer.BuildThreadMessagesPagePayload(id, totalLoaded, pages))
					},
				)
			},
			util.SafeGo,
		)
	}
	diffLen, timelineLen := 0, 0
	if runtime != nil {
		diffLen, timelineLen = len(runtime.ThreadDiff(id)), len(runtime.ThreadTimeline(id))
	}
	logger.Info("thread/messages: response prepared", append(threadLogFields(id), "page_count", len(msgs), "total", total, "timeline_len", timelineLen, "diff_len", diffLen)...)
	return messagesconsumer.BuildThreadMessagesResponse(msgs, total), nil
}

func (a *Adapter) showInjectedPromptInChat(ctx context.Context) bool {
	store := a.store()
	if store == nil {
		return false
	}
	value, err := store.Get(ctx, prefKeyShowInjectedPromptInChat)
	if err != nil {
		logger.Warn("ui preferences: load injected prompt visibility failed", logger.FieldError, err)
		return false
	}
	return messagesconsumer.ParsePreferenceBool(value, false)
}

func (a *Adapter) resolveRolloutHistorySource(ctx context.Context, threadID string) (string, string) {
	runningCodexThreadID := func(threadID string) string {
		return rolloutsvc.RunningCodexThreadIDFromManager(threadID, a.managerProcess, func(proc any) string {
			typed, _ := proc.(*codexsdk.AgentProcess)
			return a.GetThreadID(typed)
		})
	}
	bindingRolloutSourceByAgentID := func(ctx context.Context, agentID string) (string, string, error) {
		bindingStore := a.bindingStore()
		if bindingStore == nil {
			return "", "", nil
		}
		binding, err := bindingStore.FindByAgentID(ctx, agentID)
		if err != nil {
			return "", "", err
		}
		if binding == nil {
			return "", "", nil
		}
		return binding.CodexThreadID, binding.RolloutPath, nil
	}
	statusSessionIDByAgentID := func(ctx context.Context, agentID string) (string, error) {
		return historyconsumer.StatusSessionIDByAgentID(ctx, a.statusStore(), agentID)
	}
	return rolloutsvc.ResolveRolloutHistorySource(
		ctx,
		threadID,
		runningCodexThreadID,
		bindingRolloutSourceByAgentID,
		statusSessionIDByAgentID,
		lifecyclesvc.NormalizeCodexThreadID,
	)
}

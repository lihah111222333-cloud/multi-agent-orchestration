package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
)

func (a *Adapter) appendTurnStartUserTimeline(ctx context.Context, input []contracts.TurnInput, opt contracts.TurnAppendUserTimelineOptions) {
	if a == nil || a.ctx == nil || a.ctx.UIRuntime == nil {
		return
	}
	attachments := BuildUserTimelineAttachmentsFromInputs(input)
	if len(attachments) == 0 {
		attachments = BuildUserTimelineAttachments(opt.Images, opt.Files)
	}
	showInjected := a.showInjectedPromptInChat(ctx)
	appendInjectedHint := showInjected && !a.threadTimelineAlreadyShowsInjectedPrompt(opt.ThreadID)
	injectedHint := ""
	if appendInjectedHint {
		injectedHint = a.ResolveLSPUsagePromptHint(ctx, defaultLSPUsagePromptHint, maxLSPUsagePromptHintLen)
	}
	timelineText := ComposeUserTimelineTextForTurn(opt.Prompt, opt.SubmitPrompt, injectedHint, showInjected)
	a.ctx.UIRuntime.AppendUserMessage(opt.ThreadID, timelineText, attachments)
}

func (a *Adapter) threadTimelineAlreadyShowsInjectedPrompt(threadID string) bool {
	if a == nil || a.ctx == nil || a.ctx.UIRuntime == nil {
		return false
	}
	const marker = "\n已注入"
	for _, item := range a.ctx.UIRuntime.ThreadTimeline(threadID) {
		if item.Kind != "user" {
			continue
		}
		if strings.Contains(item.Text, marker) {
			return true
		}
	}
	return false
}

func ComposeUserTimelineTextForTurn(prompt, submitPrompt, injectedHint string, showInjected bool) string {
	if !showInjected {
		return prompt
	}
	hint := strings.TrimSpace(injectedHint)
	if hint == "" {
		return submitPrompt
	}
	if strings.Contains(submitPrompt, hint) {
		return submitPrompt
	}
	return commonadapter.MergePromptText(submitPrompt, hint)
}

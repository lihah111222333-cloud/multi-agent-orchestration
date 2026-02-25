package apiserver

import (
	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
)

func toCodexTurnInputsForTest(inputs []UserInput) []contracts.TurnInput {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]contracts.TurnInput, 0, len(inputs))
	for _, inp := range inputs {
		out = append(out, contracts.TurnInput{
			Type:    inp.Type,
			Text:    inp.Text,
			URL:     inp.URL,
			Path:    inp.Path,
			Name:    inp.Name,
			Content: inp.Content,
		})
	}
	return out
}

// 兼容历史测试：运行时代码已迁移到 codexadapter。
func extractInputs(inputs []UserInput) (prompt string, images, files []string) {
	return codexadapter.ExtractTurnInputs(toCodexTurnInputsForTest(inputs))
}

func buildAttachmentName(path string) string {
	return codexadapter.BuildAttachmentName(path)
}

func buildAttachmentPreviewURL(path string) string {
	return codexadapter.BuildAttachmentPreviewURL(path)
}

func buildUserTimelineAttachments(images, files []string) []uistate.TimelineAttachment {
	return codexadapter.BuildUserTimelineAttachments(images, files)
}

func buildUserTimelineAttachmentsFromInputs(inputs []UserInput) []uistate.TimelineAttachment {
	return codexadapter.BuildUserTimelineAttachmentsFromInputs(toCodexTurnInputsForTest(inputs))
}

func isLikelyCodexThreadID(raw string) bool {
	return codexadapter.IsLikelyCodexThreadID(raw)
}

func normalizeCodexThreadID(raw string) string {
	return codexadapter.NormalizeCodexThreadID(raw)
}

func appendUniqueThreadID(dst []string, seen map[string]struct{}, candidate string) []string {
	id := normalizeCodexThreadID(candidate)
	if id == "" {
		return dst
	}
	if _, ok := seen[id]; ok {
		return dst
	}
	seen[id] = struct{}{}
	return append(dst, id)
}

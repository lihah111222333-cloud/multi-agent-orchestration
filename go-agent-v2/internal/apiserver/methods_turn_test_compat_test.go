package apiserver

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/skillutil"
)

// 兼容历史测试：运行时代码已迁移到 Server.commonAdapter 边界。
func skillInputText(name, content string) string {
	return commonadapter.SkillInputText(name, content)
}

func fileContentInputText(name, content string) string {
	return commonadapter.FileContentInputText(name, content)
}

func mergePromptText(prompt, extra string) string {
	return commonadapter.MergePromptText(prompt, extra)
}

func composeUserTimelineTextForTurn(prompt, submitPrompt, injectedHint string, showInjected bool) string {
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
	return mergePromptText(submitPrompt, hint)
}

func fuzzyMatch(text, pattern string) bool {
	return commonadapter.FuzzyMatch(text, pattern)
}

func collectInputSkillNames(inputs []UserInput) map[string]struct{} {
	return skillutil.CollectInputSkillNames(
		inputs,
		func(input UserInput) string { return input.Type },
		func(input UserInput) string { return input.Name },
	)
}

func collectSkillNameSet(raw []string) map[string]struct{} {
	return commonadapter.CollectSkillNameSet(raw)
}

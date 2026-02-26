package codexadapter

import (
	"context"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"strings"
)

func passthroughSkillInputText(_ string, content string) string {
	return content
}

// buildSelectedSkillPrompt reads skill contents and joins them into a prompt string.
func buildSelectedSkillPrompt(
	selectedSkills []string,
	readSkillContent func(skillName string) (string, error),
	skillInputText func(name, content string) string,
) (string, int) {
	if readSkillContent == nil {
		return "", 0
	}
	ordered := collectTrimmedUniqueValues(selectedSkills, func(value string) string {
		return strings.ToLower(value)
	})
	if len(ordered) == 0 {
		return "", 0
	}

	texts := make([]string, 0, len(ordered))
	inputText := skillInputText
	if inputText == nil {
		inputText = passthroughSkillInputText
	}
	for _, skillName := range ordered {
		content, err := readSkillContent(skillName)
		if err != nil {
			logger.Warn("turn/start: selected skill unavailable, skip",
				logger.FieldSkill, skillName,
				logger.FieldError, err,
			)
			continue
		}
		texts = append(texts, inputText(skillName, content))
	}
	if len(texts) == 0 {
		return "", 0
	}
	return strings.Join(texts, "\n"), len(texts)
}

// resolveLSPUsagePromptHint resolves the user-configured LSP usage prompt hint.
func resolveLSPUsagePromptHint(
	ctx context.Context,
	defaultHint string,
	maxHintLen int,
	getPref func(context.Context, string) (any, error),
) string {
	if getPref == nil {
		return defaultHint
	}
	value, err := getPref(ctx, "lsp_usage_prompt_hint")
	if err != nil {
		logger.Warn("lsp hint: load preference failed", logger.FieldError, err)
		return defaultHint
	}
	hint := ""
	if s, ok := value.(string); ok {
		hint = strings.TrimSpace(s)
	}
	if hint == "" {
		return defaultHint
	}
	if maxHintLen > 0 && len(hint) > maxHintLen {
		logger.Warn("lsp hint: invalid preference fallback to default",
			"hint_len", len(hint), "max_len", maxHintLen)
		return defaultHint
	}
	return hint
}

// prependLSPAvailabilityWarning adds a warning when referenced LSP tools are unavailable.
func prependLSPAvailabilityWarning(
	hint string,
	dynamicToolNames map[string]struct{},
	collectReferencedToolNames func(string) []string,
	mergePromptText func(string, string) string,
) (string, []string) {
	collectRefs := collectReferencedToolNames
	if collectRefs == nil {
		return hint, nil
	}
	referenced := collectRefs(hint)
	if len(referenced) == 0 {
		return hint, nil
	}
	missing := make([]string, 0, len(referenced))
	for _, name := range referenced {
		if _, ok := dynamicToolNames[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 {
		return hint, nil
	}
	warning := "注意：当前会话未注入以下 LSP 工具（无可用 language server）：" +
		strings.Join(missing, ", ") +
		"。不要调用这些工具，请改用当前可用工具完成任务。"
	merge := mergePromptText
	if merge == nil {
		return warning + "\n" + hint, missing
	}
	return merge(warning, hint), missing
}

func logAutoMatchedSkillReadError(agentID, skillName, matchedBy string, readErr error) {
	logger.Warn("turn/start: auto-matched skill unavailable, skip",
		append(threadLogFields(agentID),
			logger.FieldSkill, skillName,
			"matched_by", matchedBy,
			logger.FieldError, readErr,
		)...,
	)
}

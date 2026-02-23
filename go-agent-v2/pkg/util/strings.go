package util

import "strings"

// FirstNonEmpty 返回第一个非空 (trim 后) 的字符串。
//
// 用于统一多处重复的 firstNonEmpty / firstTrackedTurnNonEmpty 模式。
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

const systemNoiseTrimLeftCutset = "\ufeff \t\r\n"

var systemNoiseTagPairs = []struct {
	open  string
	close string
}{
	{open: "<environment_context>", close: "</environment_context>"},
	{open: "<instructions>", close: "</instructions>"},
	{open: "<permissions instructions>", close: "</permissions instructions>"},
}

// IsSystemNoiseText 判断文本是否是系统注入噪声（AGENTS.md / environment_context / INSTRUCTIONS 等）。
func IsSystemNoiseText(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.HasPrefix(t, "# agents.md"):
		return true
	case strings.HasPrefix(t, "<environment_context>"):
		return true
	case strings.HasPrefix(t, "<instructions>"):
		return true
	case strings.HasPrefix(t, "<permissions instructions>"):
		return true
	default:
		return false
	}
}

// StripLeadingSystemNoise 去除文本开头连续的系统注入块，保留后续真实用户内容。
func StripLeadingSystemNoise(text string) string {
	current := text
	for {
		next, stripped := stripOneLeadingSystemNoise(current)
		if !stripped {
			return current
		}
		current = next
		if strings.TrimSpace(current) == "" {
			return ""
		}
	}
}

func stripOneLeadingSystemNoise(text string) (string, bool) {
	trimmed := strings.TrimLeft(text, systemNoiseTrimLeftCutset)
	lower := strings.ToLower(trimmed)

	if strings.HasPrefix(lower, "# agents.md") {
		return stripAgentsMDBlock(trimmed), true
	}
	for _, pair := range systemNoiseTagPairs {
		if strings.HasPrefix(lower, pair.open) {
			return stripTagBlock(trimmed, pair.close), true
		}
	}
	return text, false
}

func stripTagBlock(text, closeTag string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, closeTag)
	if idx < 0 {
		return ""
	}
	return strings.TrimLeft(text[idx+len(closeTag):], systemNoiseTrimLeftCutset)
}

func stripAgentsMDBlock(text string) string {
	const closeInstructions = "</instructions>"
	lower := strings.ToLower(text)
	if idx := strings.Index(lower, closeInstructions); idx >= 0 {
		return strings.TrimLeft(text[idx+len(closeInstructions):], systemNoiseTrimLeftCutset)
	}

	if idx, width := firstBlankLine(text); idx >= 0 {
		return strings.TrimLeft(text[idx+width:], systemNoiseTrimLeftCutset)
	}
	return ""
}

func firstBlankLine(text string) (idx int, width int) {
	lf := strings.Index(text, "\n\n")
	crlf := strings.Index(text, "\r\n\r\n")
	switch {
	case lf < 0 && crlf < 0:
		return -1, 0
	case lf >= 0 && (crlf < 0 || lf < crlf):
		return lf, 2
	default:
		return crlf, 4
	}
}

package util

import "strings"

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

func IsSystemNoiseText(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(t, "# agents.md") {
		return true
	}
	for _, pair := range systemNoiseTagPairs {
		if strings.HasPrefix(t, pair.open) { return true }
	}
	return false
}

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
	if idx < 0 { return "" }
	return strings.TrimLeft(text[idx+len(closeTag):], systemNoiseTrimLeftCutset)
}

func stripAgentsMDBlock(text string) string {
	const closeInstructions = "</instructions>"
	lower := strings.ToLower(text)
	if idx := strings.Index(lower, closeInstructions); idx >= 0 {
		return strings.TrimLeft(text[idx+len(closeInstructions):], systemNoiseTrimLeftCutset)
	}
	if idx, width := firstBlankLine(text); idx < 0 { return "" } else { return strings.TrimLeft(text[idx+width:], systemNoiseTrimLeftCutset) }
}

func firstBlankLine(text string) (idx int, width int) {
	lf := strings.Index(text, "\n\n")
	crlf := strings.Index(text, "\r\n\r\n")
	if lf >= 0 && (crlf < 0 || lf < crlf) { return lf, 2 }
	if crlf >= 0 { return crlf, 4 }
	return -1, 0
}

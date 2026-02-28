package commonadapter

import (
	"strings"
)

func SkillInputText(name, content string) string {
	return "[skill:" + strings.TrimSpace(name) + "] " + content
}

func FileContentInputText(name, content string) string {
	if content = strings.TrimSpace(content); content == "" {
		return ""
	}
	if name = strings.TrimSpace(name); name == "" {
		return content
	}
	return "[file:" + name + "]\n" + content
}

func MergePromptText(prompt, extra string) string {
	if strings.TrimSpace(extra) == "" {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return extra
	}
	return prompt + "\n" + extra
}

func FuzzyMatch(text, pattern string) bool {
	pi := 0
	for i := 0; i < len(text) && pi < len(pattern); i++ {
		if text[i] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

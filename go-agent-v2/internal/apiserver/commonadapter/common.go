package commonadapter

import (
	"fmt"
	"strings"
)

// Adapter 承载 apiserver 的非 codex 通用逻辑。
type Adapter struct{}

// New 创建通用适配器。
func New() *Adapter {
	return &Adapter{}
}

// SkillInputText 构建 skill 注入文本。
func (a *Adapter) SkillInputText(name, content string) string {
	return SkillInputText(name, content)
}

// FileContentInputText 构建文件内容注入文本。
func (a *Adapter) FileContentInputText(name, content string) string {
	return FileContentInputText(name, content)
}

// MergePromptText 合并主 prompt 与追加提示。
func (a *Adapter) MergePromptText(prompt, extra string) string {
	return MergePromptText(prompt, extra)
}

// FuzzyMatch 子序列模糊匹配。
func (a *Adapter) FuzzyMatch(text, pattern string) bool {
	return FuzzyMatch(text, pattern)
}

// SkillInputText 构建 skill 注入文本。
func SkillInputText(name, content string) string {
	return fmt.Sprintf("[skill:%s] %s", strings.TrimSpace(name), content)
}

// FileContentInputText 构建 fileContent 文本。
func FileContentInputText(name, content string) string {
	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		return ""
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return trimmedContent
	}
	return fmt.Sprintf("[file:%s]\n%s", trimmedName, trimmedContent)
}

// MergePromptText 合并主 prompt 与追加提示。
func MergePromptText(prompt, extra string) string {
	trimmedExtra := strings.TrimSpace(extra)
	if trimmedExtra == "" {
		return prompt
	}
	trimmedPrompt := strings.TrimSpace(prompt)
	if trimmedPrompt == "" {
		return extra
	}
	return prompt + "\n" + extra
}

// FuzzyMatch 子序列模糊匹配。
func FuzzyMatch(text, pattern string) bool {
	pi := 0
	for i := 0; i < len(text) && pi < len(pattern); i++ {
		if text[i] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

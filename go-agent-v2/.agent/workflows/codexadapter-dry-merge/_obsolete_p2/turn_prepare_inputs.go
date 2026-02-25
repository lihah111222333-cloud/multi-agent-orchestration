package codexadapter

import (
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/commonadapter"
	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func ExtractTurnInputs(inputs []contracts.TurnInput) (prompt string, images, files []string) {
	var texts []string
	for _, inp := range inputs {
		switch strings.ToLower(strings.TrimSpace(inp.Type)) {
		case "text":
			text := util.StripLeadingSystemNoise(inp.Text)
			if strings.TrimSpace(text) != "" {
				texts = append(texts, text)
			}
		case "image":
			if value := strings.TrimSpace(inp.URL); value != "" {
				images = append(images, value)
				continue
			}
			if value := strings.TrimSpace(inp.Path); value != "" {
				images = append(images, value)
			}
		case "localimage":
			if value := strings.TrimSpace(inp.URL); isRemoteImageURL(value) {
				images = append(images, value)
				continue
			}
			if value := strings.TrimSpace(inp.Path); value != "" {
				images = append(images, value)
			}
		case "filecontent":
			if value := strings.TrimSpace(inp.Path); value != "" {
				files = append(files, value)
				continue
			}
			if inline := commonadapter.FileContentInputText(inp.Name, inp.Content); inline != "" {
				texts = append(texts, inline)
			}
		case "mention", "file":
			if value := strings.TrimSpace(inp.Path); value != "" {
				files = append(files, value)
			}
		case "skill":
			// 技能注入统一由 selectedSkills 处理，避免透传输入中的摘要内容。
			continue
		}
	}
	prompt = strings.Join(texts, "\n")
	return
}

func isRemoteImageURL(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "data:image/")
}

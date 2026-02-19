package service

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// SkillInfo Skill 目录元数据。
type SkillInfo struct {
	Name         string   `json:"name"`
	Dir          string   `json:"dir"`
	Description  string   `json:"description"` // 从 SKILL.md frontmatter 提取
	TriggerWords []string `json:"trigger_words,omitempty"`
	ForceWords   []string `json:"force_words,omitempty"`
}

// SkillService 扫描 .agent/skills/ 文件系统。
type SkillService struct {
	dir string
}

// NewSkillService 创建 SkillService。
func NewSkillService(dir string) *SkillService {
	return &SkillService{dir: dir}
}

// ListSkills 扫描目录并返回所有 Skill 信息。
func (s *SkillService) ListSkills() ([]SkillInfo, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var skills []SkillInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(s.dir, entry.Name(), "SKILL.md")
		meta := extractSkillMetadata(skillPath)
		info := SkillInfo{
			Name: entry.Name(),
			Dir:  filepath.Join(s.dir, entry.Name()),
			// 尝试读取 SKILL.md frontmatter 元数据
			Description:  meta.Description,
			TriggerWords: meta.TriggerWords,
			ForceWords:   meta.ForceWords,
		}
		skills = append(skills, info)
	}
	return skills, nil
}

// ReadSkillContent 读取 SKILL.md 完整内容。
//
// 含路径遍历防护: 拒绝包含 "/", "\", ".." 的名称。
func (s *SkillService) ReadSkillContent(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", apperrors.Newf("SkillService.ReadSkillContent", "invalid skill name: %q", name)
	}
	path := filepath.Join(s.dir, name, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type skillMetadata struct {
	Description  string
	TriggerWords []string
	ForceWords   []string
}

// extractSkillMetadata 从 SKILL.md frontmatter 提取描述与关键字元数据。
func extractSkillMetadata(path string) skillMetadata {
	data, err := os.ReadFile(path)
	if err != nil {
		return skillMetadata{}
	}
	return parseSkillMetadata(string(data))
}

func parseSkillMetadata(content string) skillMetadata {
	frontmatter, ok := extractFrontmatter(content)
	if !ok {
		return skillMetadata{}
	}

	lines := strings.Split(frontmatter, "\n")
	meta := skillMetadata{}
	for idx := 0; idx < len(lines); idx++ {
		line := strings.TrimSpace(lines[idx])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])
		switch key {
		case "description":
			meta.Description = parseFrontmatterScalar(value)
		case "trigger_words", "triggerwords", "trigger_words_list", "triggers":
			words, consumed := parseFrontmatterWords(value, lines[idx+1:])
			meta.TriggerWords = words
			idx += consumed
		case "force_words", "forcewords", "mandatory_words", "must_words":
			words, consumed := parseFrontmatterWords(value, lines[idx+1:])
			meta.ForceWords = words
			idx += consumed
		case "aliases", "alias", "tags", "tag", "keywords", "keyword":
			words, consumed := parseFrontmatterWords(value, lines[idx+1:])
			meta.TriggerWords = append(meta.TriggerWords, words...)
			idx += consumed
		}
	}

	meta.Description = truncateRunes(meta.Description, 120)
	meta.TriggerWords = uniqueWords(meta.TriggerWords)
	meta.ForceWords = uniqueWords(meta.ForceWords)
	return meta
}

func extractFrontmatter(content string) (string, bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", false
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func parseFrontmatterWords(value string, tail []string) ([]string, int) {
	if strings.TrimSpace(value) != "" {
		return parseWordsFromValue(value), 0
	}
	words := make([]string, 0, len(tail))
	consumed := 0
	for _, raw := range tail {
		line := strings.TrimSpace(raw)
		if line == "" {
			consumed++
			continue
		}
		if !strings.HasPrefix(line, "-") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if item != "" {
			words = append(words, parseFrontmatterScalar(item))
		}
		consumed++
	}
	return words, consumed
}

func parseWordsFromValue(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		inside := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
		if inside == "" {
			return nil
		}
		parts := strings.Split(inside, ",")
		words := make([]string, 0, len(parts))
		for _, part := range parts {
			item := parseFrontmatterScalar(part)
			if item != "" {
				words = append(words, item)
			}
		}
		return words
	}
	normalizedComma := strings.NewReplacer("，", ",", "、", ",", ";", ",", "；", ",", "\n", ",").Replace(trimmed)
	parts := strings.Split(normalizedComma, ",")
	words := make([]string, 0, len(parts))
	for _, part := range parts {
		item := parseFrontmatterScalar(part)
		if item != "" {
			words = append(words, item)
		}
	}
	return words
}

func parseFrontmatterScalar(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, "\"'")
	return strings.TrimSpace(trimmed)
}

func uniqueWords(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		word := strings.TrimSpace(item)
		if word == "" {
			continue
		}
		key := strings.ToLower(word)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, word)
	}
	return out
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if value == "" {
		return ""
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}

	const ellipsis = "..."
	ellipsisRunes := utf8.RuneCountInString(ellipsis)
	if limit <= ellipsisRunes {
		return ellipsis
	}
	maxContentRunes := limit - ellipsisRunes
	if maxContentRunes <= 0 {
		return ellipsis
	}

	var builder strings.Builder
	builder.Grow(len(value))
	usedRunes := 0
	for _, r := range value {
		if usedRunes >= maxContentRunes {
			break
		}
		builder.WriteRune(r)
		usedRunes += 1
	}

	result := builder.String() + ellipsis
	if !utf8.ValidString(result) {
		return value
	}
	return result
}

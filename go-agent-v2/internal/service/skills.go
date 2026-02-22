package service

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

const (
	maxSkillSummaryRunes      = 220
	maxSkillSectionTitleRunes = 80
	maxSkillDigestSections    = 8
)

// SkillInfo Skill 目录元数据。
type SkillInfo struct {
	Name         string   `json:"name"`
	Dir          string   `json:"dir"`
	Description  string   `json:"description"` // 从 SKILL.md frontmatter 提取
	Summary      string   `json:"summary"`     // 运行时注入与列表展示的摘要
	TriggerWords []string `json:"trigger_words,omitempty"`
	ForceWords   []string `json:"force_words,omitempty"`
}

// SkillDigest 运行时注入使用的轻量摘要。
type SkillDigest struct {
	Summary     string             `json:"summary"`
	Sections    []string           `json:"sections,omitempty"`
	SectionRefs []SkillDigestEntry `json:"section_refs,omitempty"`
}

// SkillDigestEntry 轻量段落索引（用于定位到源文件行号）。
type SkillDigestEntry struct {
	Title string `json:"title"`
	File  string `json:"file"`
	Line  int    `json:"line"`
}

// SkillService 扫描 .agent/skills/ 文件系统。
type SkillService struct {
	dir string
}

type skillDirEntry struct {
	DirName string
	DirPath string
	Meta    skillMetadata
}

// NewSkillService 创建 SkillService。
func NewSkillService(dir string) *SkillService {
	return &SkillService{dir: dir}
}

func (s *SkillService) scanSkillDirEntries() ([]skillDirEntry, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	out := make([]skillDirEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		dirPath := filepath.Join(s.dir, dirName)
		meta := extractSkillMetadata(filepath.Join(dirPath, "SKILL.md"))
		out = append(out, skillDirEntry{
			DirName: dirName,
			DirPath: dirPath,
			Meta:    meta,
		})
	}
	return out, nil
}

func skillDisplayName(dirName string, meta skillMetadata) string {
	if name := strings.TrimSpace(meta.Name); name != "" {
		return name
	}
	return dirName
}

func matchSkillName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func (s *SkillService) resolveSkillDirName(name string) (string, error) {
	requested := strings.TrimSpace(name)
	if err := validateSkillName(requested); err != nil {
		return "", apperrors.Wrap(err, "SkillService.resolveSkillDirName", "validate skill name")
	}
	list, err := s.scanSkillDirEntries()
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", os.ErrNotExist
	}

	for _, item := range list {
		if matchSkillName(item.DirName, requested) {
			return item.DirName, nil
		}
	}
	for _, item := range list {
		if matchSkillName(item.Meta.Name, requested) {
			return item.DirName, nil
		}
	}
	return "", os.ErrNotExist
}

// ListSkills 扫描目录并返回所有 Skill 信息。
func (s *SkillService) ListSkills() ([]SkillInfo, error) {
	entries, err := s.scanSkillDirEntries()
	if err != nil {
		return nil, err
	}

	skills := make([]SkillInfo, 0, len(entries))
	for _, entry := range entries {
		meta := entry.Meta
		info := SkillInfo{
			Name: skillDisplayName(entry.DirName, meta),
			Dir:  entry.DirPath,
			// 尝试读取 SKILL.md frontmatter 元数据
			Description:  meta.Description,
			Summary:      meta.Summary,
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
	dirName, err := s.resolveSkillDirName(name)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.dir, dirName, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadSkillDigest 读取技能摘要与段落目录（用于运行时注入）。
func (s *SkillService) ReadSkillDigest(name string) (SkillDigest, error) {
	content, err := s.ReadSkillContent(name)
	if err != nil {
		return SkillDigest{}, err
	}
	meta := parseSkillMetadata(content)
	sectionRefs := extractSkillSections(content, maxSkillDigestSections)
	sections := make([]string, 0, len(sectionRefs))
	for _, item := range sectionRefs {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		sections = append(sections, title)
	}
	digest := SkillDigest{
		Summary:     strings.TrimSpace(meta.Summary),
		Sections:    sections,
		SectionRefs: sectionRefs,
	}
	if digest.Summary == "" {
		digest.Summary = "未提供摘要"
	}
	return digest, nil
}

type skillMetadata struct {
	Name          string
	Description   string
	Summary       string
	SummarySource string
	TriggerWords  []string
	ForceWords    []string
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
	meta := skillMetadata{}
	if frontmatter, ok := extractFrontmatter(content); ok {
		lines := strings.Split(frontmatter, "\n")
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
			case "name":
				meta.Name = parseFrontmatterScalar(value)
			case "description":
				meta.Description = parseFrontmatterScalar(value)
			case "summary", "digest":
				meta.Summary = parseFrontmatterScalar(value)
				if strings.TrimSpace(meta.Summary) != "" {
					meta.SummarySource = "frontmatter"
				}
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
	}

	name := strings.TrimSpace(meta.Name)
	if name != "" {
		// Frontmatter name should be reachable via explicit @mention even when
		// directory name differs from display name.
		meta.TriggerWords = append(meta.TriggerWords,
			"@"+name,
			"[skill:"+name+"]",
		)
	}

	meta.Description = truncateRunes(meta.Description, 120)
	if strings.TrimSpace(meta.Summary) == "" {
		meta.Summary = meta.Description
		if strings.TrimSpace(meta.Summary) != "" {
			meta.SummarySource = "description"
		}
	}
	if strings.TrimSpace(meta.Summary) == "" {
		meta.Summary = deriveSummaryFromBody(content)
		if strings.TrimSpace(meta.Summary) != "" {
			meta.SummarySource = "generated"
		}
	}
	meta.Summary = truncateRunes(meta.Summary, maxSkillSummaryRunes)
	meta.TriggerWords = uniqueWords(meta.TriggerWords)
	meta.ForceWords = uniqueWords(meta.ForceWords)
	return meta
}

// SummarizeSkillContent 根据技能内容提取摘要及来源。
func SummarizeSkillContent(content string) (summary, source string) {
	meta := parseSkillMetadata(content)
	return meta.Summary, meta.SummarySource
}

// UpsertSkillSummaryFrontmatter 将摘要写入（或清空）SKILL.md frontmatter 的 summary 字段。
func UpsertSkillSummaryFrontmatter(content, summary string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	summary = strings.TrimSpace(summary)

	frontmatter, body, ok := splitFrontmatterContent(normalized)
	if !ok {
		if summary == "" {
			return normalized
		}
		lines := []string{
			"---",
			"summary: " + quoteYAMLScalar(summary),
			"---",
		}
		trimmedBody := strings.TrimPrefix(normalized, "\n")
		if trimmedBody != "" {
			lines = append(lines, "", trimmedBody)
		}
		return strings.Join(lines, "\n")
	}

	lines := strings.Split(frontmatter, "\n")
	next := make([]string, 0, len(lines)+1)
	insertAt := -1
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			next = append(next, raw)
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			next = append(next, raw)
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		switch key {
		case "summary", "digest":
			continue
		case "description":
			next = append(next, raw)
			insertAt = len(next)
		case "name":
			next = append(next, raw)
			if insertAt < 0 {
				insertAt = len(next)
			}
		default:
			next = append(next, raw)
		}
	}
	if summary != "" {
		summaryLine := "summary: " + quoteYAMLScalar(summary)
		if insertAt < 0 || insertAt > len(next) {
			insertAt = len(next)
		}
		next = append(next, "")
		copy(next[insertAt+1:], next[insertAt:])
		next[insertAt] = summaryLine
	}

	rebuilt := strings.TrimSpace(strings.Join(next, "\n"))
	if rebuilt == "" {
		return body
	}
	if body == "" {
		return strings.Join([]string{"---", rebuilt, "---"}, "\n")
	}
	return strings.Join([]string{"---", rebuilt, "---", body}, "\n")
}

func splitFrontmatterContent(content string) (frontmatter, body string, ok bool) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content, false
	}
	rest := content[len("---\n"):]
	frontmatter, tail, ok := strings.Cut(rest, "\n---")
	if !ok {
		return "", content, false
	}
	return frontmatter, strings.TrimPrefix(tail, "\n"), true
}

func quoteYAMLScalar(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func extractFrontmatter(content string) (string, bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", false
	}
	rest := normalized[len("---\n"):]
	frontmatter, _, ok := strings.Cut(rest, "\n---")
	if !ok {
		return "", false
	}
	return frontmatter, true
}

func stripFrontmatter(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return normalized
	}
	rest := normalized[len("---\n"):]
	_, tail, ok := strings.Cut(rest, "\n---")
	if !ok {
		return normalized
	}
	return strings.TrimPrefix(tail, "\n")
}

func deriveSummaryFromBody(content string) string {
	body := stripFrontmatter(content)
	lines := strings.Split(body, "\n")
	inFence := false
	fragments := make([]string, 0, 4)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, ">") {
			line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		line = strings.TrimSpace(strings.TrimPrefix(line, "* "))
		line = strings.TrimSpace(strings.Trim(line, "`"))
		if line == "" {
			continue
		}
		fragments = append(fragments, line)
		if utf8.RuneCountInString(strings.Join(fragments, " ")) >= maxSkillSummaryRunes {
			break
		}
	}
	return strings.Join(fragments, " ")
}

func extractSkillSections(content string, limit int) []SkillDigestEntry {
	if limit <= 0 {
		return nil
	}

	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	sections := make([]SkillDigestEntry, 0, limit)
	seen := make(map[string]struct{}, limit)
	inFence := false
	inFrontmatter := false
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		inFrontmatter = true
	}
	for idx, raw := range lines {
		line := strings.TrimSpace(raw)
		if inFrontmatter {
			if idx > 0 && line == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" || !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		title = strings.TrimSpace(strings.Trim(title, "`"))
		if title == "" {
			continue
		}
		key := strings.ToLower(title)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sections = append(sections, SkillDigestEntry{
			Title: truncateRunes(title, maxSkillSectionTitleRunes),
			File:  "SKILL.md",
			Line:  idx + 1,
		})
		if len(sections) >= limit {
			break
		}
	}
	if len(sections) == 0 {
		return nil
	}
	return sections
}

func validateSkillName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return apperrors.Newf("validateSkillName", "invalid skill name: %q", name)
	}
	return nil
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

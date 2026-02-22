package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

const (
	maxSkillSummaryRunes      = 220
	maxSkillSectionTitleRunes = 80
	maxSkillDigestSections    = 8

	skillStoreByIDDir = "by-id"
	skillIndexFile    = "skill.json"
	skillMainFile     = "SKILL.md"

	maxSkillImportFiles          = 1000
	maxSkillImportSingleFileSize = 4 << 20  // 4MB
	maxSkillImportTotalFileSize  = 20 << 20 // 20MB
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

// SkillImportResult 目录导入结果。
type SkillImportResult struct {
	Name      string
	Dir       string
	SkillFile string
	Files     int
	Bytes     int64
}

// SkillService 统一管理技能存储。
type SkillService struct {
	dir string
}

type skillRecord struct {
	ID         string
	DirPath    string
	SkillPath  string
	StoredName string
	Meta       skillMetadata
}

type skillImportStats struct {
	Files int
	Bytes int64
}

type skillIndex struct {
	Name string `json:"name"`
}

// NewSkillService 创建 SkillService。
func NewSkillService(dir string) *SkillService {
	return &SkillService{dir: dir}
}

func (s *SkillService) byIDRoot() string {
	return filepath.Join(s.dir, skillStoreByIDDir)
}

func (s *SkillService) ensureByIDRoot() error {
	return os.MkdirAll(s.byIDRoot(), 0o755)
}

func skillStorageID(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func skillDisplayName(storedName string, meta skillMetadata, storageID string) string {
	if name := strings.TrimSpace(meta.Name); name != "" {
		return name
	}
	if name := strings.TrimSpace(storedName); name != "" {
		return name
	}
	return storageID
}

func matchSkillName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func (s *SkillService) readSkillIndex(dirPath string) skillIndex {
	path := filepath.Join(dirPath, skillIndexFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return skillIndex{}
	}
	var index skillIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return skillIndex{}
	}
	index.Name = strings.TrimSpace(index.Name)
	return index
}

func (s *SkillService) writeSkillIndex(dirPath, name string) error {
	index := skillIndex{Name: strings.TrimSpace(name)}
	data, err := json.Marshal(index)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dirPath, skillIndexFile), data, 0o644)
}

func (s *SkillService) scanSkillRecords() ([]skillRecord, error) {
	entries, err := os.ReadDir(s.byIDRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	records := make([]skillRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := strings.TrimSpace(entry.Name())
		if id == "" || strings.HasPrefix(id, ".") {
			continue
		}
		dirPath := filepath.Join(s.byIDRoot(), id)
		skillPath := filepath.Join(dirPath, skillMainFile)
		info, statErr := os.Stat(skillPath)
		if statErr != nil || info.IsDir() {
			continue
		}
		records = append(records, skillRecord{
			ID:         id,
			DirPath:    dirPath,
			SkillPath:  skillPath,
			StoredName: s.readSkillIndex(dirPath).Name,
			Meta:       extractSkillMetadata(skillPath),
		})
	}

	sort.Slice(records, func(i, j int) bool {
		left := strings.ToLower(skillDisplayName(records[i].StoredName, records[i].Meta, records[i].ID))
		right := strings.ToLower(skillDisplayName(records[j].StoredName, records[j].Meta, records[j].ID))
		if left == right {
			return records[i].ID < records[j].ID
		}
		return left < right
	})
	return records, nil
}

func (s *SkillService) resolveSkillRecord(name string) (skillRecord, error) {
	requested := strings.TrimSpace(name)
	if requested == "" {
		return skillRecord{}, apperrors.New("SkillService.resolveSkillRecord", "skill name is required")
	}
	records, err := s.scanSkillRecords()
	if err != nil {
		return skillRecord{}, err
	}
	if len(records) == 0 {
		return skillRecord{}, os.ErrNotExist
	}

	for _, record := range records {
		displayName := skillDisplayName(record.StoredName, record.Meta, record.ID)
		if matchSkillName(displayName, requested) {
			return record, nil
		}
	}
	for _, record := range records {
		if matchSkillName(record.StoredName, requested) {
			return record, nil
		}
	}
	for _, record := range records {
		if matchSkillName(record.Meta.Name, requested) {
			return record, nil
		}
	}
	for _, record := range records {
		if matchSkillName(record.ID, requested) {
			return record, nil
		}
	}
	return skillRecord{}, os.ErrNotExist
}

// ListSkills 扫描目录并返回所有 Skill 信息。
func (s *SkillService) ListSkills() ([]SkillInfo, error) {
	records, err := s.scanSkillRecords()
	if err != nil {
		return nil, err
	}

	skills := make([]SkillInfo, 0, len(records))
	for _, record := range records {
		meta := record.Meta
		skills = append(skills, SkillInfo{
			Name:         skillDisplayName(record.StoredName, meta, record.ID),
			Dir:          record.DirPath,
			Description:  meta.Description,
			Summary:      meta.Summary,
			TriggerWords: meta.TriggerWords,
			ForceWords:   meta.ForceWords,
		})
	}
	return skills, nil
}

// ReadSkillContent 读取 SKILL.md 完整内容。
func (s *SkillService) ReadSkillContent(name string) (string, error) {
	record, err := s.resolveSkillRecord(name)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(record.SkillPath)
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

// WriteSkillContent 覆盖写入技能内容并更新索引。
func (s *SkillService) WriteSkillContent(name, content string) (string, error) {
	storedName := strings.TrimSpace(name)
	if storedName == "" {
		return "", apperrors.New("SkillService.WriteSkillContent", "skill name is required")
	}
	if err := s.ensureByIDRoot(); err != nil {
		return "", err
	}
	id := skillStorageID(storedName)
	targetDir := filepath.Join(s.byIDRoot(), id)
	stagingDir := filepath.Join(s.byIDRoot(), fmt.Sprintf(".%s.write-%d", id, time.Now().UnixNano()))
	if err := os.RemoveAll(stagingDir); err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stagingDir, skillMainFile), []byte(content), 0o644); err != nil {
		return "", err
	}
	if err := s.writeSkillIndex(stagingDir, storedName); err != nil {
		return "", err
	}
	if err := activateStagedSkillDir(targetDir, stagingDir); err != nil {
		return "", err
	}
	return filepath.Join(targetDir, skillMainFile), nil
}

// UpdateSkillSummary 更新技能 frontmatter summary 字段。
func (s *SkillService) UpdateSkillSummary(name, summary string) (skillPath string, resolvedName string, err error) {
	record, err := s.resolveSkillRecord(name)
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(record.SkillPath)
	if err != nil {
		return "", "", err
	}
	summary = strings.TrimSpace(summary)
	updated := UpsertSkillSummaryFrontmatter(string(data), summary)
	if err := os.WriteFile(record.SkillPath, []byte(updated), 0o644); err != nil {
		return "", "", err
	}
	resolvedName = skillDisplayName(record.StoredName, record.Meta, record.ID)
	if resolvedName == "" {
		resolvedName = strings.TrimSpace(name)
	}
	return record.SkillPath, resolvedName, nil
}

// DeleteSkill 删除技能目录。
func (s *SkillService) DeleteSkill(name string) (resolvedName string, dir string, err error) {
	record, err := s.resolveSkillRecord(name)
	if err != nil {
		return "", "", err
	}
	if err := os.RemoveAll(record.DirPath); err != nil {
		return "", "", err
	}
	resolvedName = skillDisplayName(record.StoredName, record.Meta, record.ID)
	if resolvedName == "" {
		resolvedName = strings.TrimSpace(name)
	}
	return resolvedName, record.DirPath, nil
}

// ImportSkillDirectory 导入技能目录到 by-id 存储。
func (s *SkillService) ImportSkillDirectory(sourceDir, name string) (SkillImportResult, error) {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return SkillImportResult{}, apperrors.Wrap(err, "SkillService.ImportSkillDirectory", "stat source dir")
	}
	if !info.IsDir() {
		return SkillImportResult{}, apperrors.Newf("SkillService.ImportSkillDirectory", "path is not a directory: %s", sourceDir)
	}
	if _, err := ensureSourceSkillFile(sourceDir); err != nil {
		return SkillImportResult{}, apperrors.Wrap(err, "SkillService.ImportSkillDirectory", "missing SKILL.md in source directory")
	}

	storedName := strings.TrimSpace(name)
	if storedName == "" {
		candidate := strings.TrimSpace(strings.TrimRight(sourceDir, `/\\`))
		if candidate == "" {
			return SkillImportResult{}, apperrors.New("SkillService.ImportSkillDirectory", "source directory is required")
		}
		storedName = strings.TrimSpace(filepath.Base(candidate))
	}
	if storedName == "" {
		return SkillImportResult{}, apperrors.New("SkillService.ImportSkillDirectory", "skill name is required")
	}

	if err := s.ensureByIDRoot(); err != nil {
		return SkillImportResult{}, apperrors.Wrap(err, "SkillService.ImportSkillDirectory", "mkdir by-id root")
	}
	id := skillStorageID(storedName)
	targetDir := filepath.Join(s.byIDRoot(), id)
	stagingDir := filepath.Join(s.byIDRoot(), fmt.Sprintf(".%s.import-%d", id, time.Now().UnixNano()))
	if err := os.RemoveAll(stagingDir); err != nil {
		return SkillImportResult{}, apperrors.Wrap(err, "SkillService.ImportSkillDirectory", "clean staging dir")
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()

	stats, err := copySkillDirectory(sourceDir, stagingDir)
	if err != nil {
		return SkillImportResult{}, apperrors.Wrap(err, "SkillService.ImportSkillDirectory", "copy skill directory")
	}
	if _, err := ensureSourceSkillFile(stagingDir); err != nil {
		return SkillImportResult{}, apperrors.Wrap(err, "SkillService.ImportSkillDirectory", "copied package missing SKILL.md")
	}
	if err := s.writeSkillIndex(stagingDir, storedName); err != nil {
		return SkillImportResult{}, apperrors.Wrap(err, "SkillService.ImportSkillDirectory", "write skill index")
	}
	if err := activateStagedSkillDir(targetDir, stagingDir); err != nil {
		return SkillImportResult{}, apperrors.Wrap(err, "SkillService.ImportSkillDirectory", "activate imported skill dir")
	}

	return SkillImportResult{
		Name:      storedName,
		Dir:       targetDir,
		SkillFile: filepath.Join(targetDir, skillMainFile),
		Files:     stats.Files,
		Bytes:     stats.Bytes,
	}, nil
}

func ensureSourceSkillFile(sourceDir string) (string, error) {
	path := filepath.Join(sourceDir, skillMainFile)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", apperrors.Newf("ensureSourceSkillFile", "SKILL.md is a directory: %s", path)
	}
	return path, nil
}

func copyRegularFile(srcPath, dstPath string, mode fs.FileMode) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()
	if mode == 0 {
		mode = 0o644
	}
	dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func copySkillDirectory(sourceDir, targetDir string) (skillImportStats, error) {
	stats := skillImportStats{}
	err := filepath.WalkDir(sourceDir, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceDir, currentPath)
		if err != nil {
			return err
		}
		relative = filepath.Clean(relative)
		if relative == "." {
			return os.MkdirAll(targetDir, 0o755)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return apperrors.Newf("copySkillDirectory", "path escapes source dir: %s", currentPath)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return apperrors.Newf("copySkillDirectory", "symlink is not allowed: %s", relative)
		}
		if entry.IsDir() && strings.EqualFold(entry.Name(), ".git") {
			return filepath.SkipDir
		}
		destinationPath := filepath.Join(targetDir, relative)
		if entry.IsDir() {
			return os.MkdirAll(destinationPath, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > maxSkillImportSingleFileSize {
			return apperrors.Newf(
				"copySkillDirectory",
				"file too large: %s (%d bytes, limit %d bytes)",
				relative,
				info.Size(),
				maxSkillImportSingleFileSize,
			)
		}
		stats.Files++
		if stats.Files > maxSkillImportFiles {
			return apperrors.Newf("copySkillDirectory", "too many files: limit %d", maxSkillImportFiles)
		}
		stats.Bytes += info.Size()
		if stats.Bytes > maxSkillImportTotalFileSize {
			return apperrors.Newf(
				"copySkillDirectory",
				"skill package too large: %d bytes (limit %d bytes)",
				stats.Bytes,
				maxSkillImportTotalFileSize,
			)
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return err
		}
		return copyRegularFile(currentPath, destinationPath, info.Mode().Perm())
	})
	return stats, err
}

func activateStagedSkillDir(targetDir, stagedDir string) error {
	parentDir := filepath.Dir(targetDir)
	base := filepath.Base(targetDir)
	backupDir := filepath.Join(parentDir, fmt.Sprintf(".%s.backup-%d", base, time.Now().UnixNano()))
	backupCreated := false
	if _, err := os.Stat(targetDir); err == nil {
		if err := os.Rename(targetDir, backupDir); err != nil {
			return err
		}
		backupCreated = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stagedDir, targetDir); err != nil {
		if backupCreated {
			_ = os.Rename(backupDir, targetDir)
		}
		return err
	}
	if backupCreated {
		_ = os.RemoveAll(backupDir)
	}
	return nil
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
			File:  skillMainFile,
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

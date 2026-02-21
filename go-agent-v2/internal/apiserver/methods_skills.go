// methods_skills.go — skills/* JSON-RPC 方法实现 (列表/导入/读写/配置/预览)。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/service"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ========================================
// skills/list, app/list
// ========================================

func (s *Server) skillsList(_ context.Context, _ json.RawMessage) (any, error) {
	if s.skillSvc != nil {
		list, err := s.skillSvc.ListSkills()
		if err == nil {
			skills := make([]map[string]any, 0, len(list))
			for _, item := range list {
				skills = append(skills, map[string]any{
					"name":          item.Name,
					"dir":           item.Dir,
					"description":   item.Description,
					"summary":       item.Summary,
					"trigger_words": item.TriggerWords,
					"force_words":   item.ForceWords,
				})
			}
			return map[string]any{"skills": skills}, nil
		}
		logger.Warn("skills/list: load metadata failed, fallback to dir scan", logger.FieldError, err)
	}

	var skills []map[string]any
	entries, err := os.ReadDir(s.skillsDirectory())
	if err != nil {
		return map[string]any{"skills": skills}, nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			skills = append(skills, map[string]any{"name": entry.Name()})
		}
	}
	return map[string]any{"skills": skills}, nil
}

func (s *Server) appList(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"apps": []any{}}, nil
}

// ========================================
// skills/local/read, skills/local/importDir
// ========================================

type skillsLocalReadParams struct {
	Path string `json:"path"`
}

const (
	maxSkillImportFiles          = 1000
	maxSkillImportSingleFileSize = 4 << 20  // 4MB
	maxSkillImportTotalFileSize  = 20 << 20 // 20MB
)

type skillsLocalImportDirParams struct {
	Path  string   `json:"path"`
	Paths []string `json:"paths,omitempty"`
	Name  string   `json:"name,omitempty"`
}

type skillImportStats struct {
	Files int
	Bytes int64
}

type skillImportFailure struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

type skillImportResult struct {
	Name      string `json:"name"`
	Dir       string `json:"dir"`
	SkillFile string `json:"skill_file"`
	Source    string `json:"source"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
}

func skillImportDirName(rawName, sourceDir string) (string, error) {
	name := strings.TrimSpace(rawName)
	if name != "" {
		return normalizeSkillName(name)
	}
	candidate := strings.TrimSpace(strings.TrimRight(sourceDir, `/\`))
	if candidate == "" {
		return "", apperrors.New("skillImportDirName", "source directory is required")
	}
	base := filepath.Base(candidate)
	return normalizeSkillName(base)
}

func ensureSourceSkillFile(sourceDir string) (string, error) {
	path := filepath.Join(sourceDir, "SKILL.md")
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

func collectSkillImportSources(path string, paths []string) []string {
	candidates := make([]string, 0, len(paths)+1)
	if strings.TrimSpace(path) != "" {
		candidates = append(candidates, path)
	}
	candidates = append(candidates, paths...)

	out := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, raw := range candidates {
		source := strings.TrimSpace(raw)
		if source == "" {
			continue
		}
		abs, err := filepath.Abs(source)
		if err == nil {
			source = abs
		}
		key := strings.ToLower(filepath.Clean(source))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, source)
	}
	return out
}

func (s *Server) importSingleSkillDirectory(sourceDir, name string) (skillImportResult, error) {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "stat source dir")
	}
	if !info.IsDir() {
		return skillImportResult{}, apperrors.Newf("Server.importSingleSkillDirectory", "path is not a directory: %s", sourceDir)
	}
	if _, err := ensureSourceSkillFile(sourceDir); err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "missing SKILL.md in source directory")
	}

	skillName, err := skillImportDirName(name, sourceDir)
	if err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "resolve skill name")
	}
	skillsRoot := s.skillsDirectory()
	targetRoot := filepath.Join(skillsRoot, skillName)
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "mkdir skills root")
	}

	tmpRoot := filepath.Join(skillsRoot, fmt.Sprintf(".%s.import-%d", skillName, time.Now().UnixNano()))
	if err := os.RemoveAll(tmpRoot); err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "clean temp skill dir")
	}
	defer func() {
		_ = os.RemoveAll(tmpRoot)
	}()

	stats, err := copySkillDirectory(sourceDir, tmpRoot)
	if err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "copy skill directory")
	}
	skillFilePath := filepath.Join(tmpRoot, "SKILL.md")
	if _, err := os.Stat(skillFilePath); err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "copied package missing SKILL.md")
	}

	backupRoot := filepath.Join(skillsRoot, fmt.Sprintf(".%s.backup-%d", skillName, time.Now().UnixNano()))
	backupCreated := false
	if _, err := os.Stat(targetRoot); err == nil {
		if err := os.Rename(targetRoot, backupRoot); err != nil {
			return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "backup existing skill dir")
		}
		backupCreated = true
	} else if !os.IsNotExist(err) {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "stat existing skill dir")
	}
	if err := os.Rename(tmpRoot, targetRoot); err != nil {
		if backupCreated {
			_ = os.Rename(backupRoot, targetRoot)
		}
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "activate imported skill dir")
	}
	if backupCreated {
		_ = os.RemoveAll(backupRoot)
	}
	skillFilePath = filepath.Join(targetRoot, "SKILL.md")

	logger.Info("skills/local/importDir: imported",
		logger.FieldSkill, skillName,
		logger.FieldPath, sourceDir,
		"files", stats.Files,
		"bytes", stats.Bytes,
	)
	return skillImportResult{
		Name:      skillName,
		Dir:       targetRoot,
		SkillFile: skillFilePath,
		Source:    sourceDir,
		Files:     stats.Files,
		Bytes:     stats.Bytes,
	}, nil
}

func (s *Server) skillsLocalReadTyped(_ context.Context, p skillsLocalReadParams) (any, error) {
	path := strings.TrimSpace(p.Path)
	if path == "" {
		return nil, apperrors.New("Server.skillsLocalRead", "path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsLocalRead", "stat file")
	}
	if info.IsDir() {
		return nil, apperrors.Newf("Server.skillsLocalRead", "path is directory: %s", path)
	}
	const maxSkillLocalReadBytes = 1 << 20 // 1MB
	if info.Size() > maxSkillLocalReadBytes {
		return nil, apperrors.Newf("Server.skillsLocalRead", "file too large: %d bytes", info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsLocalRead", "read file")
	}
	summary, summarySource := service.SummarizeSkillContent(string(data))
	return map[string]any{
		"skill": map[string]string{
			"path":           path,
			"content":        string(data),
			"summary":        summary,
			"summary_source": summarySource,
		},
	}, nil
}

func (s *Server) skillsLocalImportDirTyped(_ context.Context, p skillsLocalImportDirParams) (any, error) {
	sources := collectSkillImportSources(p.Path, p.Paths)
	if len(sources) == 0 {
		return nil, apperrors.New("Server.skillsLocalImportDir", "path or paths is required")
	}

	if len(sources) == 1 {
		result, err := s.importSingleSkillDirectory(sources[0], p.Name)
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.skillsLocalImportDir", "import directory")
		}
		skillPayload := map[string]any{
			"name":       result.Name,
			"dir":        result.Dir,
			"skill_file": result.SkillFile,
			"source":     result.Source,
			"files":      result.Files,
			"bytes":      result.Bytes,
		}
		return map[string]any{
			"ok": true,
			"summary": map[string]int{
				"requested": 1,
				"imported":  1,
				"failed":    0,
			},
			"skill":    skillPayload,
			"skills":   []map[string]any{skillPayload},
			"failures": []map[string]string{},
		}, nil
	}

	if strings.TrimSpace(p.Name) != "" {
		return nil, apperrors.New("Server.skillsLocalImportDir", "name is only supported for single directory import")
	}

	results := make([]skillImportResult, 0, len(sources))
	failures := make([]skillImportFailure, 0)
	seenNames := make(map[string]string, len(sources))

	for _, source := range sources {
		skillName, nameErr := skillImportDirName("", source)
		if nameErr != nil {
			failures = append(failures, skillImportFailure{
				Source: source,
				Error:  nameErr.Error(),
			})
			continue
		}
		nameKey := strings.ToLower(skillName)
		if previousSource, exists := seenNames[nameKey]; exists {
			failures = append(failures, skillImportFailure{
				Source: source,
				Error:  fmt.Sprintf("duplicate skill name %q with source %s", skillName, previousSource),
			})
			continue
		}
		seenNames[nameKey] = source

		result, err := s.importSingleSkillDirectory(source, "")
		if err != nil {
			failures = append(failures, skillImportFailure{
				Source: source,
				Error:  err.Error(),
			})
			continue
		}
		results = append(results, result)
	}

	skillsPayload := make([]map[string]any, 0, len(results))
	for _, result := range results {
		skillsPayload = append(skillsPayload, map[string]any{
			"name":       result.Name,
			"dir":        result.Dir,
			"skill_file": result.SkillFile,
			"source":     result.Source,
			"files":      result.Files,
			"bytes":      result.Bytes,
		})
	}
	failuresPayload := make([]map[string]string, 0, len(failures))
	for _, failure := range failures {
		failuresPayload = append(failuresPayload, map[string]string{
			"source": failure.Source,
			"error":  failure.Error,
		})
	}

	return map[string]any{
		"ok": len(failures) == 0,
		"summary": map[string]int{
			"requested": len(sources),
			"imported":  len(results),
			"failed":    len(failures),
		},
		"skills":   skillsPayload,
		"failures": failuresPayload,
	}, nil
}

// ========================================
// skills/config, skills/match/preview
// ========================================

// skillsConfigWriteParams skills/config/write 请求参数。
//
// 两种模式:
//  1. 写入 SKILL.md 文件: {"name": "skill_name", "content": "..."}
//  2. 为会话配置技能列表: {"agent_id": "thread-xxx", "skills": ["s1", "s2"]}
type skillsConfigWriteParams struct {
	// 模式 1: 写文件
	Name    string `json:"name"`
	Content string `json:"content"`
	// 模式 2: per-session 技能配置
	AgentID string   `json:"agent_id"`
	Skills  []string `json:"skills"`
}

// skillsSummaryWriteParams skills/summary/write 请求参数。
type skillsSummaryWriteParams struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type skillsMatchPreviewParams struct {
	ThreadID string      `json:"threadId"`
	AgentID  string      `json:"agent_id,omitempty"`
	Text     string      `json:"text"`
	Input    []UserInput `json:"input,omitempty"`
}

type skillsMatchPreviewItem struct {
	Name         string   `json:"name"`
	MatchedBy    string   `json:"matched_by"`
	MatchedTerms []string `json:"matched_terms,omitempty"`
}

func resolveSkillMatchPreviewThreadID(p skillsMatchPreviewParams) string {
	threadID := strings.TrimSpace(p.ThreadID)
	if threadID != "" {
		return threadID
	}
	return strings.TrimSpace(p.AgentID)
}

type skillsConfigReadParams struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) skillsMatchPreviewTyped(_ context.Context, p skillsMatchPreviewParams) (any, error) {
	threadID := resolveSkillMatchPreviewThreadID(p)
	matches := s.collectAutoMatchedSkillMatches(threadID, p.Text, p.Input, autoSkillMatchOptions{
		IncludeConfiguredExplicit: true,
	})
	items := make([]skillsMatchPreviewItem, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match.Name)
		if name == "" {
			continue
		}
		item := skillsMatchPreviewItem{
			Name:      name,
			MatchedBy: match.MatchedBy,
		}
		if len(match.MatchedTerms) > 0 {
			item.MatchedTerms = append([]string(nil), match.MatchedTerms...)
		}
		items = append(items, item)
	}
	return map[string]any{
		"thread_id": threadID,
		"matches":   items,
	}, nil
}

func (s *Server) skillsConfigReadTyped(_ context.Context, p skillsConfigReadParams) (any, error) {
	agentID := strings.TrimSpace(p.AgentID)
	if agentID == "" {
		return nil, apperrors.New("Server.skillsConfigRead", "agent_id is required")
	}
	return map[string]any{
		"agent_id": agentID,
		"skills":   s.GetAgentSkills(agentID),
	}, nil
}

func (s *Server) skillsConfigWriteTyped(_ context.Context, p skillsConfigWriteParams) (any, error) {
	// 模式 2: 为指定 agent/session 配置技能列表
	if p.AgentID != "" {
		normalizedSkills, err := normalizeSkillNames(p.Skills)
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "normalize skills")
		}
		s.skillsMu.Lock()
		if len(normalizedSkills) == 0 {
			delete(s.agentSkills, p.AgentID)
		} else {
			s.agentSkills[p.AgentID] = normalizedSkills
		}
		s.skillsMu.Unlock()
		logger.Info("skills/config/write: agent skills configured",
			logger.FieldAgentID, p.AgentID, "skills", normalizedSkills)
		return map[string]any{"ok": true, "agent_id": p.AgentID, "skills": normalizedSkills}, nil
	}

	// 模式 1: 写 SKILL.md 文件
	if strings.TrimSpace(p.Name) == "" {
		return nil, apperrors.New("Server.skillsConfigWrite", "name or agent_id is required")
	}
	skillName, err := normalizeSkillName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "normalize skill name")
	}
	dir := filepath.Join(s.skillsDirectory(), skillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "mkdir")
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "write SKILL.md")
	}
	logger.Info("skills/config/write: saved", logger.FieldSkill, skillName, logger.FieldBytes, len(p.Content))
	return map[string]any{"ok": true, "path": path}, nil
}

func (s *Server) skillsSummaryWriteTyped(_ context.Context, p skillsSummaryWriteParams) (any, error) {
	if strings.TrimSpace(p.Name) == "" {
		return nil, apperrors.New("Server.skillsSummaryWrite", "name is required")
	}
	skillName, err := normalizeSkillName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsSummaryWrite", "normalize skill name")
	}
	path := filepath.Join(s.skillsDirectory(), skillName, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsSummaryWrite", "read SKILL.md")
	}
	summary := strings.TrimSpace(p.Summary)
	updated := service.UpsertSkillSummaryFrontmatter(string(data), summary)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsSummaryWrite", "write SKILL.md")
	}
	logger.Info("skills/summary/write: saved", logger.FieldSkill, skillName, "summary_len", len(summary))
	return map[string]any{
		"ok":      true,
		"path":    path,
		"name":    skillName,
		"summary": summary,
	}, nil
}

// GetAgentSkills 返回指定 agent 配置的技能列表。
func (s *Server) GetAgentSkills(agentID string) []string {
	s.skillsMu.RLock()
	defer s.skillsMu.RUnlock()
	values := s.agentSkills[agentID]
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

// ========================================
// skills/remote/read, skills/remote/write
// ========================================

// skillsRemoteReadParams skills/remote/read 请求参数。
type skillsRemoteReadParams struct {
	URL string `json:"url"`
}

// skillsRemoteReadTyped 读取远程 Skill。
func (s *Server) skillsRemoteReadTyped(_ context.Context, p skillsRemoteReadParams) (any, error) {
	logger.Info("skills/remote/read: fetching", logger.FieldURL, p.URL)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(p.URL)
	if err != nil {
		logger.Warn("skills/remote/read: fetch failed", logger.FieldURL, p.URL, logger.FieldError, err)
		return nil, apperrors.Wrap(err, "Server.skillsRemoteRead", "fetch remote skill")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, apperrors.Newf(
			"Server.skillsRemoteRead",
			"fetch remote skill failed status=%d body=%s",
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsRemoteRead", "read response body")
	}
	return map[string]any{
		"skill": map[string]string{"url": p.URL, "content": string(body)},
	}, nil
}

// skillsRemoteWriteParams skills/remote/write 请求参数。
type skillsRemoteWriteParams struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// skillsRemoteWriteTyped 写入远程 Skill 到本地。
func (s *Server) skillsRemoteWriteTyped(_ context.Context, p skillsRemoteWriteParams) (any, error) {
	skillName, err := normalizeSkillName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsRemoteWrite", "normalize skill name")
	}
	skillsDir := filepath.Join(s.skillsDirectory(), skillName)
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(p.Content), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

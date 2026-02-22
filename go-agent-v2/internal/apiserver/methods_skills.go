// methods_skills.go — skills/* JSON-RPC 方法实现 (列表/导入/读写/配置/预览)。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	if s.skillSvc == nil {
		return map[string]any{"skills": []map[string]any{}}, nil
	}
	list, err := s.skillSvc.ListSkills()
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsList", "list skills")
	}
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

func (s *Server) appList(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"apps": []any{}}, nil
}

// ========================================
// skills/local/read, skills/local/importDir, skills/local/delete
// ========================================

type skillsLocalReadParams struct {
	Path string `json:"path"`
}

type skillsLocalImportDirParams struct {
	Path  string   `json:"path"`
	Paths []string `json:"paths,omitempty"`
	Name  string   `json:"name,omitempty"`
}

type skillsLocalDeleteParams struct {
	Name string `json:"name"`
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

func sourceDirHasSkillFile(source string) (bool, error) {
	info, err := os.Stat(filepath.Join(source, "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

func expandSkillImportSource(source string) ([]string, error) {
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return []string{source}, nil
	}

	hasSkillFile, err := sourceDirHasSkillFile(source)
	if err != nil {
		return nil, err
	}
	if hasSkillFile {
		return []string{source}, nil
	}

	entries, err := os.ReadDir(source)
	if err != nil {
		return nil, err
	}
	children := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child := filepath.Join(source, entry.Name())
		childHasSkillFile, statErr := sourceDirHasSkillFile(child)
		if statErr != nil {
			return nil, statErr
		}
		if childHasSkillFile {
			children = append(children, child)
		}
	}
	if len(children) == 0 {
		return []string{source}, nil
	}
	sort.Strings(children)
	return children, nil
}

func (s *Server) importSingleSkillDirectory(sourceDir, name string) (skillImportResult, error) {
	if s.skillSvc == nil {
		return skillImportResult{}, apperrors.New("Server.importSingleSkillDirectory", "skill service unavailable")
	}
	skillName, err := skillImportDirName(name, sourceDir)
	if err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "resolve skill name")
	}
	result, err := s.skillSvc.ImportSkillDirectory(sourceDir, skillName)
	if err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "import directory")
	}

	logger.Info("skills/local/importDir: imported",
		logger.FieldSkill, skillName,
		logger.FieldPath, sourceDir,
		"files", result.Files,
		"bytes", result.Bytes,
	)
	return skillImportResult{
		Name:      skillName,
		Dir:       result.Dir,
		SkillFile: result.SkillFile,
		Source:    sourceDir,
		Files:     result.Files,
		Bytes:     result.Bytes,
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
	requestedSources := collectSkillImportSources(p.Path, p.Paths)
	if len(requestedSources) == 0 {
		return nil, apperrors.New("Server.skillsLocalImportDir", "path or paths is required")
	}
	expandedSources := make([]string, 0, len(requestedSources))
	for _, source := range requestedSources {
		resolved, err := expandSkillImportSource(source)
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.skillsLocalImportDir", "expand import source")
		}
		expandedSources = append(expandedSources, resolved...)
	}
	sources := collectSkillImportSources("", expandedSources)

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

func (s *Server) skillsLocalDeleteTyped(_ context.Context, p skillsLocalDeleteParams) (any, error) {
	if s.skillSvc == nil {
		return nil, apperrors.New("Server.skillsLocalDelete", "skill service unavailable")
	}
	skillName, err := normalizeSkillName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsLocalDelete", "normalize skill name")
	}
	resolvedName, targetDir, err := s.skillSvc.DeleteSkill(skillName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apperrors.Newf("Server.skillsLocalDelete", "skill not found: %s", skillName)
		}
		return nil, apperrors.Wrap(err, "Server.skillsLocalDelete", "delete skill")
	}

	removedBindings := 0

	logger.Info("skills/local/delete: removed",
		logger.FieldSkill, resolvedName,
		logger.FieldPath, targetDir,
		"removed_agent_bindings", removedBindings,
	)
	return map[string]any{
		"ok":                     true,
		"name":                   resolvedName,
		"dir":                    targetDir,
		"removed_agent_bindings": removedBindings,
	}, nil
}

// ========================================
// skills/config, skills/match/preview
// ========================================

// skillsConfigWriteParams skills/config/write 请求参数。
type skillsConfigWriteParams struct {
	Name    string `json:"name"`
	Content string `json:"content"`
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
		IncludeConfiguredForce:    true,
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
		"agent_id":      agentID,
		"skills":        []string{},
		"session_bound": false,
	}, nil
}

func (s *Server) skillsConfigWriteTyped(_ context.Context, p skillsConfigWriteParams) (any, error) {
	if s.skillSvc == nil {
		return nil, apperrors.New("Server.skillsConfigWrite", "skill service unavailable")
	}

	if strings.TrimSpace(p.Name) == "" {
		return nil, apperrors.New("Server.skillsConfigWrite", "name is required")
	}
	skillName, err := normalizeSkillName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "normalize skill name")
	}
	path, err := s.skillSvc.WriteSkillContent(skillName, p.Content)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "write skill content")
	}
	logger.Info("skills/config/write: saved", logger.FieldSkill, skillName, logger.FieldBytes, len(p.Content))
	return map[string]any{"ok": true, "path": path}, nil
}

func (s *Server) skillsSummaryWriteTyped(_ context.Context, p skillsSummaryWriteParams) (any, error) {
	if s.skillSvc == nil {
		return nil, apperrors.New("Server.skillsSummaryWrite", "skill service unavailable")
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, apperrors.New("Server.skillsSummaryWrite", "name is required")
	}
	skillName, err := normalizeSkillName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsSummaryWrite", "normalize skill name")
	}
	summary := strings.TrimSpace(p.Summary)
	path, resolvedName, err := s.skillSvc.UpdateSkillSummary(skillName, summary)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsSummaryWrite", "update skill summary")
	}
	logger.Info("skills/summary/write: saved", logger.FieldSkill, resolvedName, "summary_len", len(summary))
	return map[string]any{
		"ok":      true,
		"path":    path,
		"name":    resolvedName,
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
	if s.skillSvc == nil {
		return nil, apperrors.New("Server.skillsRemoteWrite", "skill service unavailable")
	}
	skillName, err := normalizeSkillName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsRemoteWrite", "normalize skill name")
	}
	path, err := s.skillSvc.WriteSkillContent(skillName, p.Content)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsRemoteWrite", "write skill content")
	}
	return map[string]any{"ok": true, "path": path}, nil
}

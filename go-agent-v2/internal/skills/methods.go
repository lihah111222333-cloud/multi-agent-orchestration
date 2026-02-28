package skills

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/skillutil"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type SkillsLocalReadParams struct {
	Path string `json:"path"`
}

type SkillsLocalImportDirParams struct {
	Path  string   `json:"path"`
	Paths []string `json:"paths,omitempty"`
	Name  string   `json:"name,omitempty"`
}

type SkillsLocalDeleteParams struct {
	Name string `json:"name"`
}

// SkillsConfigWriteParams skills/config/write 请求参数。
type SkillsConfigWriteParams struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// SkillsSummaryWriteParams skills/summary/write 请求参数。
type SkillsSummaryWriteParams struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type UserInput struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	URL     string `json:"url,omitempty"`
	Path    string `json:"path,omitempty"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

type SkillsMatchPreviewParams struct {
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

type SkillsConfigReadParams struct {
	AgentID string `json:"agent_id"`
}

// SkillsRemoteReadParams skills/remote/read 请求参数。
type SkillsRemoteReadParams struct {
	URL string `json:"url"`
}

// SkillsRemoteWriteParams skills/remote/write 请求参数。
type SkillsRemoteWriteParams struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// SkillsList handles skills/list.
func (m *Manager) SkillsList(_ context.Context) (any, error) {
	skillSvc := m.skillService()
	if skillSvc == nil {
		return map[string]any{"skills": []map[string]any{}}, nil
	}
	list, err := skillSvc.ListSkills()
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

// AppList handles app/list.
func (m *Manager) AppList(_ context.Context) (any, error) {
	return map[string]any{"apps": []any{}}, nil
}

func (m *Manager) importSingleSkillDirectory(sourceDir, name string) (skillImportResult, error) {
	skillSvc := m.skillService()
	if skillSvc == nil {
		return skillImportResult{}, apperrors.New("Server.importSingleSkillDirectory", "skill service unavailable")
	}
	skillName, err := skillImportDirName(name, sourceDir)
	if err != nil {
		return skillImportResult{}, apperrors.Wrap(err, "Server.importSingleSkillDirectory", "resolve skill name")
	}
	result, err := skillSvc.ImportSkillDirectory(sourceDir, skillName)
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

// SkillsLocalRead handles skills/local/read.
func (m *Manager) SkillsLocalRead(_ context.Context, p SkillsLocalReadParams) (any, error) {
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

// SkillsLocalImportDir handles skills/local/importDir.
func (m *Manager) SkillsLocalImportDir(_ context.Context, p SkillsLocalImportDirParams) (any, error) {
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
		result, err := m.importSingleSkillDirectory(sources[0], p.Name)
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.skillsLocalImportDir", "import directory")
		}
		response := skillImportResponse(1, []skillImportResult{result}, nil)
		response["skill"] = skillImportResultPayload(result)
		return response, nil
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

		result, err := m.importSingleSkillDirectory(source, "")
		if err != nil {
			failures = append(failures, skillImportFailure{
				Source: source,
				Error:  err.Error(),
			})
			continue
		}
		results = append(results, result)
	}

	return skillImportResponse(len(sources), results, failures), nil
}

// SkillsLocalDelete handles skills/local/delete.
func (m *Manager) SkillsLocalDelete(_ context.Context, p SkillsLocalDeleteParams) (any, error) {
	skillSvc := m.skillService()
	if skillSvc == nil {
		return nil, apperrors.New("Server.skillsLocalDelete", "skill service unavailable")
	}
	skillName, err := skillutil.NormalizeName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsLocalDelete", "normalize skill name")
	}
	resolvedName, targetDir, err := skillSvc.DeleteSkill(skillName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apperrors.Newf("Server.skillsLocalDelete", "skill not found: %s", skillName)
		}
		return nil, apperrors.Wrap(err, "Server.skillsLocalDelete", "delete skill")
	}

	logger.Info("skills/local/delete: removed",
		logger.FieldSkill, resolvedName,
		logger.FieldPath, targetDir,
		"removed_agent_bindings", 0,
	)
	return map[string]any{
		"ok":                     true,
		"name":                   resolvedName,
		"dir":                    targetDir,
		"removed_agent_bindings": 0,
	}, nil
}

// SkillsMatchPreview handles skills/match/preview.
func (m *Manager) SkillsMatchPreview(_ context.Context, p SkillsMatchPreviewParams) (any, error) {
	threadID := resolveSkillMatchPreviewThreadID(p)
	var matches []AutoMatchedSkillMatch
	if matcher := m.autoMatcher(); matcher != nil {
		matches = matcher.CollectAutoMatchedSkillMatches(threadID, p.Text, p.Input, AutoSkillMatchOptions{
			IncludeConfiguredExplicit: true,
			IncludeConfiguredForce:    true,
		})
	}
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

// SkillsConfigRead handles skills/config/read.
func (m *Manager) SkillsConfigRead(_ context.Context, p SkillsConfigReadParams) (any, error) {
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

// SkillsConfigWrite handles skills/config/write.
func (m *Manager) SkillsConfigWrite(_ context.Context, p SkillsConfigWriteParams) (any, error) {
	skillSvc := m.skillService()
	if skillSvc == nil {
		return nil, apperrors.New("Server.skillsConfigWrite", "skill service unavailable")
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, apperrors.New("Server.skillsConfigWrite", "name is required")
	}
	skillName, err := skillutil.NormalizeName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "normalize skill name")
	}
	path, err := skillSvc.WriteSkillContent(skillName, p.Content)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsConfigWrite", "write skill content")
	}
	logger.Info("skills/config/write: saved", logger.FieldSkill, skillName, logger.FieldBytes, len(p.Content))
	return map[string]any{"ok": true, "path": path}, nil
}

// SkillsSummaryWrite handles skills/summary/write.
func (m *Manager) SkillsSummaryWrite(_ context.Context, p SkillsSummaryWriteParams) (any, error) {
	skillSvc := m.skillService()
	if skillSvc == nil {
		return nil, apperrors.New("Server.skillsSummaryWrite", "skill service unavailable")
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, apperrors.New("Server.skillsSummaryWrite", "name is required")
	}
	skillName, err := skillutil.NormalizeName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsSummaryWrite", "normalize skill name")
	}
	summary := strings.TrimSpace(p.Summary)
	path, resolvedName, err := skillSvc.UpdateSkillSummary(skillName, summary)
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

// SkillsRemoteRead handles skills/remote/read.
func (m *Manager) SkillsRemoteRead(_ context.Context, p SkillsRemoteReadParams) (any, error) {
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

// SkillsRemoteWrite handles skills/remote/write.
func (m *Manager) SkillsRemoteWrite(_ context.Context, p SkillsRemoteWriteParams) (any, error) {
	skillSvc := m.skillService()
	if skillSvc == nil {
		return nil, apperrors.New("Server.skillsRemoteWrite", "skill service unavailable")
	}
	skillName, err := skillutil.NormalizeName(p.Name)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsRemoteWrite", "normalize skill name")
	}
	path, err := skillSvc.WriteSkillContent(skillName, p.Content)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.skillsRemoteWrite", "write skill content")
	}
	return map[string]any{"ok": true, "path": path}, nil
}

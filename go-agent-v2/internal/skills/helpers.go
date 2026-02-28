package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/skillutil"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

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

func skillImportResultPayload(result skillImportResult) map[string]any {
	return map[string]any{
		"name":       result.Name,
		"dir":        result.Dir,
		"skill_file": result.SkillFile,
		"source":     result.Source,
		"files":      result.Files,
		"bytes":      result.Bytes,
	}
}

func skillImportResponse(requested int, results []skillImportResult, failures []skillImportFailure) map[string]any {
	skillsPayload := make([]map[string]any, len(results))
	for i, result := range results {
		skillsPayload[i] = skillImportResultPayload(result)
	}
	failuresPayload := make([]map[string]string, len(failures))
	for i, failure := range failures {
		failuresPayload[i] = map[string]string{
			"source": failure.Source,
			"error":  failure.Error,
		}
	}
	return map[string]any{
		"ok": len(failures) == 0,
		"summary": map[string]int{
			"requested": requested,
			"imported":  len(results),
			"failed":    len(failures),
		},
		"skills":   skillsPayload,
		"failures": failuresPayload,
	}
}

func skillImportDirName(rawName, sourceDir string) (string, error) {
	name := strings.TrimSpace(rawName)
	if name != "" {
		return skillutil.NormalizeName(name)
	}
	candidate := strings.TrimSpace(strings.TrimRight(sourceDir, `/\`))
	if candidate == "" {
		return "", apperrors.New("skillImportDirName", "source directory is required")
	}
	return skillutil.NormalizeName(filepath.Base(candidate))
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
	if err == nil { return !info.IsDir(), nil }
	if os.IsNotExist(err) { return false, nil }
	return false, err
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
		childHasSkillFile, err := sourceDirHasSkillFile(child)
		if err != nil {
			return nil, err
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

func resolveSkillMatchPreviewThreadID(p SkillsMatchPreviewParams) string {
	if threadID := strings.TrimSpace(p.ThreadID); threadID != "" {
		return threadID
	}
	return strings.TrimSpace(p.AgentID)
}

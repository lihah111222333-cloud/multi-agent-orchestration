package apiserver

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/dashboard"
)

const (
	prefProjectsList   = "projects.list"
	prefProjectsActive = "projects.active"
)

type uiProjectsAddParams struct {
	Path string `json:"path"`
}

type uiProjectsRemoveParams = uiProjectsAddParams
type uiProjectsSetActiveParams = uiProjectsAddParams

func normalizeProjectPath(path string) string {
	value := strings.TrimSpace(path)
	if value == "" || value == "/" || isWindowsDriveRoot(value) {
		return value
	}
	return strings.TrimRight(value, "\\/")
}

func isWindowsDriveRoot(path string) bool {
	if len(path) < 2 || len(path) > 3 || path[1] != ':' {
		return false
	}
	ch := path[0]
	if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
		return false
	}
	return len(path) == 2 || path[2] == '/' || path[2] == '\\'
}

func appendUniqueNormalizedProject(projects []string, path string) []string {
	normalized := normalizeProjectPath(path)
	if normalized == "" || normalized == "." || slices.Contains(projects, normalized) {
		return projects
	}
	return append(projects, normalized)
}

func parseProjectsList(value any) []string {
	var projects []string

	switch list := value.(type) {
	case []string:
		for _, item := range list {
			projects = appendUniqueNormalizedProject(projects, item)
		}
	case []any:
		for _, item := range list {
			projects = appendUniqueNormalizedProject(projects, dashboard.AsString(item))
		}
	}

	return projects
}

func readProjectsState(s *Server, ctx context.Context) ([]string, string, error) {
	if s.prefManager == nil {
		return []string{}, ".", nil
	}

	prefs, err := s.prefManager.GetAll(ctx)
	if err != nil {
		return nil, "", err
	}
	projects := parseProjectsList(prefs[prefProjectsList])
	active := normalizeProjectPath(dashboard.AsString(prefs[prefProjectsActive]))
	if active == "" || (active != "." && !slices.Contains(projects, active)) {
		active = "."
	}
	return projects, active, nil
}

func writeProjectsState(s *Server, ctx context.Context, projects []string, active string) error {
	if s.prefManager == nil {
		return nil
	}

	normalizedProjects := parseProjectsList(projects)
	normalizedActive := normalizeProjectPath(active)
	if normalizedActive == "" || (normalizedActive != "." && !slices.Contains(normalizedProjects, normalizedActive)) {
		normalizedActive = "."
	}

	if err := s.prefManager.Set(ctx, prefProjectsList, normalizedProjects); err != nil {
		return err
	}
	if err := s.prefManager.Set(ctx, prefProjectsActive, normalizedActive); err != nil {
		return err
	}
	return nil
}

func uiProjectsGet(s *Server, ctx context.Context, _ json.RawMessage) (any, error) {
	projects, active, err := readProjectsState(s, ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": projects,
		"active":   active,
	}, nil
}

func uiProjectsAdd(s *Server, ctx context.Context, p uiProjectsAddParams) (any, error) {
	projects, _, err := readProjectsState(s, ctx)
	if err != nil {
		return nil, err
	}
	next := normalizeProjectPath(p.Path)
	if next == "" || next == "." {
		return map[string]any{
			"projects": projects,
			"active":   ".",
		}, nil
	}
	if !slices.Contains(projects, next) {
		projects = append(projects, next)
	}
	if err := writeProjectsState(s, ctx, projects, next); err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": projects,
		"active":   next,
	}, nil
}

func uiProjectsRemove(s *Server, ctx context.Context, p uiProjectsRemoveParams) (any, error) {
	projects, active, err := readProjectsState(s, ctx)
	if err != nil {
		return nil, err
	}
	target := normalizeProjectPath(p.Path)
	next := make([]string, 0, len(projects))
	for _, item := range projects {
		if item != target {
			next = append(next, item)
		}
	}
	if active == target {
		active = "."
	}
	if err := writeProjectsState(s, ctx, next, active); err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": next,
		"active":   active,
	}, nil
}

func uiProjectsSetActive(s *Server, ctx context.Context, p uiProjectsSetActiveParams) (any, error) {
	projects, _, err := readProjectsState(s, ctx)
	if err != nil {
		return nil, err
	}
	next := normalizeProjectPath(p.Path)
	if next == "" || (next != "." && !slices.Contains(projects, next)) {
		next = "."
	}
	if err := writeProjectsState(s, ctx, projects, next); err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": projects,
		"active":   next,
	}, nil
}

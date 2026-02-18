package apiserver

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	prefProjectsList   = "projects.list"
	prefProjectsActive = "projects.active"
)

type uiProjectsAddParams struct {
	Path string `json:"path"`
}

type uiProjectsRemoveParams struct {
	Path string `json:"path"`
}

type uiProjectsSetActiveParams struct {
	Path string `json:"path"`
}

func normalizeProjectPath(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	if value != "/" && !isWindowsDriveRoot(value) {
		value = strings.TrimRight(value, "\\/")
	}
	return value
}

func isWindowsDriveRoot(path string) bool {
	if len(path) == 2 {
		return isASCIILetter(path[0]) && path[1] == ':'
	}
	if len(path) == 3 {
		return isASCIILetter(path[0]) && path[1] == ':' && (path[2] == '/' || path[2] == '\\')
	}
	return false
}

func isASCIILetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func containsProject(projects []string, target string) bool {
	for _, item := range projects {
		if item == target {
			return true
		}
	}
	return false
}

func parseProjectsList(value any) []string {
	projects := []string{}
	appendProject := func(path string) {
		normalized := normalizeProjectPath(path)
		if normalized == "" || normalized == "." {
			return
		}
		if containsProject(projects, normalized) {
			return
		}
		projects = append(projects, normalized)
	}

	switch list := value.(type) {
	case []string:
		for _, item := range list {
			appendProject(item)
		}
	case []any:
		for _, item := range list {
			appendProject(asString(item))
		}
	}

	return projects
}

func (s *Server) readProjectsState(ctx context.Context) ([]string, string, error) {
	if s.prefManager == nil {
		return []string{}, ".", nil
	}

	prefs, err := s.prefManager.GetAll(ctx)
	if err != nil {
		return nil, "", err
	}
	projects := parseProjectsList(prefs[prefProjectsList])
	active := normalizeProjectPath(asString(prefs[prefProjectsActive]))
	if active == "" {
		active = "."
	}
	if active != "." && !containsProject(projects, active) {
		active = "."
	}
	return projects, active, nil
}

func (s *Server) writeProjectsState(ctx context.Context, projects []string, active string) error {
	if s.prefManager == nil {
		return nil
	}

	normalizedProjects := parseProjectsList(projects)
	normalizedActive := normalizeProjectPath(active)
	if normalizedActive == "" {
		normalizedActive = "."
	}
	if normalizedActive != "." && !containsProject(normalizedProjects, normalizedActive) {
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

func (s *Server) uiProjectsGet(ctx context.Context, _ json.RawMessage) (any, error) {
	projects, active, err := s.readProjectsState(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": projects,
		"active":   active,
	}, nil
}

func (s *Server) uiProjectsAdd(ctx context.Context, p uiProjectsAddParams) (any, error) {
	projects, _, err := s.readProjectsState(ctx)
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
	if !containsProject(projects, next) {
		projects = append(projects, next)
	}
	if err := s.writeProjectsState(ctx, projects, next); err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": projects,
		"active":   next,
	}, nil
}

func (s *Server) uiProjectsRemove(ctx context.Context, p uiProjectsRemoveParams) (any, error) {
	projects, active, err := s.readProjectsState(ctx)
	if err != nil {
		return nil, err
	}
	target := normalizeProjectPath(p.Path)
	next := make([]string, 0, len(projects))
	for _, item := range projects {
		if item == target {
			continue
		}
		next = append(next, item)
	}
	if active == target {
		active = "."
	}
	if err := s.writeProjectsState(ctx, next, active); err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": next,
		"active":   active,
	}, nil
}

func (s *Server) uiProjectsSetActive(ctx context.Context, p uiProjectsSetActiveParams) (any, error) {
	projects, _, err := s.readProjectsState(ctx)
	if err != nil {
		return nil, err
	}
	next := normalizeProjectPath(p.Path)
	if next == "" || (next != "." && !containsProject(projects, next)) {
		next = "."
	}
	if err := s.writeProjectsState(ctx, projects, next); err != nil {
		return nil, err
	}
	return map[string]any{
		"projects": projects,
		"active":   next,
	}, nil
}


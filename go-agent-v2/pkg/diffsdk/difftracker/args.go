package difftracker

import (
	"path/filepath"
	"strings"
)

func NormalizeDynamicToolName(tool string) string {
	tool = strings.ToLower(strings.TrimSpace(tool))
	tool = strings.TrimPrefix(strings.TrimPrefix(tool, "functions."), "tools.")
	return tool
}

func ExtractStringArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := args[key].(string)
		if !ok {
			continue
		}
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func ExtractBoolArg(args map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch typed := args[key].(type) {
		case bool:
			return typed
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "y":
				return true
			case "false", "0", "no", "n":
				return false
			}
		case int:
			if typed != 0 {
				return true
			}
		case int64:
			if typed != 0 {
				return true
			}
		case float64:
			if typed != 0 {
				return true
			}
		}
	}
	return false
}

func ShouldCaptureDynamicToolDiff(tool string, args map[string]any) bool {
	switch NormalizeDynamicToolName(tool) {
	case "lsp_file":
		action := strings.ToLower(ExtractStringArg(args, "action"))
		if action == "open_file" || action == "diagnostics" {
			return false
		}
		return ExtractBoolArg(args, "persist_to_disk")
	case "code_run", "run":
		return true
	default:
		return false
	}
}

func ResolveDynamicToolDiffRepoRoot(agentID string, args map[string]any, resolveWorkDir WorkDirResolver) string {
	baseDir := ""
	if resolveWorkDir != nil {
		baseDir = strings.TrimSpace(resolveWorkDir(agentID))
	}

	candidates := make([]string, 0, 16)
	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		candidates = append(candidates, path, filepath.Dir(path))
		if baseDir != "" && !filepath.IsAbs(path) {
			absPath := filepath.Join(baseDir, path)
			candidates = append(candidates, absPath, filepath.Dir(absPath))
		}
	}

	for _, key := range []string{"work_dir", "workdir", "cwd", "file_path", "path", "file"} {
		addCandidate(ExtractStringArg(args, key))
	}
	if baseDir != "" {
		candidates = append(candidates, baseDir)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		normalized := filepath.Clean(candidate)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		repoRoot, err := GitRepoRootFromPath(normalized)
		if err != nil {
			continue
		}
		return repoRoot
	}

	return ""
}

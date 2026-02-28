package difftracker

import (
	"errors"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func RunGit(repoRoot string, args ...string) (string, error) {
	if repoRoot = strings.TrimSpace(repoRoot); repoRoot != "" {
		args = append([]string{"-C", repoRoot}, args...)
	}
	output, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func GitRepoRootFromPath(path string) (string, error) {
	if path = strings.TrimSpace(path); path == "" {
		return "", errors.New("git path is empty")
	}
	output, err := RunGit(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	repoRoot := strings.TrimSpace(output)
	if repoRoot == "" {
		return "", errors.New("git repo root is empty")
	}
	return repoRoot, nil
}

func ListRepoDirtyPaths(repoRoot string) ([]string, error) {
	trackedOutput, err := RunGit(repoRoot, "diff", "--name-only", "HEAD", "--", ".")
	if err != nil {
		return nil, err
	}
	untrackedOutput, err := RunGit(repoRoot, "ls-files", "--others", "--exclude-standard", "--")
	if err != nil {
		return nil, err
	}
	return UniqueSortedPaths(append(CollectPathLines(trackedOutput), CollectPathLines(untrackedOutput)...)), nil
}

func CollectPathLines(output string) []string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
			if decoded, err := strconv.Unquote(path); err == nil {
				path = decoded
			}
		}
		if path = strings.TrimPrefix(strings.TrimPrefix(path, "a/"), "b/"); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func UniqueSortedPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		if clean == "" || clean == "." {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}

package codexadapter

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/codex"
)

func addThreadArtifactCandidate(candidates *[]ThreadArtifactCandidate, seen map[string]struct{}, kind, path string) {
	if candidates == nil {
		return
	}
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return
	}
	if _, ok := seen[cleaned]; ok {
		return
	}
	seen[cleaned] = struct{}{}
	*candidates = append(*candidates, ThreadArtifactCandidate{Kind: kind, Path: cleaned})
}

// CollectThreadArtifactCandidates enumerates candidate codex files for archive.
func CollectThreadArtifactCandidates(codexThreadID string, rolloutPath string) []ThreadArtifactCandidate {
	candidates := make([]ThreadArtifactCandidate, 0, 8)
	seen := make(map[string]struct{}, 8)

	resolvedRollout := strings.TrimSpace(rolloutPath)
	if resolvedRollout == "" && strings.TrimSpace(codexThreadID) != "" {
		if found, err := codex.FindRolloutPath(codexThreadID); err == nil {
			resolvedRollout = found
		}
	}
	if resolvedRollout != "" {
		addThreadArtifactCandidate(&candidates, seen, "rollout", resolvedRollout)
	}
	if strings.TrimSpace(codexThreadID) == "" {
		return candidates
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return candidates
	}
	addThreadArtifactCandidate(&candidates, seen, "shell_snapshot", filepath.Join(homeDir, ".codex", "shell_snapshots", codexThreadID+".sh"))

	searchRoots := []string{
		filepath.Join(homeDir, ".codex", "sessions"),
		filepath.Join(homeDir, ".codex", "shell_snapshots"),
		filepath.Join(homeDir, ".codex", "archived_sessions"),
		filepath.Join(homeDir, ".codex", "tmp"),
	}
	for _, root := range searchRoots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d == nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.Contains(name, codexThreadID) {
				return nil
			}
			addThreadArtifactCandidate(&candidates, seen, InferThreadArtifactKind(name), path)
			return nil
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].Path < candidates[j].Path
	})
	return candidates
}

// InferThreadArtifactKind infers the artifact kind from filename.
func InferThreadArtifactKind(filename string) string {
	lower := strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasPrefix(lower, "rollout-") && strings.HasSuffix(lower, ".jsonl"):
		return "rollout"
	case strings.Contains(lower, "bp"):
		return "breakpoint"
	case strings.HasSuffix(lower, ".sh"):
		return "shell_snapshot"
	case strings.HasSuffix(lower, ".jsonl"):
		return "jsonl"
	default:
		return "artifact"
	}
}

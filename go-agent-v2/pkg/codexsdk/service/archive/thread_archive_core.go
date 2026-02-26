package archive

import "strings"

type ThreadArchiveFile struct {
	Kind         string `json:"kind"`
	SourcePath   string `json:"sourcePath"`
	ArchivedPath string `json:"archivedPath"`
	SizeBytes    int64  `json:"sizeBytes"`
	SHA256       string `json:"sha256,omitempty"`
}

type ThreadArchiveManifest struct {
	ThreadID      string              `json:"threadId"`
	CodexThreadID string              `json:"codexThreadId,omitempty"`
	ArchivedAt    string              `json:"archivedAt"`
	ArchiveDir    string              `json:"archiveDir"`
	RolloutPath   string              `json:"rolloutPath,omitempty"`
	Files         []ThreadArchiveFile `json:"files"`
}

// ThreadArchiveRestoreNotice describes archive integrity status before restore.
type ThreadArchiveRestoreNotice struct {
	Modified      bool
	ManifestPath  string
	ModifiedFiles []string
}

type ThreadArtifactCandidate struct {
	Kind string
	Path string
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

// MergeThreadArchiveMaps merges archive timestamps and keeps the latest per thread.
func MergeThreadArchiveMaps(dst map[string]int64, src map[string]int64) map[string]int64 {
	if dst == nil {
		dst = map[string]int64{}
	}
	for id, at := range src {
		trimmedID := strings.TrimSpace(id)
		if trimmedID == "" || at <= 0 {
			continue
		}
		if current, ok := dst[trimmedID]; !ok || at > current {
			dst[trimmedID] = at
		}
	}
	return dst
}

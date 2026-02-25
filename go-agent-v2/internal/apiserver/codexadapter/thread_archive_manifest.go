package codexadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type threadArchiveManifestCandidate struct {
	Path       string
	ModifiedAt int64
}

func appendThreadArchiveManifestCandidate(candidates *[]threadArchiveManifestCandidate, path string) error {
	if candidates == nil {
		return nil
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fileInfo.IsDir() {
		return nil
	}
	*candidates = append(*candidates, threadArchiveManifestCandidate{
		Path:       path,
		ModifiedAt: fileInfo.ModTime().UnixNano(),
	})
	return nil
}

// FindLatestThreadArchiveManifestPath returns the newest manifest path under thread dir.
func FindLatestThreadArchiveManifestPath(threadDir string) (string, error) {
	info, err := os.Stat(threadDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", apperrors.Newf("findLatestThreadArchiveManifestPath", "thread archive path is not a directory: %s", threadDir)
	}

	candidates := make([]threadArchiveManifestCandidate, 0, 8)

	if err := appendThreadArchiveManifestCandidate(&candidates, filepath.Join(threadDir, "manifest.json")); err != nil {
		return "", apperrors.Wrap(err, "findLatestThreadArchiveManifestPath", "stat legacy manifest")
	}
	entries, err := os.ReadDir(threadDir)
	if err != nil {
		return "", apperrors.Wrap(err, "findLatestThreadArchiveManifestPath", "read thread archive dir")
	}
	for _, entry := range entries {
		if entry == nil || !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(threadDir, entry.Name(), "manifest.json")
		if err := appendThreadArchiveManifestCandidate(&candidates, manifestPath); err != nil {
			return "", apperrors.Wrapf(err, "findLatestThreadArchiveManifestPath", "stat manifest for snapshot %s", entry.Name())
		}
	}
	if len(candidates) == 0 {
		return "", os.ErrNotExist
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].ModifiedAt != candidates[j].ModifiedAt {
			return candidates[i].ModifiedAt > candidates[j].ModifiedAt
		}
		return candidates[i].Path > candidates[j].Path
	})
	return candidates[0].Path, nil
}

// ReadThreadArchiveManifest loads archive manifest from disk.
func ReadThreadArchiveManifest(manifestPath string) (ThreadArchiveManifest, error) {
	manifest := ThreadArchiveManifest{}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

// WriteThreadArchiveManifest persists archive manifest in archiveDir/manifest.json.
func WriteThreadArchiveManifest(manifest ThreadArchiveManifest) error {
	if strings.TrimSpace(manifest.ArchiveDir) == "" {
		return apperrors.New("writeThreadArchiveManifest", "archive dir is empty")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(manifest.ArchiveDir, "manifest.json")
	return os.WriteFile(manifestPath, data, 0o644)
}

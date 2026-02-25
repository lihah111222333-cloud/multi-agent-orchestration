package codexadapter

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// ThreadArchiveRestoreNotice describes archive integrity status before restore.
type ThreadArchiveRestoreNotice struct {
	Modified      bool
	ManifestPath  string
	ModifiedFiles []string
}

// InspectThreadArchiveForRestore verifies archived files before restore.
func InspectThreadArchiveForRestore(
	threadID string,
	resolveThreadArchiveRoot func() (string, error),
	sanitizeArchiveNameStrict func(string) (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	fileSHA256 func(path string) (string, error),
	findLatestThreadArchiveManifestPath func(threadDir string) (string, error),
	readThreadArchiveManifest func(manifestPath string) (ThreadArchiveManifest, error),
) (ThreadArchiveRestoreNotice, error) {
	notice := ThreadArchiveRestoreNotice{
		Modified:      false,
		ManifestPath:  "",
		ModifiedFiles: []string{},
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return notice, nil
	}
	if resolveThreadArchiveRoot == nil ||
		sanitizeArchiveNameStrict == nil ||
		pathWithinRoot == nil ||
		fileSHA256 == nil {
		return notice, apperrors.New("inspectThreadArchiveForRestore", "inspect dependencies are not configured")
	}
	findLatestManifest := findLatestThreadArchiveManifestPath
	if findLatestManifest == nil {
		findLatestManifest = FindLatestThreadArchiveManifestPath
	}
	readManifest := readThreadArchiveManifest
	if readManifest == nil {
		readManifest = ReadThreadArchiveManifest
	}

	rootDir, err := resolveThreadArchiveRoot()
	if err != nil {
		return notice, err
	}
	safeThreadID, err := sanitizeArchiveNameStrict(id)
	if err != nil {
		return notice, apperrors.Wrap(err, "inspectThreadArchiveForRestore", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	manifestPath, err := findLatestManifest(threadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return notice, nil
		}
		return notice, apperrors.Wrap(err, "inspectThreadArchiveForRestore", "find latest manifest")
	}
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return notice, apperrors.Wrap(err, "inspectThreadArchiveForRestore", "read manifest")
	}
	notice.ManifestPath = manifestPath

	modified := make([]string, 0, len(manifest.Files))
	for _, meta := range manifest.Files {
		archivedPath := strings.TrimSpace(meta.ArchivedPath)
		if archivedPath == "" {
			continue
		}
		if !filepath.IsAbs(archivedPath) && strings.TrimSpace(manifest.ArchiveDir) != "" {
			archivedPath = filepath.Join(strings.TrimSpace(manifest.ArchiveDir), archivedPath)
		}
		if strings.TrimSpace(manifest.ArchiveDir) != "" {
			withinRoot, err := pathWithinRoot(manifest.ArchiveDir, archivedPath)
			if err != nil || !withinRoot {
				modified = append(modified, archivedPath)
				continue
			}
		}
		info, err := os.Stat(archivedPath)
		if err != nil || info.IsDir() {
			modified = append(modified, archivedPath)
			continue
		}
		if meta.SizeBytes > 0 && info.Size() != meta.SizeBytes {
			modified = append(modified, archivedPath)
			continue
		}
		if checksum := strings.TrimSpace(meta.SHA256); checksum != "" {
			actualSHA256, err := fileSHA256(archivedPath)
			if err != nil || !strings.EqualFold(checksum, actualSHA256) {
				modified = append(modified, archivedPath)
				continue
			}
		}
	}
	if len(modified) > 0 {
		sort.Strings(modified)
		notice.Modified = true
		notice.ModifiedFiles = modified
	}
	return notice, nil
}

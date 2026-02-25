package codexadapter

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func appendRestoreSkippedSource(
	threadID string,
	skipped *[]string,
	skippedSet map[string]struct{},
	sourcePath string,
	archivedPath string,
	reason string,
	skipErr error,
) {
	if skipped == nil {
		return
	}
	value := strings.TrimSpace(sourcePath)
	if value == "" {
		return
	}
	if skipErr != nil {
		logger.Error("thread/unarchive: restore artifact skipped",
			logger.FieldThreadID, threadID,
			"source_path", value,
			"archived_path", strings.TrimSpace(archivedPath),
			"reason", reason,
			logger.FieldError, skipErr,
		)
	} else {
		logger.Error("thread/unarchive: restore artifact skipped",
			logger.FieldThreadID, threadID,
			"source_path", value,
			"archived_path", strings.TrimSpace(archivedPath),
			"reason", reason,
		)
	}
	if _, ok := skippedSet[value]; ok {
		return
	}
	skippedSet[value] = struct{}{}
	*skipped = append(*skipped, value)
}

// RestoreThreadArchiveSources restores archived source files back to codex root.
func RestoreThreadArchiveSources(
	threadID string,
	resolveThreadArchiveRoot func() (string, error),
	sanitizeArchiveNameStrict func(string) (string, error),
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	copyFileOverwrite func(srcPath, targetPath string) error,
	fileSHA256 func(path string) (string, error),
	findLatestThreadArchiveManifestPath func(threadDir string) (string, error),
	readThreadArchiveManifest func(manifestPath string) (ThreadArchiveManifest, error),
) ([]string, []string, error) {
	restored := []string{}
	skipped := []string{}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return restored, skipped, nil
	}
	if resolveThreadArchiveRoot == nil ||
		sanitizeArchiveNameStrict == nil ||
		resolveCodexRootDir == nil ||
		pathWithinRoot == nil ||
		copyFileOverwrite == nil ||
		fileSHA256 == nil {
		return nil, nil, apperrors.New("restoreThreadArchiveSources", "restore dependencies are not configured")
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
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "resolve archive root")
	}
	safeThreadID, err := sanitizeArchiveNameStrict(id)
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	manifestPath, err := findLatestManifest(threadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return restored, skipped, nil
		}
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "find latest manifest")
	}
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "read manifest")
	}
	codexRoot, err := resolveCodexRootDir()
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "resolve codex root")
	}

	restoredSet := map[string]struct{}{}
	skippedSet := map[string]struct{}{}

	for _, meta := range manifest.Files {
		srcPath := strings.TrimSpace(meta.SourcePath)
		if srcPath == "" {
			continue
		}
		withinCodex, err := pathWithinRoot(codexRoot, srcPath)
		if err != nil {
			appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, "", "validate source path scope", err)
			continue
		}
		if !withinCodex {
			appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, "", "source path is outside codex root", nil)
			continue
		}

		archivedPath := strings.TrimSpace(meta.ArchivedPath)
		if archivedPath == "" {
			appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, "", "archived path is empty", nil)
			continue
		}
		if !filepath.IsAbs(archivedPath) && strings.TrimSpace(manifest.ArchiveDir) != "" {
			archivedPath = filepath.Join(strings.TrimSpace(manifest.ArchiveDir), archivedPath)
		}
		if strings.TrimSpace(manifest.ArchiveDir) != "" {
			withinArchive, err := pathWithinRoot(manifest.ArchiveDir, archivedPath)
			if err != nil {
				appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "validate archived path scope", err)
				continue
			}
			if !withinArchive {
				appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "archived path is outside archive root", nil)
				continue
			}
		}

		info, err := os.Stat(archivedPath)
		if err != nil {
			appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "stat archived file", err)
			continue
		}
		if info.IsDir() {
			appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "archived path is a directory", nil)
			continue
		}

		expectedSHA256 := strings.TrimSpace(meta.SHA256)
		if expectedSHA256 != "" {
			actualArchiveSHA256, err := fileSHA256(archivedPath)
			if err != nil {
				appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "compute archived checksum", err)
				continue
			}
			if !strings.EqualFold(expectedSHA256, actualArchiveSHA256) {
				appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "archived checksum mismatch", nil)
				continue
			}
		}

		if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
			appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "ensure source parent dir", err)
			continue
		}
		if err := copyFileOverwrite(archivedPath, srcPath); err != nil {
			appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "restore file to source path", err)
			continue
		}
		if expectedSHA256 != "" {
			actualSourceSHA256, err := fileSHA256(srcPath)
			if err != nil {
				_ = os.Remove(srcPath)
				appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "compute restored source checksum", err)
				continue
			}
			if !strings.EqualFold(expectedSHA256, actualSourceSHA256) {
				_ = os.Remove(srcPath)
				appendRestoreSkippedSource(id, &skipped, skippedSet, srcPath, archivedPath, "restored source checksum mismatch", nil)
				continue
			}
		}
		if _, ok := restoredSet[srcPath]; !ok {
			restoredSet[srcPath] = struct{}{}
			restored = append(restored, srcPath)
		}
	}
	sort.Strings(restored)
	sort.Strings(skipped)
	return restored, skipped, nil
}

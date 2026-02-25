package codexadapter

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// PruneArchivedCodexSourceFiles removes source codex files once archived safely.
func PruneArchivedCodexSourceFiles(
	threadID string,
	files []ThreadArchiveFile,
	archiveDir string,
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	fileSHA256 func(path string) (string, error),
	removeEmptyCodexParentDir func(startDir, codexRoot string),
) {
	if len(files) == 0 {
		return
	}
	if resolveCodexRootDir == nil || pathWithinRoot == nil || fileSHA256 == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	codexRoot, err := resolveCodexRootDir()
	if err != nil {
		logger.Error("thread/archive: resolve codex root failed",
			logger.FieldThreadID, threadID,
			logger.FieldError, err,
		)
		return
	}

	archiveRoot := strings.TrimSpace(archiveDir)
	seen := make(map[string]struct{}, len(files))
	deleted := 0
	for _, meta := range files {
		srcPath := strings.TrimSpace(meta.SourcePath)
		if srcPath == "" {
			continue
		}
		if _, ok := seen[srcPath]; ok {
			continue
		}
		seen[srcPath] = struct{}{}

		withinCodex, err := pathWithinRoot(codexRoot, srcPath)
		if err != nil || !withinCodex {
			continue
		}
		if archiveRoot != "" {
			if withinArchive, err := pathWithinRoot(archiveRoot, srcPath); err == nil && withinArchive {
				continue
			}
		}

		info, err := os.Stat(srcPath)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Error("thread/archive: stat source artifact failed",
					logger.FieldThreadID, threadID,
					"source_path", srcPath,
					logger.FieldError, err,
				)
			}
			continue
		}
		if info.IsDir() {
			continue
		}

		expectedSHA256 := strings.TrimSpace(meta.SHA256)
		if expectedSHA256 == "" {
			continue
		}
		sourceSHA256, err := fileSHA256(srcPath)
		if err != nil {
			logger.Error("thread/archive: source artifact checksum failed",
				logger.FieldThreadID, threadID,
				"source_path", srcPath,
				logger.FieldError, err,
			)
			continue
		}
		if !strings.EqualFold(expectedSHA256, sourceSHA256) {
			logger.Error("thread/archive: source artifact changed after backup, skip delete",
				logger.FieldThreadID, threadID,
				"source_path", srcPath,
				"expected_sha256", expectedSHA256,
				"actual_sha256", sourceSHA256,
			)
			continue
		}

		if err := os.Remove(srcPath); err != nil {
			logger.Error("thread/archive: remove source artifact failed",
				logger.FieldThreadID, threadID,
				"source_path", srcPath,
				logger.FieldError, err,
			)
			continue
		}
		deleted++
		if removeEmptyCodexParentDir != nil {
			removeEmptyCodexParentDir(filepath.Dir(srcPath), codexRoot)
		}
	}

	if deleted > 0 {
		logger.Info("thread/archive: pruned codex source artifacts",
			logger.FieldThreadID, threadID,
			"deleted_count", deleted,
		)
	}
}

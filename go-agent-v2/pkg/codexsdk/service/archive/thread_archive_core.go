package archive

import (
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/pathutil"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

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

type ThreadArchiveFileState struct {
	Exists    bool
	IsDir     bool
	SizeBytes int64
}

func InferThreadArtifactKind(filename string) string { return inferThreadArtifactKind(filename) }

type ThreadArtifactCandidate = threadArtifactCandidate

func BuildThreadArchiveRestoreDeps(
	resolveThreadArchiveRoot func() (string, error),
	sanitizeArchiveNameStrict func(string) (string, error),
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	copyFileOverwrite func(srcPath, targetPath string) error,
	fileSHA256 func(path string) (string, error),
	findLatestManifestPath func(threadDir string) (manifestPath string, found bool, err error),
	readManifestFile func(manifestPath string) (ThreadArchiveManifest, error),
	fileState func(path string) (ThreadArchiveFileState, error),
	removeFile func(path string) error,
) ThreadArchiveRestoreDeps {
	return ThreadArchiveRestoreDeps{
		resolveThreadArchiveRoot:  resolveThreadArchiveRoot,
		sanitizeArchiveNameStrict: sanitizeArchiveNameStrict,
		resolveCodexRootDir:       resolveCodexRootDir,
		pathWithinRoot:            pathWithinRoot,
		copyFileOverwrite:         copyFileOverwrite,
		fileSHA256:                fileSHA256,
		findLatestManifestPath:    findLatestManifestPath,
		readManifestFile:          readManifestFile,
		fileState:                 fileState,
		removeFile:                removeFile,
	}
}

func InspectThreadArchiveForRestore(threadID string, deps ThreadArchiveRestoreDeps) (ThreadArchiveRestoreNotice, error) {
	return inspectThreadArchiveForRestoreWithDeps(threadID, deps)
}

func RestoreThreadArchiveSources(
	threadID string,
	resolveThreadArchiveRoot func() (string, error),
	sanitizeArchiveNameStrict func(string) (string, error),
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	copyFileOverwrite func(srcPath, targetPath string) error,
	fileSHA256 func(path string) (string, error),
	findLatestManifestPath func(threadDir string) (manifestPath string, found bool, err error),
	readManifestFile func(manifestPath string) (ThreadArchiveManifest, error),
	fileState func(path string) (ThreadArchiveFileState, error),
	removeFile func(path string) error,
) ([]string, []string, error) {
	deps := BuildThreadArchiveRestoreDeps(resolveThreadArchiveRoot, sanitizeArchiveNameStrict, resolveCodexRootDir, pathWithinRoot, copyFileOverwrite, fileSHA256, findLatestManifestPath, readManifestFile, fileState, removeFile)
	return restoreThreadArchiveSourcesWithDeps(threadID, deps)
}

type ThreadArchiveRestoreNotice struct {
	Modified      bool
	ManifestPath  string
	ModifiedFiles []string
}

type ThreadArchiveRestoreDeps struct {
	resolveThreadArchiveRoot, resolveCodexRootDir func() (string, error)
	sanitizeArchiveNameStrict                     func(string) (string, error)
	pathWithinRoot                                func(root, path string) (bool, error)
	copyFileOverwrite                             func(srcPath, targetPath string) error
	fileSHA256                                    func(path string) (string, error)
	findLatestManifestPath                        func(threadDir string) (manifestPath string, found bool, err error)
	readManifestFile                              func(manifestPath string) (ThreadArchiveManifest, error)
	fileState                                     func(path string) (ThreadArchiveFileState, error)
	removeFile                                    func(path string) error
}

func (deps ThreadArchiveRestoreDeps) validateInspect() error {
	if deps.resolveThreadArchiveRoot == nil || deps.sanitizeArchiveNameStrict == nil || deps.pathWithinRoot == nil || deps.fileSHA256 == nil || deps.findLatestManifestPath == nil || deps.readManifestFile == nil || deps.fileState == nil {
		return apperrors.New("inspectThreadArchiveForRestore", "inspect dependencies are not configured")
	}
	return nil
}

func (deps ThreadArchiveRestoreDeps) validateRestore() error {
	if deps.resolveCodexRootDir == nil || deps.copyFileOverwrite == nil || deps.validateInspect() != nil {
		return apperrors.New("restoreThreadArchiveSources", "restore dependencies are not configured")
	}
	return nil
}

func loadThreadArchiveManifestScope(threadID, op string, deps ThreadArchiveRestoreDeps) (manifestPath string, manifest ThreadArchiveManifest, found bool, err error) {
	if threadID = strings.TrimSpace(threadID); threadID == "" {
		return "", ThreadArchiveManifest{}, false, nil
	}
	rootDir, err := deps.resolveThreadArchiveRoot()
	if err != nil {
		return "", ThreadArchiveManifest{}, false, apperrors.Wrap(err, op, "resolve archive root")
	}
	safeThreadID, err := deps.sanitizeArchiveNameStrict(threadID)
	if err != nil {
		return "", ThreadArchiveManifest{}, false, apperrors.Wrap(err, op, "sanitize thread id")
	}
	manifestPath, found, err = deps.findLatestManifestPath(pathutil.Join(rootDir, safeThreadID))
	if err != nil {
		return "", ThreadArchiveManifest{}, false, apperrors.Wrap(err, op, "find latest manifest")
	}
	if !found {
		return "", ThreadArchiveManifest{}, false, nil
	}
	manifest, err = deps.readManifestFile(manifestPath)
	if err != nil {
		return "", ThreadArchiveManifest{}, false, apperrors.Wrap(err, op, "read manifest")
	}
	return manifestPath, manifest, true, nil
}

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

func ParseArchiveTimestamp(raw string) int64 {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	if idx := strings.Index(value, "-"); idx > 0 {
		value = value[:idx]
	}
	at, err := strconv.ParseInt(value, 10, 64)
	if err != nil || at <= 0 {
		return 0
	}
	return at
}

func validateOptionalChecksum(fileSHA256 func(path string) (string, error), path, checksum, computeReason, mismatchReason string) (string, error) {
	checksum = strings.TrimSpace(checksum)
	if checksum == "" {
		return "", nil
	}
	actualSHA256, err := fileSHA256(path)
	if err != nil {
		return computeReason, err
	}
	if strings.EqualFold(checksum, actualSHA256) {
		return "", nil
	}
	return mismatchReason, nil
}

func validateArchivedArtifact(
	meta ThreadArchiveFile,
	archiveDir string,
	pathWithinRoot func(root, path string) (bool, error),
	fileState func(path string) (ThreadArchiveFileState, error),
	fileSHA256 func(path string) (string, error),
	checkSize bool,
	emptyPathReason string,
) (archivedPath string, reason string, err error) {
	archivedPath = strings.TrimSpace(meta.ArchivedPath)
	if archivedPath == "" {
		return "", emptyPathReason, nil
	}
	archiveRoot := strings.TrimSpace(archiveDir)
	if archiveRoot != "" && !pathutil.IsAbs(archivedPath) {
		archivedPath = pathutil.Join(archiveRoot, archivedPath)
	}
	if archiveRoot != "" {
		withinArchive, err := pathWithinRoot(archiveRoot, archivedPath)
		if err != nil {
			return archivedPath, "validate archived path scope", err
		}
		if !withinArchive {
			return archivedPath, "archived path is outside archive root", nil
		}
	}
	state, err := fileState(archivedPath)
	if err != nil {
		return archivedPath, "stat archived file", err
	}
	if !state.Exists {
		return archivedPath, "archived file missing", nil
	}
	if state.IsDir {
		return archivedPath, "archived path is a directory", nil
	}
	if checkSize && meta.SizeBytes > 0 && state.SizeBytes != meta.SizeBytes {
		return archivedPath, "archived file size mismatch", nil
	}
	reason, err = validateOptionalChecksum(fileSHA256, archivedPath, meta.SHA256, "compute archived checksum", "archived checksum mismatch")
	return archivedPath, reason, err
}

func inspectThreadArchiveForRestoreWithDeps(threadID string, deps ThreadArchiveRestoreDeps) (ThreadArchiveRestoreNotice, error) {
	notice := ThreadArchiveRestoreNotice{ModifiedFiles: []string{}}
	if err := deps.validateInspect(); err != nil {
		return notice, err
	}

	manifestPath, manifest, ok, err := loadThreadArchiveManifestScope(threadID, "inspectThreadArchiveForRestore", deps)
	if err != nil {
		return notice, err
	}
	if !ok {
		return notice, nil
	}
	notice.ManifestPath = manifestPath
	modified := make([]string, 0, len(manifest.Files))
	for _, meta := range manifest.Files {
		archivedPath, reason, err := validateArchivedArtifact(meta, manifest.ArchiveDir, deps.pathWithinRoot, deps.fileState, deps.fileSHA256, true, "")
		if archivedPath == "" {
			continue
		}
		if err != nil || reason != "" {
			modified = append(modified, archivedPath)
		}
	}
	sort.Strings(modified)
	notice.Modified = len(modified) > 0
	notice.ModifiedFiles = modified
	return notice, nil
}

// PruneArchivedCodexSourceFiles removes source codex files once archived safely.
func PruneArchivedCodexSourceFiles(
	threadID string,
	files []ThreadArchiveFile,
	archiveDir string,
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	fileSHA256 func(path string) (string, error),
	fileState func(path string) (ThreadArchiveFileState, error),
	removeFile func(path string) error,
	removeEmptyCodexParentDir func(startDir, codexRoot string),
) {
	if len(files) == 0 || resolveCodexRootDir == nil || pathWithinRoot == nil || fileSHA256 == nil || fileState == nil || removeFile == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	codexRoot, err := resolveCodexRootDir()
	if err != nil {
		logger.Error("thread/archive: resolve codex root failed", logger.FieldThreadID, threadID, logger.FieldError, err)
		return
	}

	archiveRoot, seen, deleted := strings.TrimSpace(archiveDir), make(map[string]struct{}, len(files)), 0
	for _, meta := range files {
		srcPath := strings.TrimSpace(meta.SourcePath)
		if srcPath == "" {
			continue
		}
		if _, ok := seen[srcPath]; ok {
			continue
		}
		seen[srcPath] = struct{}{}
		if withinCodex, err := pathWithinRoot(codexRoot, srcPath); err != nil || !withinCodex {
			continue
		}
		if withinArchive, err := pathWithinRoot(archiveRoot, srcPath); archiveRoot != "" && err == nil && withinArchive {
			continue
		}
		state, err := fileState(srcPath)
		if err != nil {
			logger.Error("thread/archive: stat source artifact failed", logger.FieldThreadID, threadID, "source_path", srcPath, logger.FieldError, err)
			continue
		}
		if !state.Exists || state.IsDir {
			continue
		}
		expectedSHA256 := strings.TrimSpace(meta.SHA256)
		if expectedSHA256 == "" {
			continue
		}
		sourceSHA256, err := fileSHA256(srcPath)
		if err != nil {
			logger.Error("thread/archive: source artifact checksum failed", logger.FieldThreadID, threadID, "source_path", srcPath, logger.FieldError, err)
			continue
		}
		if !strings.EqualFold(expectedSHA256, sourceSHA256) {
			logger.Error("thread/archive: source artifact changed after backup, skip delete", logger.FieldThreadID, threadID, "source_path", srcPath, "expected_sha256", expectedSHA256, "actual_sha256", sourceSHA256)
			continue
		}
		if err := removeFile(srcPath); err != nil {
			logger.Error("thread/archive: remove source artifact failed", logger.FieldThreadID, threadID, "source_path", srcPath, logger.FieldError, err)
			continue
		}
		deleted++
		if removeEmptyCodexParentDir != nil {
			removeEmptyCodexParentDir(pathutil.Dir(srcPath), codexRoot)
		}
	}
	if deleted > 0 {
		logger.Info("thread/archive: pruned codex source artifacts", logger.FieldThreadID, threadID, "deleted_count", deleted)
	}
}

func appendRestoreSkippedSource(
	threadID string,
	skippedSet map[string]struct{},
	sourcePath string,
	archivedPath string,
	reason string,
	skipErr error,
) {
	if sourcePath = strings.TrimSpace(sourcePath); sourcePath == "" {
		return
	}
	logFields := []any{logger.FieldThreadID, threadID, "source_path", sourcePath, "archived_path", strings.TrimSpace(archivedPath), "reason", reason}
	if skipErr != nil {
		logFields = append(logFields, logger.FieldError, skipErr)
	}
	logger.Error("thread/unarchive: restore artifact skipped", logFields...)
	skippedSet[sourcePath] = struct{}{}
}

func restoreThreadArchiveSourcesWithDeps(threadID string, deps ThreadArchiveRestoreDeps) ([]string, []string, error) {
	if threadID = strings.TrimSpace(threadID); threadID == "" {
		return []string{}, []string{}, nil
	}
	if err := deps.validateRestore(); err != nil {
		return nil, nil, err
	}

	_, manifest, ok, err := loadThreadArchiveManifestScope(threadID, "restoreThreadArchiveSources", deps)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return []string{}, []string{}, nil
	}

	codexRoot, err := deps.resolveCodexRootDir()
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "resolve codex root")
	}

	restoredSet, skippedSet := map[string]struct{}{}, map[string]struct{}{}
	for _, meta := range manifest.Files {
		sourcePath := strings.TrimSpace(meta.SourcePath)
		if sourcePath == "" {
			continue
		}
		if withinCodex, err := deps.pathWithinRoot(codexRoot, sourcePath); err != nil || !withinCodex {
			reason := "source path is outside codex root"
			if err != nil {
				reason = "validate source path scope"
			}
			appendRestoreSkippedSource(threadID, skippedSet, sourcePath, "", reason, err)
			continue
		}
		archivedPath, reason, skipErr := validateArchivedArtifact(meta, manifest.ArchiveDir, deps.pathWithinRoot, deps.fileState, deps.fileSHA256, false, "archived path is empty")
		if reason != "" || skipErr != nil {
			appendRestoreSkippedSource(threadID, skippedSet, sourcePath, archivedPath, reason, skipErr)
			continue
		}
		if err := deps.copyFileOverwrite(archivedPath, sourcePath); err != nil {
			appendRestoreSkippedSource(threadID, skippedSet, sourcePath, archivedPath, "restore file to source path", err)
			continue
		}
		if reason, err := validateOptionalChecksum(deps.fileSHA256, sourcePath, meta.SHA256, "compute restored source checksum", "restored source checksum mismatch"); err != nil || reason != "" {
			if deps.removeFile != nil {
				_ = deps.removeFile(sourcePath)
			}
			appendRestoreSkippedSource(threadID, skippedSet, sourcePath, archivedPath, reason, err)
			continue
		}
		restoredSet[sourcePath] = struct{}{}
	}
	return slices.Sorted(maps.Keys(restoredSet)), slices.Sorted(maps.Keys(skippedSet)), nil
}

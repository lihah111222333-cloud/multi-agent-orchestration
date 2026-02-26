package codexadapter

import (
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type threadArchiveFile struct {
	Kind         string `json:"kind"`
	SourcePath   string `json:"sourcePath"`
	ArchivedPath string `json:"archivedPath"`
	SizeBytes    int64  `json:"sizeBytes"`
	SHA256       string `json:"sha256,omitempty"`
}

type threadArchiveManifest struct {
	ThreadID      string              `json:"threadId"`
	CodexThreadID string              `json:"codexThreadId,omitempty"`
	ArchivedAt    string              `json:"archivedAt"`
	ArchiveDir    string              `json:"archiveDir"`
	RolloutPath   string              `json:"rolloutPath,omitempty"`
	Files         []threadArchiveFile `json:"files"`
}

// InferThreadArtifactKind infers the artifact kind from filename.
func InferThreadArtifactKind(filename string) string {
	return inferThreadArtifactKind(filename)
}

// threadArchiveRestoreNotice describes archive integrity status before restore.
type threadArchiveRestoreNotice struct {
	Modified      bool
	ManifestPath  string
	ModifiedFiles []string
}

type threadArchiveRestoreDeps struct {
	resolveThreadArchiveRoot  func() (string, error)
	sanitizeArchiveNameStrict func(string) (string, error)
	resolveCodexRootDir       func() (string, error)
	pathWithinRoot            func(root, path string) (bool, error)
	copyFileOverwrite         func(srcPath, targetPath string) error
	fileSHA256                func(path string) (string, error)
	findLatestManifestPath    func(threadDir string) (string, error)
	readManifestFile          func(manifestPath string) (threadArchiveManifest, error)
}

type threadArchiveManifestScope struct {
	ThreadID     string
	ManifestPath string
	Manifest     threadArchiveManifest
}

func buildThreadArchiveRestoreDeps(
	resolveThreadArchiveRoot func() (string, error),
	sanitizeArchiveNameStrict func(string) (string, error),
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	copyFileOverwrite func(srcPath, targetPath string) error,
	fileSHA256 func(path string) (string, error),
	findLatestManifestPath func(threadDir string) (string, error),
	readManifestFile func(manifestPath string) (threadArchiveManifest, error),
) threadArchiveRestoreDeps {
	return threadArchiveRestoreDeps{
		resolveThreadArchiveRoot:  resolveThreadArchiveRoot,
		sanitizeArchiveNameStrict: sanitizeArchiveNameStrict,
		resolveCodexRootDir:       resolveCodexRootDir,
		pathWithinRoot:            pathWithinRoot,
		copyFileOverwrite:         copyFileOverwrite,
		fileSHA256:                fileSHA256,
		findLatestManifestPath:    findLatestManifestPath,
		readManifestFile:          readManifestFile,
	}
}

func (deps threadArchiveRestoreDeps) withDefaults() threadArchiveRestoreDeps {
	if deps.findLatestManifestPath == nil {
		deps.findLatestManifestPath = findLatestThreadArchiveManifestPath
	}
	if deps.readManifestFile == nil {
		deps.readManifestFile = readThreadArchiveManifest
	}
	return deps
}

func (deps threadArchiveRestoreDeps) validateInspect() error {
	if deps.resolveThreadArchiveRoot == nil ||
		deps.sanitizeArchiveNameStrict == nil ||
		deps.pathWithinRoot == nil ||
		deps.fileSHA256 == nil {
		return apperrors.New("inspectThreadArchiveForRestore", "inspect dependencies are not configured")
	}
	return nil
}

func (deps threadArchiveRestoreDeps) validateRestore() error {
	if err := deps.validateInspect(); err != nil {
		return apperrors.New("restoreThreadArchiveSources", "restore dependencies are not configured")
	}
	if deps.resolveCodexRootDir == nil || deps.copyFileOverwrite == nil {
		return apperrors.New("restoreThreadArchiveSources", "restore dependencies are not configured")
	}
	return nil
}

func loadThreadArchiveManifestScope(threadID, op string, deps threadArchiveRestoreDeps) (threadArchiveManifestScope, bool, error) {
	scope := threadArchiveManifestScope{}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return scope, false, nil
	}
	deps = deps.withDefaults()

	rootDir, err := deps.resolveThreadArchiveRoot()
	if err != nil {
		return scope, false, apperrors.Wrap(err, op, "resolve archive root")
	}
	safeThreadID, err := deps.sanitizeArchiveNameStrict(id)
	if err != nil {
		return scope, false, apperrors.Wrap(err, op, "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	manifestPath, err := deps.findLatestManifestPath(threadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return scope, false, nil
		}
		return scope, false, apperrors.Wrap(err, op, "find latest manifest")
	}
	manifest, err := deps.readManifestFile(manifestPath)
	if err != nil {
		return scope, false, apperrors.Wrap(err, op, "read manifest")
	}

	scope.ThreadID = id
	scope.ManifestPath = manifestPath
	scope.Manifest = manifest
	return scope, true, nil
}

func mergeThreadArchiveMaps(dst map[string]int64, src map[string]int64) map[string]int64 {
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

func loadThreadArchiveMapFromDisk() (map[string]int64, error) {
	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		return nil, err
	}
	return collectThreadArchiveMapFromRoot(rootDir)
}

func collectThreadArchiveMapFromRoot(rootDir string) (map[string]int64, error) {
	result := map[string]int64{}
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return result, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		threadID := strings.TrimSpace(entry.Name())
		if threadID == "" {
			continue
		}
		threadDir := filepath.Join(root, entry.Name())
		archivedAt := latestArchiveTimestampFromThreadDir(threadDir)

		manifestPath, manifestErr := findLatestThreadArchiveManifestPath(threadDir)
		if manifestErr == nil {
			manifest, readErr := readThreadArchiveManifest(manifestPath)
			if readErr == nil {
				if id := strings.TrimSpace(manifest.ThreadID); id != "" {
					threadID = id
				}
				if parsed := parseArchiveTimestamp(manifest.ArchivedAt); parsed > 0 {
					archivedAt = parsed
				}
			}
		}
		if archivedAt <= 0 {
			continue
		}
		if current, ok := result[threadID]; !ok || archivedAt > current {
			result[threadID] = archivedAt
		}
	}
	return result, nil
}

func latestArchiveTimestampFromThreadDir(threadDir string) int64 {
	entries, err := os.ReadDir(threadDir)
	if err != nil {
		return 0
	}
	var maxAt int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		at := parseArchiveTimestamp(entry.Name())
		if at > maxAt {
			maxAt = at
		}
	}
	return maxAt
}

func parseArchiveTimestamp(raw string) int64 {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	// Snapshot directories may include a numeric suffix, e.g. "1700000000000-2".
	if idx := strings.Index(value, "-"); idx > 0 {
		value = value[:idx]
	}
	at, err := strconv.ParseInt(value, 10, 64)
	if err != nil || at <= 0 {
		return 0
	}
	return at
}

// inspectThreadArchiveForRestore verifies archived files before restore.
func inspectThreadArchiveForRestore(threadID string, deps threadArchiveRestoreDeps) (threadArchiveRestoreNotice, error) {
	return inspectThreadArchiveForRestoreWithDeps(threadID, deps)
}

func inspectThreadArchiveForRestoreWithDeps(threadID string, deps threadArchiveRestoreDeps) (threadArchiveRestoreNotice, error) {
	notice := threadArchiveRestoreNotice{
		Modified:      false,
		ManifestPath:  "",
		ModifiedFiles: []string{},
	}
	deps = deps.withDefaults()
	if err := deps.validateInspect(); err != nil {
		return notice, err
	}

	scope, ok, err := loadThreadArchiveManifestScope(threadID, "inspectThreadArchiveForRestore", deps)
	if err != nil {
		return notice, err
	}
	if !ok {
		return notice, nil
	}

	manifest := scope.Manifest
	notice.ManifestPath = scope.ManifestPath
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
			withinRoot, err := deps.pathWithinRoot(manifest.ArchiveDir, archivedPath)
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
			actualSHA256, err := deps.fileSHA256(archivedPath)
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

// pruneArchivedCodexSourceFiles removes source codex files once archived safely.
func pruneArchivedCodexSourceFiles(
	threadID string,
	files []threadArchiveFile,
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

func restoreThreadArchiveEntry(
	threadID string,
	meta threadArchiveFile,
	manifest threadArchiveManifest,
	codexRoot string,
	pathWithinRoot func(root, path string) (bool, error),
	copyFileOverwrite func(srcPath, targetPath string) error,
	fileSHA256 func(path string) (string, error),
	restoredSet map[string]struct{},
	restored *[]string,
	skippedSet map[string]struct{},
	skipped *[]string,
) {
	srcPath := strings.TrimSpace(meta.SourcePath)
	if srcPath == "" {
		return
	}
	withinCodex, err := pathWithinRoot(codexRoot, srcPath)
	if err != nil {
		appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, "", "validate source path scope", err)
		return
	}
	if !withinCodex {
		appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, "", "source path is outside codex root", nil)
		return
	}

	archivedPath := strings.TrimSpace(meta.ArchivedPath)
	if archivedPath == "" {
		appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, "", "archived path is empty", nil)
		return
	}
	if !filepath.IsAbs(archivedPath) && strings.TrimSpace(manifest.ArchiveDir) != "" {
		archivedPath = filepath.Join(strings.TrimSpace(manifest.ArchiveDir), archivedPath)
	}
	if strings.TrimSpace(manifest.ArchiveDir) != "" {
		withinArchive, err := pathWithinRoot(manifest.ArchiveDir, archivedPath)
		if err != nil {
			appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "validate archived path scope", err)
			return
		}
		if !withinArchive {
			appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "archived path is outside archive root", nil)
			return
		}
	}

	info, err := os.Stat(archivedPath)
	if err != nil {
		appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "stat archived file", err)
		return
	}
	if info.IsDir() {
		appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "archived path is a directory", nil)
		return
	}

	expectedSHA256 := strings.TrimSpace(meta.SHA256)
	if expectedSHA256 != "" {
		actualArchiveSHA256, err := fileSHA256(archivedPath)
		if err != nil {
			appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "compute archived checksum", err)
			return
		}
		if !strings.EqualFold(expectedSHA256, actualArchiveSHA256) {
			appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "archived checksum mismatch", nil)
			return
		}
	}

	if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
		appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "ensure source parent dir", err)
		return
	}
	if err := copyFileOverwrite(archivedPath, srcPath); err != nil {
		appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "restore file to source path", err)
		return
	}
	if expectedSHA256 != "" {
		actualSourceSHA256, err := fileSHA256(srcPath)
		if err != nil {
			_ = os.Remove(srcPath)
			appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "compute restored source checksum", err)
			return
		}
		if !strings.EqualFold(expectedSHA256, actualSourceSHA256) {
			_ = os.Remove(srcPath)
			appendRestoreSkippedSource(threadID, skipped, skippedSet, srcPath, archivedPath, "restored source checksum mismatch", nil)
			return
		}
	}
	if _, ok := restoredSet[srcPath]; !ok {
		restoredSet[srcPath] = struct{}{}
		*restored = append(*restored, srcPath)
	}
}

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

// restoreThreadArchiveSources restores archived source files back to codex root.
func restoreThreadArchiveSources(
	threadID string,
	resolveThreadArchiveRoot func() (string, error),
	sanitizeArchiveNameStrict func(string) (string, error),
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	copyFileOverwrite func(srcPath, targetPath string) error,
	fileSHA256 func(path string) (string, error),
	findLatestManifestPath func(threadDir string) (string, error),
	readManifestFile func(manifestPath string) (threadArchiveManifest, error),
) ([]string, []string, error) {
	deps := buildThreadArchiveRestoreDeps(
		resolveThreadArchiveRoot,
		sanitizeArchiveNameStrict,
		resolveCodexRootDir,
		pathWithinRoot,
		copyFileOverwrite,
		fileSHA256,
		findLatestManifestPath,
		readManifestFile,
	)
	return restoreThreadArchiveSourcesWithDeps(threadID, deps)
}

func restoreThreadArchiveSourcesWithDeps(threadID string, deps threadArchiveRestoreDeps) ([]string, []string, error) {
	restored := []string{}
	skipped := []string{}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return restored, skipped, nil
	}
	deps = deps.withDefaults()
	if err := deps.validateRestore(); err != nil {
		return nil, nil, err
	}

	scope, ok, err := loadThreadArchiveManifestScope(id, "restoreThreadArchiveSources", deps)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return restored, skipped, nil
	}

	codexRoot, err := deps.resolveCodexRootDir()
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "resolve codex root")
	}

	manifest := scope.Manifest
	restoredSet := map[string]struct{}{}
	skippedSet := map[string]struct{}{}

	for _, meta := range manifest.Files {
		restoreThreadArchiveEntry(
			id,
			meta,
			manifest,
			codexRoot,
			deps.pathWithinRoot,
			deps.copyFileOverwrite,
			deps.fileSHA256,
			restoredSet,
			&restored,
			skippedSet,
			&skipped,
		)
	}
	sort.Strings(restored)
	sort.Strings(skipped)
	return restored, skipped, nil
}

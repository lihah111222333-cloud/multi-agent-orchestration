package codexadapter

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
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

type threadArtifactCandidate struct {
	Kind string
	Path string
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

const prefThreadArchivesChat = "threadArchives.chat"

func (a *Adapter) loadThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	store := a.store()
	if store == nil {
		return map[string]int64{}, nil
	}
	value, err := store.Get(ctx, prefThreadArchivesChat)
	if err != nil {
		return nil, err
	}
	return NormalizeThreadArchiveMap(value), nil
}

// PersistThreadArchivedState writes thread archive marker to preference storage.
func (a *Adapter) PersistThreadArchivedState(
	ctx context.Context,
	threadID string,
	archivedAt int64,
) error {
	store := a.store()
	if store == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	if archivedAt <= 0 {
		archivedAt = time.Now().UnixMilli()
	}
	return a.updateThreadArchiveMap(ctx, func(archivedMap map[string]int64) {
		archivedMap[id] = archivedAt
	})
}

// RemoveThreadArchivedState clears thread archive marker from preference storage.
func (a *Adapter) RemoveThreadArchivedState(
	ctx context.Context,
	threadID string,
) error {
	store := a.store()
	if store == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	return a.updateThreadArchiveMap(ctx, func(archivedMap map[string]int64) {
		delete(archivedMap, id)
	})
}

func (a *Adapter) updateThreadArchiveMap(ctx context.Context, update func(map[string]int64)) error {
	store := a.store()
	if store == nil {
		return nil
	}
	archivedMap, err := a.loadThreadArchiveMap(ctx)
	if err != nil {
		return err
	}
	if archivedMap == nil {
		archivedMap = map[string]int64{}
	}
	if update != nil {
		update(archivedMap)
	}
	return store.Set(ctx, prefThreadArchivesChat, archivedMap)
}

// inspectThreadArchiveForRestore verifies archived files before restore.
func inspectThreadArchiveForRestore(
	threadID string,
	resolveThreadArchiveRoot func() (string, error),
	sanitizeArchiveNameStrict func(string) (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	fileSHA256 func(path string) (string, error),
	findLatestManifestPath func(threadDir string) (string, error),
	readManifestFile func(manifestPath string) (threadArchiveManifest, error),
) (threadArchiveRestoreNotice, error) {
	notice := threadArchiveRestoreNotice{
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
	findLatestManifest := findLatestManifestPath
	if findLatestManifest == nil {
		findLatestManifest = findLatestThreadArchiveManifestPath
	}
	readManifest := readManifestFile
	if readManifest == nil {
		readManifest = readThreadArchiveManifest
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

func (a *Adapter) inspectThreadArchiveForRestore(threadID string) (threadArchiveRestoreNotice, error) {
	return inspectThreadArchiveForRestore(
		threadID,
		resolveThreadArchiveRootDir,
		SanitizeArchiveNameStrict,
		PathWithinRoot,
		fileSHA256,
		findLatestThreadArchiveManifestPath,
		readThreadArchiveManifest,
	)
}

func (a *Adapter) restoreThreadArchiveSources(threadID string) ([]string, []string, error) {
	return restoreThreadArchiveSources(
		threadID,
		resolveThreadArchiveRootDir,
		SanitizeArchiveNameStrict,
		resolveCodexRootDir,
		PathWithinRoot,
		copyFileOverwrite,
		fileSHA256,
		findLatestThreadArchiveManifestPath,
		readThreadArchiveManifest,
	)
}

// ThreadArchive validates archive eligibility, archives artifacts, and persists archive state.
func (a *Adapter) ThreadArchive(ctx context.Context, threadID string) (map[string]any, error) {
	id, err := requireThreadID("Server.threadArchive", threadID)
	if err != nil {
		return nil, err
	}
	if !a.threadExistsForArchive(ctx, id) {
		return nil, apperrors.Newf("Server.threadArchive", "thread %s not found", id)
	}
	manifest, err := a.ArchiveThreadArtifacts(ctx, id)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadArchive", "archive codex artifacts")
	}
	archivedAt := time.Now().UnixMilli()
	if err := a.PersistThreadArchivedState(ctx, id, archivedAt); err != nil {
		return nil, apperrors.Wrap(err, "Server.threadArchive", "persist archive state")
	}

	return map[string]any{
		"ok":            true,
		"threadId":      id,
		"archivedAt":    archivedAt,
		"codexThreadId": manifest.CodexThreadID,
		"archiveDir":    manifest.ArchiveDir,
		"rolloutPath":   manifest.RolloutPath,
		"files":         manifest.Files,
	}, nil
}

// ThreadUnarchive clears archive state and attempts source restore if archived.
func (a *Adapter) ThreadUnarchive(ctx context.Context, threadID string) (map[string]any, error) {
	id, err := requireThreadID("Server.threadUnarchive", threadID)
	if err != nil {
		return nil, err
	}
	archivedMap, err := a.loadThreadArchiveMap(ctx)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadUnarchive", "load archive state")
	}
	_, wasArchived := archivedMap[id]

	restoreNotice := threadArchiveRestoreNotice{
		Modified:      false,
		ManifestPath:  "",
		ModifiedFiles: []string{},
	}
	restoredFiles := []string{}
	skippedRestoreFiles := []string{}
	if wasArchived {
		notice, err := a.inspectThreadArchiveForRestore(id)
		if err != nil {
			logger.Error("thread/unarchive: inspect archive integrity failed",
				logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else {
			restoreNotice = notice
		}
		restored, skipped, err := a.restoreThreadArchiveSources(id)
		if err != nil {
			logger.Error("thread/unarchive: restore archived codex artifacts failed",
				logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else {
			restoredFiles = restored
			skippedRestoreFiles = skipped
		}
	}
	if err := a.RemoveThreadArchivedState(ctx, id); err != nil {
		return nil, apperrors.Wrap(err, "Server.threadUnarchive", "persist archive state")
	}
	result := map[string]any{
		"ok":       true,
		"threadId": id,
	}
	if len(restoredFiles) > 0 {
		result["restoredFiles"] = restoredFiles
	}
	if len(skippedRestoreFiles) > 0 {
		result["restoreSkippedFiles"] = skippedRestoreFiles
	}
	if restoreNotice.Modified {
		result["archiveModified"] = true
		result["manifestPath"] = restoreNotice.ManifestPath
		result["modifiedFiles"] = restoreNotice.ModifiedFiles
	}
	return result, nil
}

func (a *Adapter) threadExistsForArchive(ctx context.Context, threadID string) bool {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return false
	}
	if manager := a.manager(); manager != nil && manager.Get(id) != nil {
		return true
	}
	if a.threadExistsInRuntime(id) {
		return true
	}
	return a.ThreadExistsInHistory(ctx, id)
}

func (a *Adapter) bindRolloutPath(ctx context.Context, agentID, codexThreadID, rolloutPath string) {
	if strings.TrimSpace(codexThreadID) == "" || strings.TrimSpace(rolloutPath) == "" {
		return
	}
	bindingStore := a.bindingStore()
	if bindingStore == nil {
		return
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := bindingStore.Bind(dbCtx, agentID, codexThreadID, rolloutPath); err != nil {
		logger.Warn("thread/archive: persist rollout path failed",
			logger.FieldThreadID, agentID,
			"codex_thread_id", codexThreadID,
			"rollout_path", rolloutPath,
			logger.FieldError, err,
		)
	}
}

func (a *Adapter) pruneArchivedCodexSourceFiles(threadID string, files []threadArchiveFile, archiveDir string) {
	pruneArchivedCodexSourceFiles(
		threadID,
		files,
		archiveDir,
		resolveCodexRootDir,
		PathWithinRoot,
		fileSHA256,
		removeEmptyCodexParentDirs,
	)
}

// ArchiveThreadArtifacts archives codex thread related files.
func (a *Adapter) ArchiveThreadArtifacts(ctx context.Context, threadID string) (threadArchiveManifest, error) {
	id := strings.TrimSpace(threadID)
	manifest := threadArchiveManifest{
		ThreadID:   id,
		ArchivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Files:      []threadArchiveFile{},
	}
	rootDir, err := resolveThreadArchiveRootDir()
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive root")
	}
	archiveDir, err := resolveThreadArchiveSnapshotDir(rootDir, id, manifest.ArchivedAt)
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive dir")
	}
	manifest.ArchiveDir = archiveDir
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "ensure archive dir")
	}

	codexThreadID, rolloutPath := a.ResolveRolloutHistorySource(ctx, id, normalizeCodexThreadID)
	manifest.CodexThreadID = normalizeCodexThreadID(codexThreadID)
	candidates := collectThreadArtifactCandidates(manifest.CodexThreadID, rolloutPath)

	for _, candidate := range candidates {
		srcPath := strings.TrimSpace(candidate.Path)
		if srcPath == "" {
			continue
		}
		info, err := os.Stat(srcPath)
		if err != nil || info.IsDir() {
			continue
		}
		targetPath, err := nextArchiveFilePath(archiveDir, filepath.Base(srcPath))
		if err != nil {
			return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive target")
		}
		if err := copyFile(srcPath, targetPath); err != nil {
			logger.Error("thread/archive: copy artifact failed",
				logger.FieldThreadID, id,
				"source_path", srcPath,
				"target_path", targetPath,
				logger.FieldError, err,
			)
			continue
		}
		fileMeta := threadArchiveFile{
			Kind:         candidate.Kind,
			SourcePath:   srcPath,
			ArchivedPath: targetPath,
			SizeBytes:    info.Size(),
		}
		checksum, err := fileSHA256(targetPath)
		if err != nil {
			return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "compute archived file checksum")
		}
		fileMeta.SHA256 = checksum
		manifest.Files = append(manifest.Files, fileMeta)
		if manifest.RolloutPath == "" && candidate.Kind == "rollout" {
			manifest.RolloutPath = targetPath
		}
	}
	sort.SliceStable(manifest.Files, func(i, j int) bool {
		return manifest.Files[i].ArchivedPath < manifest.Files[j].ArchivedPath
	})

	if err := writeThreadArchiveManifest(manifest); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "write manifest")
	}
	a.bindRolloutPath(ctx, id, manifest.CodexThreadID, manifest.RolloutPath)
	a.pruneArchivedCodexSourceFiles(id, manifest.Files, manifest.ArchiveDir)
	return manifest, nil
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
	findLatestManifest := findLatestManifestPath
	if findLatestManifest == nil {
		findLatestManifest = findLatestThreadArchiveManifestPath
	}
	readManifest := readManifestFile
	if readManifest == nil {
		readManifest = readThreadArchiveManifest
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
		restoreThreadArchiveEntry(
			id,
			meta,
			manifest,
			codexRoot,
			pathWithinRoot,
			copyFileOverwrite,
			fileSHA256,
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

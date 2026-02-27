package codexadapter

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	archiveconsumer "github.com/multi-agent/go-agent-v2/pkg/codexsdk/consumer/archive"
	archivesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/archive"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func loadThreadArchiveMapFromStoreLogic(a *Adapter, ctx context.Context) (map[string]int64, error) {
	archivedMap := map[string]int64{}
	store := a.store()
	if store != nil {
		value, err := store.Get(ctx, prefThreadArchivesChat)
		if err != nil {
			return nil, err
		}
		archivedMap = archivesvc.NormalizeThreadArchiveMap(value)
	}
	return archivedMap, nil
}

func loadThreadArchiveMapLogic(a *Adapter, ctx context.Context) (map[string]int64, error) {
	archivedMap, err := loadThreadArchiveMapFromStoreLogic(a, ctx)
	if err != nil {
		return nil, err
	}
	fromDisk, err := archiveconsumer.LoadThreadArchiveMapFromDisk()
	if err != nil {
		logger.Warn("thread/archive: scan archive root failed", logger.FieldError, err)
		return archivedMap, nil
	}
	return archivesvc.MergeThreadArchiveMaps(archivedMap, fromDisk), nil
}

func persistThreadArchivedStateLogic(a *Adapter, ctx context.Context, threadID string, archivedAt int64) error {
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
	return updateThreadArchiveMapLogic(a, ctx, func(archivedMap map[string]int64) {
		archivedMap[id] = archivedAt
	})
}

func removeThreadArchivedStateLogic(a *Adapter, ctx context.Context, threadID string) error {
	store := a.store()
	if store == nil {
		return nil
	}
	id := strings.TrimSpace(threadID)
	if id == "" {
		return nil
	}
	return updateThreadArchiveMapLogic(a, ctx, func(archivedMap map[string]int64) {
		delete(archivedMap, id)
	})
}

func updateThreadArchiveMapLogic(a *Adapter, ctx context.Context, update func(map[string]int64)) error {
	store := a.store()
	if store == nil {
		return nil
	}
	archivedMap, err := loadThreadArchiveMapFromStoreLogic(a, ctx)
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

func threadArchiveLogic(a *Adapter, ctx context.Context, threadID string) (map[string]any, error) {
	id, err := requireThreadID("Server.threadArchive", threadID)
	if err != nil {
		return nil, err
	}
	if !threadExistsForArchiveLogic(a, ctx, id) {
		return nil, apperrors.Newf("Server.threadArchive", "thread %s not found", id)
	}
	manifest, err := archiveThreadArtifactsLogic(a, ctx, id)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadArchive", "archive codex artifacts")
	}
	archivedAt := time.Now().UnixMilli()
	if err := persistThreadArchivedStateLogic(a, ctx, id, archivedAt); err != nil {
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

func threadUnarchiveLogic(a *Adapter, ctx context.Context, threadID string) (map[string]any, error) {
	id, err := requireThreadID("Server.threadUnarchive", threadID)
	if err != nil {
		return nil, err
	}
	archivedMap, err := loadThreadArchiveMapLogic(a, ctx)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadUnarchive", "load archive state")
	}
	_, wasArchived := archivedMap[id]

	restoreNotice := archivesvc.ThreadArchiveRestoreNotice{
		Modified:      false,
		ManifestPath:  "",
		ModifiedFiles: []string{},
	}
	restoredFiles := []string{}
	skippedRestoreFiles := []string{}
	if wasArchived {
		notice, err := inspectArchivedRestoreNoticeLogic(id)
		if err != nil {
			logger.Error("thread/unarchive: inspect archive integrity failed",
				logger.FieldThreadID, id,
				logger.FieldError, err,
			)
		} else {
			restoreNotice = notice
		}
		restored, skipped, err := restoreThreadArchiveSourcesLogic(id)
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
	if err := removeThreadArchivedStateLogic(a, ctx, id); err != nil {
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

func threadExistsForArchiveLogic(a *Adapter, ctx context.Context, threadID string) bool {
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

func bindRolloutPathLogic(a *Adapter, ctx context.Context, agentID, codexThreadID, rolloutPath string) {
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

func archiveThreadArtifactsLogic(a *Adapter, ctx context.Context, threadID string) (archivesvc.ThreadArchiveManifest, error) {
	id := strings.TrimSpace(threadID)
	manifest := archivesvc.ThreadArchiveManifest{
		ThreadID:   id,
		ArchivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Files:      []archivesvc.ThreadArchiveFile{},
	}
	rootDir, err := archiveconsumer.ResolveThreadArchiveRootDir()
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive root")
	}
	archiveDir, err := archiveconsumer.ResolveThreadArchiveSnapshotDir(rootDir, id, manifest.ArchivedAt)
	if err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive dir")
	}
	manifest.ArchiveDir = archiveDir
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "ensure archive dir")
	}

	codexThreadID, rolloutPath := a.ResolveRolloutHistorySource(ctx, id, normalizeCodexThreadID)
	manifest.CodexThreadID = normalizeCodexThreadID(codexThreadID)
	candidates := archiveconsumer.CollectThreadArtifactCandidates(manifest.CodexThreadID, rolloutPath)

	for _, candidate := range candidates {
		srcPath := strings.TrimSpace(candidate.Path)
		if srcPath == "" {
			continue
		}
		sourceState, err := archiveconsumer.FileState(srcPath)
		if err != nil || !sourceState.Exists || sourceState.IsDir {
			continue
		}
		targetPath, err := archiveconsumer.NextArchiveFilePath(archiveDir, srcPath)
		if err != nil {
			return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive target")
		}
		if err := archiveconsumer.CopyFile(srcPath, targetPath); err != nil {
			logger.Error("thread/archive: copy artifact failed",
				logger.FieldThreadID, id,
				"source_path", srcPath,
				"target_path", targetPath,
				logger.FieldError, err,
			)
			continue
		}
		fileMeta := archivesvc.ThreadArchiveFile{
			Kind:         candidate.Kind,
			SourcePath:   srcPath,
			ArchivedPath: targetPath,
			SizeBytes:    sourceState.SizeBytes,
		}
		checksum, err := archiveconsumer.FileSHA256(targetPath)
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

	if err := archiveconsumer.WriteThreadArchiveManifest(manifest); err != nil {
		return manifest, apperrors.Wrap(err, "Server.archiveThreadArtifacts", "write manifest")
	}
	bindRolloutPathLogic(a, ctx, id, manifest.CodexThreadID, manifest.RolloutPath)
	pruneArchivedSourceFilesLogic(id, manifest.Files, manifest.ArchiveDir)
	return manifest, nil
}

func inspectArchivedRestoreNoticeLogic(threadID string) (archivesvc.ThreadArchiveRestoreNotice, error) {
	deps := archivesvc.BuildThreadArchiveRestoreDeps(
		archiveconsumer.ResolveThreadArchiveRootDir,
		archivesvc.SanitizeArchiveNameStrict,
		nil,
		archivesvc.PathWithinRoot,
		nil,
		archiveconsumer.FileSHA256,
		archiveconsumer.FindLatestThreadArchiveManifestPath,
		archiveconsumer.ReadThreadArchiveManifest,
		archiveconsumer.FileState,
		nil,
	)
	return archivesvc.InspectThreadArchiveForRestore(threadID, deps)
}

func restoreThreadArchiveSourcesLogic(threadID string) ([]string, []string, error) {
	return archivesvc.RestoreThreadArchiveSources(
		threadID,
		archiveconsumer.ResolveThreadArchiveRootDir,
		archivesvc.SanitizeArchiveNameStrict,
		archiveconsumer.ResolveCodexRootDir,
		archivesvc.PathWithinRoot,
		archiveconsumer.CopyFileOverwrite,
		archiveconsumer.FileSHA256,
		archiveconsumer.FindLatestThreadArchiveManifestPath,
		archiveconsumer.ReadThreadArchiveManifest,
		archiveconsumer.FileState,
		archiveconsumer.RemoveFile,
	)
}

func pruneArchivedSourceFilesLogic(threadID string, files []archivesvc.ThreadArchiveFile, archiveDir string) {
	archivesvc.PruneArchivedCodexSourceFiles(
		threadID,
		files,
		archiveDir,
		archiveconsumer.ResolveCodexRootDir,
		archivesvc.PathWithinRoot,
		archiveconsumer.FileSHA256,
		archiveconsumer.FileState,
		archiveconsumer.RemoveFile,
		archiveconsumer.RemoveEmptyCodexParentDirs,
	)
}

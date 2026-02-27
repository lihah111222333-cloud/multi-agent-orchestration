package archive

import (
	"context"
	"sort"
	"strings"
	"time"

	archivesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/archive"
	appErrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type ThreadArchiveDeps struct {
	ThreadExists                func(context.Context, string) bool
	LoadArchiveMap              func(context.Context) (map[string]int64, error)
	SaveArchiveMap              func(context.Context, map[string]int64) error
	ResolveRolloutHistorySource func(context.Context, string) (string, string)
	BindRolloutPath             func(context.Context, string, string, string)
}

func normalizeArchiveDeps(deps ThreadArchiveDeps) ThreadArchiveDeps {
	if deps.LoadArchiveMap == nil {
		deps.LoadArchiveMap = func(context.Context) (map[string]int64, error) { return map[string]int64{}, nil }
	}
	if deps.SaveArchiveMap == nil {
		deps.SaveArchiveMap = func(context.Context, map[string]int64) error { return nil }
	}
	if deps.ResolveRolloutHistorySource == nil {
		deps.ResolveRolloutHistorySource = func(context.Context, string) (string, string) { return "", "" }
	}
	if deps.BindRolloutPath == nil {
		deps.BindRolloutPath = func(context.Context, string, string, string) {}
	}
	return deps
}

func requireThreadID(caller, threadID string) (string, error) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", appErrors.New(caller, "threadId is required")
	}
	return id, nil
}

func loadArchiveStateMap(ctx context.Context, deps ThreadArchiveDeps, caller string) (map[string]int64, error) {
	archivedMap, err := deps.LoadArchiveMap(ctx)
	if err != nil {
		return nil, appErrors.Wrap(err, caller, "load archive state")
	}
	if archivedMap == nil {
		archivedMap = map[string]int64{}
	}
	return archivedMap, nil
}

func buildThreadArchiveResult(threadID string, archivedAt int64, manifest ThreadArchiveManifest) map[string]any {
	return map[string]any{
		"ok":            true,
		"threadId":      threadID,
		"archivedAt":    archivedAt,
		"codexThreadId": manifest.CodexThreadID,
		"archiveDir":    manifest.ArchiveDir,
		"rolloutPath":   manifest.RolloutPath,
		"files":         manifest.Files,
	}
}

func ThreadArchive(ctx context.Context, threadID string, deps ThreadArchiveDeps, nowUnixMilli func() int64) (map[string]any, error) {
	deps = normalizeArchiveDeps(deps)
	id, err := requireThreadID("Server.threadArchive", threadID)
	if err != nil {
		return nil, err
	}
	if deps.ThreadExists != nil && !deps.ThreadExists(ctx, id) {
		return nil, appErrors.Newf("Server.threadArchive", "thread %s not found", id)
	}
	manifest, err := ArchiveThreadArtifacts(ctx, id, deps.ResolveRolloutHistorySource, deps.BindRolloutPath)
	if err != nil {
		return nil, appErrors.Wrap(err, "Server.threadArchive", "archive codex artifacts")
	}
	if nowUnixMilli == nil {
		nowUnixMilli = func() int64 { return time.Now().UnixMilli() }
	}
	archivedAt := nowUnixMilli()
	archivedMap, err := loadArchiveStateMap(ctx, deps, "Server.threadArchive")
	if err != nil {
		return nil, err
	}
	archivedMap[id] = archivedAt
	if err := deps.SaveArchiveMap(ctx, archivedMap); err != nil {
		return nil, appErrors.Wrap(err, "Server.threadArchive", "persist archive state")
	}
	return buildThreadArchiveResult(id, archivedAt, manifest), nil
}

func ThreadUnarchive(ctx context.Context, threadID string, deps ThreadArchiveDeps) (map[string]any, error) {
	deps = normalizeArchiveDeps(deps)
	id, err := requireThreadID("Server.threadUnarchive", threadID)
	if err != nil {
		return nil, err
	}
	archivedMap, err := loadArchiveStateMap(ctx, deps, "Server.threadUnarchive")
	if err != nil {
		return nil, err
	}
	_, wasArchived := archivedMap[id]

	restoreNotice := ThreadArchiveRestoreNotice{}
	var restoredFiles, skippedRestoreFiles []string
	if wasArchived {
		notice, inspectErr := archivesvc.InspectThreadArchiveForRestore(id, archivesvc.BuildThreadArchiveRestoreDeps(ResolveThreadArchiveRootDir, SanitizeArchiveNameStrict, nil, PathWithinRoot, nil, FileSHA256, FindLatestThreadArchiveManifestPath, ReadThreadArchiveManifest, FileState, nil))
		if inspectErr != nil {
			logger.Error("thread/unarchive: inspect archive integrity failed", logger.FieldThreadID, id, logger.FieldError, inspectErr)
		} else {
			restoreNotice = notice
		}
		restored, skipped, restoreErr := archivesvc.RestoreThreadArchiveSources(id, ResolveThreadArchiveRootDir, SanitizeArchiveNameStrict, ResolveCodexRootDir, PathWithinRoot, CopyFileOverwrite, FileSHA256, FindLatestThreadArchiveManifestPath, ReadThreadArchiveManifest, FileState, RemoveFile)
		if restoreErr != nil {
			logger.Error("thread/unarchive: restore archived codex artifacts failed", logger.FieldThreadID, id, logger.FieldError, restoreErr)
		} else {
			restoredFiles = restored
			skippedRestoreFiles = skipped
		}
	}
	delete(archivedMap, id)
	if err := deps.SaveArchiveMap(ctx, archivedMap); err != nil {
		return nil, appErrors.Wrap(err, "Server.threadUnarchive", "persist archive state")
	}

	result := map[string]any{"ok": true, "threadId": id}
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

func ArchiveThreadArtifacts(ctx context.Context, threadID string, resolveRolloutHistorySource func(context.Context, string) (string, string), bindRolloutPath func(context.Context, string, string, string)) (ThreadArchiveManifest, error) {
	id := strings.TrimSpace(threadID)
	manifest := ThreadArchiveManifest{ThreadID: id, ArchivedAt: time.Now().UTC().Format(time.RFC3339Nano), Files: []ThreadArchiveFile{}}
	rootDir, err := ResolveThreadArchiveRootDir()
	if err != nil {
		return manifest, appErrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive root")
	}
	archiveDir, err := ResolveThreadArchiveSnapshotDir(rootDir, id, manifest.ArchivedAt)
	if err != nil {
		return manifest, appErrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive dir")
	}
	manifest.ArchiveDir = archiveDir
	if err := EnsureDir(archiveDir); err != nil {
		return manifest, appErrors.Wrap(err, "Server.archiveThreadArtifacts", "ensure archive dir")
	}
	if resolveRolloutHistorySource != nil {
		codexThreadID, rolloutPath := resolveRolloutHistorySource(ctx, id)
		manifest.CodexThreadID = codexThreadID
		candidates := CollectThreadArtifactCandidates(manifest.CodexThreadID, rolloutPath)
		for _, candidate := range candidates {
			srcPath := strings.TrimSpace(candidate.Path)
			if srcPath == "" {
				continue
			}
			sourceState, err := FileState(srcPath)
			if err != nil || !sourceState.Exists || sourceState.IsDir {
				continue
			}
			targetPath, err := NextArchiveFilePath(archiveDir, srcPath)
			if err != nil {
				return manifest, appErrors.Wrap(err, "Server.archiveThreadArtifacts", "resolve archive target")
			}
			if err := CopyFile(srcPath, targetPath); err != nil {
				logger.Error("thread/archive: copy artifact failed", logger.FieldThreadID, id, "source_path", srcPath, "target_path", targetPath, logger.FieldError, err)
				continue
			}
			fileMeta := ThreadArchiveFile{Kind: candidate.Kind, SourcePath: srcPath, ArchivedPath: targetPath, SizeBytes: sourceState.SizeBytes}
			checksum, err := FileSHA256(targetPath)
			if err != nil {
				return manifest, appErrors.Wrap(err, "Server.archiveThreadArtifacts", "compute archived file checksum")
			}
			fileMeta.SHA256 = checksum
			manifest.Files = append(manifest.Files, fileMeta)
			if manifest.RolloutPath == "" && candidate.Kind == "rollout" {
				manifest.RolloutPath = targetPath
			}
		}
	}
	sort.SliceStable(manifest.Files, func(i, j int) bool { return manifest.Files[i].ArchivedPath < manifest.Files[j].ArchivedPath })
	if err := WriteThreadArchiveManifest(manifest); err != nil {
		return manifest, appErrors.Wrap(err, "Server.archiveThreadArtifacts", "write manifest")
	}
	if bindRolloutPath != nil {
		bindRolloutPath(ctx, id, manifest.CodexThreadID, manifest.RolloutPath)
	}
	archivesvc.PruneArchivedCodexSourceFiles(id, manifest.Files, manifest.ArchiveDir, ResolveCodexRootDir, PathWithinRoot, FileSHA256, FileState, RemoveFile, RemoveEmptyCodexParentDirs)
	return manifest, nil
}

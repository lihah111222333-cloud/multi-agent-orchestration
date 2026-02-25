package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ThreadNameSet sets codex thread name and persists alias.
func (a *Adapter) ThreadNameSet(ctx context.Context, threadID, name string) (map[string]any, error) {
	var getProcessFn func(string) *runner.AgentProcess
	var existsInRuntimeFn func(string) bool
	var setRuntimeThreadNameFn func(string, string)
	if a != nil && a.ctx != nil {
		if mgr := a.ctx.Manager(); mgr != nil {
			getProcessFn = mgr.Get
		}
		if runtime := a.ctx.UIRuntime(); runtime != nil {
			existsInRuntimeFn = func(id string) bool {
				for _, item := range runtime.SnapshotLight().Threads {
					if strings.TrimSpace(item.ID) == strings.TrimSpace(id) {
						return true
					}
				}
				return false
			}
			setRuntimeThreadNameFn = runtime.SetThreadName
		}
	}

	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadNameSet", "threadId is required")
	}
	requestedName := strings.TrimSpace(name)
	persistedAlias := requestedName
	if persistedAlias == threadID {
		persistedAlias = ""
	}
	renameTarget := requestedName
	if renameTarget == "" {
		renameTarget = threadID
	}

	var proc *runner.AgentProcess
	if getProcessFn != nil {
		proc = getProcessFn(threadID)
	}
	existsInRuntime := false
	if existsInRuntimeFn != nil {
		existsInRuntime = existsInRuntimeFn(threadID)
	}
	hasHistory := a.threadExistsInHistory(ctx, threadID)
	if proc == nil && !existsInRuntime && !hasHistory {
		return nil, apperrors.Newf("Server.threadNameSet", "thread %s not found", threadID)
	}

	if proc != nil && renameTarget != "" {
		if err := a.SendCommand(proc, "/rename", renameTarget); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "send rename command")
		}
	}

	if setRuntimeThreadNameFn != nil {
		setRuntimeThreadNameFn(threadID, persistedAlias)
	}
	if err := a.persistThreadAlias(ctx, threadID, persistedAlias); err != nil {
		logger.Warn("thread/name/set: persist alias failed",
			logger.FieldThreadID, threadID,
			logger.FieldError, err,
		)
		return nil, apperrors.Wrap(err, "Server.threadNameSet", "persist thread alias")
	}
	return map[string]any{}, nil
}

// ThreadUnarchive clears archive state and attempts source restore if archived.
func (a *Adapter) ThreadUnarchive(ctx context.Context, threadID string) (map[string]any, error) {
	inspect := func(id string) (ThreadArchiveRestoreNotice, error) {
		return InspectThreadArchiveForRestore(InspectThreadArchiveForRestoreOptions{
			ThreadID:                            id,
			ResolveThreadArchiveRoot:            resolveThreadArchiveRootDir,
			SanitizeArchiveNameStrict:           SanitizeArchiveNameStrict,
			PathWithinRoot:                      PathWithinRoot,
			FileSHA256:                          fileSHA256,
			FindLatestThreadArchiveManifestPath: FindLatestThreadArchiveManifestPath,
			ReadThreadArchiveManifest:           ReadThreadArchiveManifest,
		})
	}
	restore := func(id string) ([]string, []string, error) {
		return RestoreThreadArchiveSources(RestoreThreadArchiveSourcesOptions{
			ThreadID:                            id,
			ResolveThreadArchiveRoot:            resolveThreadArchiveRootDir,
			SanitizeArchiveNameStrict:           SanitizeArchiveNameStrict,
			ResolveCodexRootDir:                 resolveCodexRootDir,
			PathWithinRoot:                      PathWithinRoot,
			CopyFileOverwrite:                   copyFileOverwrite,
			FileSHA256:                          fileSHA256,
			FindLatestThreadArchiveManifestPath: FindLatestThreadArchiveManifestPath,
			ReadThreadArchiveManifest:           ReadThreadArchiveManifest,
		})
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadUnarchive", "threadId is required")
	}
	archivedMap, err := a.loadThreadArchiveMap(ctx)
	if err != nil {
		return nil, apperrors.Wrap(err, "Server.threadUnarchive", "load archive state")
	}
	_, wasArchived := archivedMap[threadID]

	restoreNotice := ThreadArchiveRestoreNotice{
		Modified:      false,
		ManifestPath:  "",
		ModifiedFiles: []string{},
	}
	restoredFiles := []string{}
	skippedRestoreFiles := []string{}
	if wasArchived {
		notice, err := inspect(threadID)
		if err != nil {
			logger.Error("thread/unarchive: inspect archive integrity failed",
				logger.FieldThreadID, threadID,
				logger.FieldError, err,
			)
		} else {
			restoreNotice = notice
		}
		restored, skipped, err := restore(threadID)
		if err != nil {
			logger.Error("thread/unarchive: restore archived codex artifacts failed",
				logger.FieldThreadID, threadID,
				logger.FieldError, err,
			)
		} else {
			restoredFiles = restored
			skippedRestoreFiles = skipped
		}
	}
	if err := a.RemoveThreadArchivedState(ctx, threadID, a.loadThreadArchiveMap); err != nil {
		return nil, apperrors.Wrap(err, "Server.threadUnarchive", "persist archive state")
	}
	result := map[string]any{
		"ok":       true,
		"threadId": threadID,
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

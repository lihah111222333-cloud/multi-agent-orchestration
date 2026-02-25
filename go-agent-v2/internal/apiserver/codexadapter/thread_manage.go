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
	if a != nil && a.ctx != nil && a.ctx.Manager != nil {
		proc = a.ctx.Manager.Get(threadID)
	}
	existsInRuntime := a.threadExistsInRuntime(threadID)
	hasHistory := a.ThreadExistsInHistory(ctx, threadID)
	if proc == nil && !existsInRuntime && !hasHistory {
		return nil, apperrors.Newf("Server.threadNameSet", "thread %s not found", threadID)
	}

	if proc != nil && renameTarget != "" {
		if err := a.SendCommand(proc, "/rename", renameTarget); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "send rename command")
		}
	}

	if a != nil && a.ctx != nil && a.ctx.UIRuntime != nil {
		a.ctx.UIRuntime.SetThreadName(threadID, persistedAlias)
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
		notice, err := a.inspectThreadArchiveForRestore(threadID)
		if err != nil {
			logger.Error("thread/unarchive: inspect archive integrity failed",
				logger.FieldThreadID, threadID,
				logger.FieldError, err,
			)
		} else {
			restoreNotice = notice
		}
		restored, skipped, err := a.restoreThreadArchiveSources(threadID)
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
	if err := a.RemoveThreadArchivedState(ctx, threadID); err != nil {
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

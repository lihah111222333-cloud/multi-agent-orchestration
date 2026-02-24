package codexadapter

import (
	"context"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/runner"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ThreadNameSetOptions carries dependencies for thread rename/alias update.
type ThreadNameSetOptions struct {
	ThreadID string
	Name     string

	GetProcess            func(threadID string) *runner.AgentProcess
	ExistsInRuntime       func(threadID string) bool
	ThreadExistsInHistory func(context.Context, string) bool
	SendCommand           func(*runner.AgentProcess, string, string) error
	SetRuntimeThreadName  func(threadID, alias string)
	PersistThreadAlias    func(ctx context.Context, threadID, alias string) error
}

// ThreadNameSet sets codex thread name and persists alias.
func (a *Adapter) ThreadNameSet(ctx context.Context, opt ThreadNameSetOptions) (map[string]any, error) {
	threadID := strings.TrimSpace(opt.ThreadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadNameSet", "threadId is required")
	}
	requestedName := strings.TrimSpace(opt.Name)
	persistedAlias := requestedName
	if persistedAlias == threadID {
		persistedAlias = ""
	}
	renameTarget := requestedName
	if renameTarget == "" {
		renameTarget = threadID
	}

	var proc *runner.AgentProcess
	if opt.GetProcess != nil {
		proc = opt.GetProcess(threadID)
	}
	existsInRuntime := false
	if opt.ExistsInRuntime != nil {
		existsInRuntime = opt.ExistsInRuntime(threadID)
	}
	hasHistory := false
	if opt.ThreadExistsInHistory != nil {
		hasHistory = opt.ThreadExistsInHistory(ctx, threadID)
	}
	if proc == nil && !existsInRuntime && !hasHistory {
		return nil, apperrors.Newf("Server.threadNameSet", "thread %s not found", threadID)
	}

	if proc != nil && renameTarget != "" && opt.SendCommand != nil {
		if err := opt.SendCommand(proc, "/rename", renameTarget); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "send rename command")
		}
	}

	if opt.SetRuntimeThreadName != nil {
		opt.SetRuntimeThreadName(threadID, persistedAlias)
	}
	if opt.PersistThreadAlias != nil {
		if err := opt.PersistThreadAlias(ctx, threadID, persistedAlias); err != nil {
			logger.Warn("thread/name/set: persist alias failed",
				logger.FieldThreadID, threadID,
				logger.FieldError, err,
			)
			return nil, apperrors.Wrap(err, "Server.threadNameSet", "persist thread alias")
		}
	}
	return map[string]any{}, nil
}

// ThreadUnarchiveOptions carries dependencies for thread unarchive.
type ThreadUnarchiveOptions struct {
	ThreadID string

	LoadThreadArchiveMap      func(context.Context) (map[string]int64, error)
	InspectArchiveRestore     func(threadID string) (ThreadArchiveRestoreNotice, error)
	RestoreArchiveSources     func(threadID string) ([]string, []string, error)
	RemoveThreadArchivedState func(context.Context, string) error
}

// ThreadUnarchive clears archive state and attempts source restore if archived.
func (a *Adapter) ThreadUnarchive(ctx context.Context, opt ThreadUnarchiveOptions) (map[string]any, error) {
	threadID := strings.TrimSpace(opt.ThreadID)
	if threadID == "" {
		return nil, apperrors.New("Server.threadUnarchive", "threadId is required")
	}
	archivedMap := map[string]int64{}
	if opt.LoadThreadArchiveMap != nil {
		loaded, err := opt.LoadThreadArchiveMap(ctx)
		if err != nil {
			return nil, apperrors.Wrap(err, "Server.threadUnarchive", "load archive state")
		}
		archivedMap = loaded
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
		if opt.InspectArchiveRestore != nil {
			notice, err := opt.InspectArchiveRestore(threadID)
			if err != nil {
				logger.Error("thread/unarchive: inspect archive integrity failed",
					logger.FieldThreadID, threadID,
					logger.FieldError, err,
				)
			} else {
				restoreNotice = notice
			}
		}
		if opt.RestoreArchiveSources != nil {
			restored, skipped, err := opt.RestoreArchiveSources(threadID)
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
	}
	if opt.RemoveThreadArchivedState != nil {
		if err := opt.RemoveThreadArchivedState(ctx, threadID); err != nil {
			return nil, apperrors.Wrap(err, "Server.threadUnarchive", "persist archive state")
		}
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

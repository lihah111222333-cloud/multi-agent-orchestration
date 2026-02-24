// methods_thread_codex.go — thread 相关 codex 专属实现。
package apiserver

import (
	"context"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

type threadResumeParams struct {
	ThreadID string `json:"threadId"`
	Path     string `json:"path,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
	Model    string `json:"model,omitempty"`
}

type threadResumeResponse struct {
	Thread threadInfo `json:"thread"`
	Model  string     `json:"model"`
}

func (s *Server) threadResumeTyped(ctx context.Context, p threadResumeParams) (any, error) {
	result, err := s.codexAdapter.ThreadResume(ctx, codexadapter.ThreadResumeOptions{
		ThreadID:                     p.ThreadID,
		Path:                         p.Path,
		Cwd:                          p.Cwd,
		Model:                        p.Model,
		WithThread:                   s.withThread,
		ResolveCodexThreadCandidates: s.resolveCodexThreadCandidates,
		NormalizeCodexThreadID:       normalizeCodexThreadID,
	})
	if err != nil {
		return nil, err
	}
	return threadResumeResponse{
		Thread: threadInfo{ID: result.ThreadID, Status: result.Status},
		Model:  result.Model,
	}, nil
}

func (s *Server) threadArchiveTyped(ctx context.Context, p threadIDParams) (any, error) {
	return s.codexAdapter.ThreadArchive(ctx, codexadapter.ThreadArchiveOptions{
		ThreadID: p.ThreadID,
		ThreadExistsForArchive: func(checkCtx context.Context, threadID string) bool {
			return s.threadExistsForArchive(checkCtx, threadID)
		},
		ArchiveThreadArtifacts: s.archiveThreadArtifacts,
		PersistThreadArchiveFlag: func(saveCtx context.Context, threadID string, archivedAt int64) error {
			return s.persistThreadArchivedState(saveCtx, threadID, archivedAt)
		},
	})
}

func (s *Server) threadUnarchiveTyped(ctx context.Context, p threadIDParams) (any, error) {
	return s.codexAdapter.ThreadUnarchive(ctx, codexadapter.ThreadUnarchiveOptions{
		ThreadID:                  p.ThreadID,
		LoadThreadArchiveMap:      s.loadThreadArchiveMap,
		InspectArchiveRestore:     inspectThreadArchiveForRestore,
		RestoreArchiveSources:     restoreThreadArchiveSources,
		RemoveThreadArchivedState: s.removeThreadArchivedState,
	})
}

type threadNameSetParams struct {
	ThreadID string `json:"threadId"`
	Name     string `json:"name"`
}

func (s *Server) threadNameSetTyped(ctx context.Context, p threadNameSetParams) (any, error) {
	return s.codexAdapter.ThreadNameSet(ctx, codexadapter.ThreadNameSetOptions{
		ThreadID: p.ThreadID,
		Name:     p.Name,
		GetProcess: func(threadID string) *runner.AgentProcess {
			if s.mgr == nil {
				return nil
			}
			return s.mgr.Get(threadID)
		},
		ExistsInRuntime: func(threadID string) bool {
			if s.uiRuntime == nil {
				return false
			}
			return hasThread(s.uiRuntime.SnapshotLight().Threads, threadID)
		},
		ThreadExistsInHistory: s.threadExistsInHistory,
		SendCommand:           s.codexAdapter.SendCommand,
		SetRuntimeThreadName: func(threadID, alias string) {
			if s.uiRuntime != nil {
				s.uiRuntime.SetThreadName(threadID, alias)
			}
		},
		PersistThreadAlias: s.persistThreadAlias,
	})
}

type threadRollbackParams struct {
	ThreadID  string `json:"threadId"`
	TurnIndex int    `json:"turnIndex"`
}

func (s *Server) threadRollbackTyped(_ context.Context, p threadRollbackParams) (any, error) {
	return s.codexAdapter.ThreadRollback(codexadapter.ThreadRollbackOptions{
		ThreadID:   p.ThreadID,
		TurnIndex:  p.TurnIndex,
		WithThread: s.withThread,
	})
}

type threadMessagesParams struct {
	ThreadID string `json:"threadId"`
	Limit    int    `json:"limit,omitempty"`
	Before   int64  `json:"before,omitempty"` // cursor: id < before
}

func (s *Server) threadMessagesTyped(ctx context.Context, p threadMessagesParams) (any, error) {
	return s.codexAdapter.ThreadMessages(ctx, codexadapter.ThreadMessagesOptions{
		ThreadID:        p.ThreadID,
		Limit:           p.Limit,
		Before:          p.Before,
		LoadAllMessages: s.loadAllThreadMessagesFromCodexRollout,
		Paginate:        paginateRolloutMessages,
		HandleHydration: s.handleThreadMessagesHydration,
		Stats: func(threadID string) (int, int) {
			if s.uiRuntime == nil {
				return 0, 0
			}
			return len(s.uiRuntime.ThreadDiff(threadID)), len(s.uiRuntime.ThreadTimeline(threadID))
		},
	})
}

func (s *Server) handleThreadMessagesHydration(threadID string, all, page []threadHistoryMessage, before int64, _ int) {
	if s.uiRuntime == nil {
		return
	}
	codexadapter.HandleThreadMessagesHydration(codexadapter.HandleThreadMessagesHydrationOptions{
		ThreadID:                    threadID,
		All:                         all,
		Page:                        page,
		Before:                      before,
		CalculateHydrationLoadLimit: calculateHydrationLoadLimit,
		HydrateHistory:              s.uiRuntime.HydrateHistory,
		StreamRemainingHistory:      s.streamRemainingHistory,
		AsyncGo:                     util.SafeGo,
	})
}

func (s *Server) streamRemainingHistory(threadID string, all []threadHistoryMessage, firstPage []threadHistoryMessage, limit int) {
	if s.uiRuntime == nil {
		return
	}
	codexadapter.StreamRemainingHistory(codexadapter.StreamRemainingHistoryOptions{
		ThreadID:          threadID,
		All:               all,
		First:             firstPage,
		Limit:             limit,
		PageSize:          threadMessageHydrationPageSize,
		Paginate:          paginateRolloutMessages,
		AppendHistory:     s.uiRuntime.AppendHistory,
		ThreadDiffLen:     func(id string) int { return len(s.uiRuntime.ThreadDiff(id)) },
		ThreadTimelineLen: func(id string) int { return len(s.uiRuntime.ThreadTimeline(id)) },
		NotifyPage: func(id string, totalLoaded int, pages int) {
			s.Notify("thread/messages/page", map[string]any{
				"threadId":   id,
				"totalCount": totalLoaded,
				"pages":      pages,
			})
		},
	})
}

type threadHistoryMessage = codexadapter.ThreadHistoryMessage

func (s *Server) resolveRolloutHistorySource(ctx context.Context, threadID string) (codexThreadID string, rolloutPath string) {
	return s.codexAdapter.ResolveRolloutHistorySource(ctx, threadID, normalizeCodexThreadID)
}

func paginateRolloutMessages(all []threadHistoryMessage, limit int, before int64) []threadHistoryMessage {
	return codexadapter.PaginateRolloutMessages(all, limit, before)
}

func (s *Server) loadAllThreadMessagesFromCodexRollout(ctx context.Context, threadID string) ([]threadHistoryMessage, error) {
	return codexadapter.LoadAllThreadMessagesFromCodexRollout(ctx, codexadapter.LoadAllThreadMessagesFromCodexRolloutOptions{
		ThreadID:                    threadID,
		ResolveRolloutHistorySource: s.resolveRolloutHistorySource,
		NormalizeCodexThreadID:      normalizeCodexThreadID,
		ShowInjectedPromptInChat:    s.showInjectedPromptInChat(ctx),
	})
}

type threadArchiveFile = codexadapter.ThreadArchiveFile

type threadArchiveManifest = codexadapter.ThreadArchiveManifest

type threadArtifactCandidate = codexadapter.ThreadArtifactCandidate

func (s *Server) threadExistsForArchive(ctx context.Context, threadID string) bool {
	return s.codexAdapter.ThreadExistsForArchive(ctx, threadID, s.threadExistsInHistory)
}

func (s *Server) persistThreadArchivedState(ctx context.Context, threadID string, archivedAt int64) error {
	return s.codexAdapter.PersistThreadArchivedState(ctx, threadID, archivedAt, s.loadThreadArchiveMap)
}

func (s *Server) removeThreadArchivedState(ctx context.Context, threadID string) error {
	return s.codexAdapter.RemoveThreadArchivedState(ctx, threadID, s.loadThreadArchiveMap)
}

func (s *Server) archiveThreadArtifacts(ctx context.Context, threadID string) (threadArchiveManifest, error) {
	return s.codexAdapter.ArchiveThreadArtifacts(ctx, codexadapter.ArchiveThreadArtifactsOptions{
		ThreadID:                        threadID,
		ResolveThreadArchiveRootDir:     resolveThreadArchiveRootDir,
		ResolveThreadArchiveSnapshotDir: resolveThreadArchiveSnapshotDir,
		ResolveRolloutHistorySource:     s.resolveRolloutHistorySource,
		NormalizeCodexThreadID:          normalizeCodexThreadID,
		CollectThreadArtifactCandidates: collectThreadArtifactCandidates,
		NextArchiveFilePath:             nextArchiveFilePath,
		CopyFile:                        copyFile,
		FileSHA256:                      fileSHA256,
		WriteThreadArchiveManifest:      writeThreadArchiveManifest,
		BindRolloutPath: func(bindCtx context.Context, agentID, codexThreadID, rolloutPath string) error {
			if s.bindingStore == nil {
				return nil
			}
			dbCtx, cancel := context.WithTimeout(bindCtx, 5*time.Second)
			defer cancel()
			return s.bindingStore.Bind(dbCtx, agentID, codexThreadID, rolloutPath)
		},
		PruneArchivedCodexSourceFiles: pruneArchivedCodexSourceFiles,
	})
}

func collectThreadArtifactCandidates(codexThreadID string, rolloutPath string) []threadArtifactCandidate {
	return codexadapter.CollectThreadArtifactCandidates(codexThreadID, rolloutPath)
}

func pruneArchivedCodexSourceFiles(threadID string, files []threadArchiveFile, archiveDir string) {
	codexadapter.PruneArchivedCodexSourceFiles(codexadapter.PruneArchivedCodexSourceFilesOptions{
		ThreadID:                  threadID,
		Files:                     files,
		ArchiveDir:                archiveDir,
		ResolveCodexRootDir:       resolveCodexRootDir,
		PathWithinRoot:            pathWithinRoot,
		FileSHA256:                fileSHA256,
		RemoveEmptyCodexParentDir: removeEmptyCodexParentDirs,
	})
}

func restoreThreadArchiveSources(threadID string) ([]string, []string, error) {
	return codexadapter.RestoreThreadArchiveSources(codexadapter.RestoreThreadArchiveSourcesOptions{
		ThreadID:                            threadID,
		ResolveThreadArchiveRoot:            resolveThreadArchiveRootDir,
		SanitizeArchiveNameStrict:           sanitizeArchiveNameStrict,
		ResolveCodexRootDir:                 resolveCodexRootDir,
		PathWithinRoot:                      pathWithinRoot,
		CopyFileOverwrite:                   copyFileOverwrite,
		FileSHA256:                          fileSHA256,
		FindLatestThreadArchiveManifestPath: findLatestThreadArchiveManifestPath,
		ReadThreadArchiveManifest:           readThreadArchiveManifest,
	})
}

type threadArchiveRestoreNotice = codexadapter.ThreadArchiveRestoreNotice

func inspectThreadArchiveForRestore(threadID string) (threadArchiveRestoreNotice, error) {
	return codexadapter.InspectThreadArchiveForRestore(codexadapter.InspectThreadArchiveForRestoreOptions{
		ThreadID:                            threadID,
		ResolveThreadArchiveRoot:            resolveThreadArchiveRootDir,
		SanitizeArchiveNameStrict:           sanitizeArchiveNameStrict,
		PathWithinRoot:                      pathWithinRoot,
		FileSHA256:                          fileSHA256,
		FindLatestThreadArchiveManifestPath: findLatestThreadArchiveManifestPath,
		ReadThreadArchiveManifest:           readThreadArchiveManifest,
	})
}

func findLatestThreadArchiveManifestPath(threadDir string) (string, error) {
	return codexadapter.FindLatestThreadArchiveManifestPath(threadDir)
}

func readThreadArchiveManifest(manifestPath string) (threadArchiveManifest, error) {
	return codexadapter.ReadThreadArchiveManifest(manifestPath)
}

func writeThreadArchiveManifest(manifest threadArchiveManifest) error {
	return codexadapter.WriteThreadArchiveManifest(manifest)
}

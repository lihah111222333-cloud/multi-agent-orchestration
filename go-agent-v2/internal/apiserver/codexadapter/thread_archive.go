package codexadapter

import "context"

type threadArtifactCandidate struct {
	Kind string
	Path string
}

const prefThreadArchivesChat = "threadArchives.chat"

func (a *Adapter) loadThreadArchiveMapFromStore(ctx context.Context) (map[string]int64, error) {
	return loadThreadArchiveMapFromStoreLogic(a, ctx)
}

func (a *Adapter) loadThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	return loadThreadArchiveMapLogic(a, ctx)
}

// ThreadArchiveMap returns merged archived thread mapping from preference and archive dir.
func (a *Adapter) ThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	return loadThreadArchiveMapLogic(a, ctx)
}

// PersistThreadArchivedState writes thread archive marker to preference storage.
func (a *Adapter) PersistThreadArchivedState(
	ctx context.Context,
	threadID string,
	archivedAt int64,
) error {
	return persistThreadArchivedStateLogic(a, ctx, threadID, archivedAt)
}

// RemoveThreadArchivedState clears thread archive marker from preference storage.
func (a *Adapter) RemoveThreadArchivedState(
	ctx context.Context,
	threadID string,
) error {
	return removeThreadArchivedStateLogic(a, ctx, threadID)
}

func (a *Adapter) updateThreadArchiveMap(ctx context.Context, update func(map[string]int64)) error {
	return updateThreadArchiveMapLogic(a, ctx, update)
}

// ThreadArchive validates archive eligibility, archives artifacts, and persists archive state.
func (a *Adapter) ThreadArchive(ctx context.Context, threadID string) (map[string]any, error) {
	return threadArchiveLogic(a, ctx, threadID)
}

// ThreadUnarchive clears archive state and attempts source restore if archived.
func (a *Adapter) ThreadUnarchive(ctx context.Context, threadID string) (map[string]any, error) {
	return threadUnarchiveLogic(a, ctx, threadID)
}

func (a *Adapter) threadExistsForArchive(ctx context.Context, threadID string) bool {
	return threadExistsForArchiveLogic(a, ctx, threadID)
}

func (a *Adapter) bindRolloutPath(ctx context.Context, agentID, codexThreadID, rolloutPath string) {
	bindRolloutPathLogic(a, ctx, agentID, codexThreadID, rolloutPath)
}

// ArchiveThreadArtifacts archives codex thread related files.
func (a *Adapter) ArchiveThreadArtifacts(ctx context.Context, threadID string) (threadArchiveManifest, error) {
	return archiveThreadArtifactsLogic(a, ctx, threadID)
}

func (a *Adapter) inspectThreadArchiveForRestore(threadID string) (threadArchiveRestoreNotice, error) {
	return inspectThreadArchiveForRestoreLogic(threadID)
}

func (a *Adapter) restoreThreadArchiveSources(threadID string) ([]string, []string, error) {
	return restoreThreadArchiveSourcesLogic(threadID)
}

func (a *Adapter) pruneArchivedCodexSourceFiles(threadID string, files []threadArchiveFile, archiveDir string) {
	pruneArchivedCodexSourceFilesLogic(threadID, files, archiveDir)
}

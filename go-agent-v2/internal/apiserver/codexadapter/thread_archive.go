package codexadapter

func (a *Adapter) inspectThreadArchiveForRestore(threadID string) (ThreadArchiveRestoreNotice, error) {
	return InspectThreadArchiveForRestore(
		threadID,
		resolveThreadArchiveRootDir,
		SanitizeArchiveNameStrict,
		PathWithinRoot,
		fileSHA256,
		FindLatestThreadArchiveManifestPath,
		ReadThreadArchiveManifest,
	)
}

func (a *Adapter) restoreThreadArchiveSources(threadID string) ([]string, []string, error) {
	return RestoreThreadArchiveSources(
		threadID,
		resolveThreadArchiveRootDir,
		SanitizeArchiveNameStrict,
		resolveCodexRootDir,
		PathWithinRoot,
		copyFileOverwrite,
		fileSHA256,
		FindLatestThreadArchiveManifestPath,
		ReadThreadArchiveManifest,
	)
}

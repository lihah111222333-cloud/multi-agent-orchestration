package archive

import (
	"os"

	archivesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/archive"
)

type ThreadArchiveFile = archivesvc.ThreadArchiveFile
type ThreadArchiveManifest = archivesvc.ThreadArchiveManifest
type ThreadArchiveRestoreNotice = archivesvc.ThreadArchiveRestoreNotice
type ThreadArchiveRestoreDeps = archivesvc.ThreadArchiveRestoreDeps
type ThreadArtifactCandidate = archivesvc.ThreadArtifactCandidate
type ThreadArchiveFileState = archivesvc.ThreadArchiveFileState

var (
	NormalizeThreadArchiveMap      = archivesvc.NormalizeThreadArchiveMap
	SanitizeArchiveName            = archivesvc.SanitizeArchiveName
	SanitizeArchiveNameStrict      = archivesvc.SanitizeArchiveNameStrict
	PathWithinRoot                 = archivesvc.PathWithinRoot
	InferThreadArtifactKind        = archivesvc.InferThreadArtifactKind
	MergeThreadArchiveMaps         = archivesvc.MergeThreadArchiveMaps
	ParseArchiveTimestamp          = archivesvc.ParseArchiveTimestamp
	BuildThreadArchiveRestoreDeps  = archivesvc.BuildThreadArchiveRestoreDeps
	InspectThreadArchiveForRestore = archivesvc.InspectThreadArchiveForRestore
	RestoreThreadArchiveSources    = archivesvc.RestoreThreadArchiveSources
	PruneArchivedCodexSourceFiles  = archivesvc.PruneArchivedCodexSourceFiles
)

func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

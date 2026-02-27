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

func NormalizeThreadArchiveMap(value any) map[string]int64 {
	return archivesvc.NormalizeThreadArchiveMap(value)
}

func SanitizeArchiveName(raw string) string {
	return archivesvc.SanitizeArchiveName(raw)
}

func SanitizeArchiveNameStrict(raw string) (string, error) {
	return archivesvc.SanitizeArchiveNameStrict(raw)
}

func PathWithinRoot(root string, path string) (bool, error) {
	return archivesvc.PathWithinRoot(root, path)
}

func InferThreadArtifactKind(filename string) string {
	return archivesvc.InferThreadArtifactKind(filename)
}

func MergeThreadArchiveMaps(dst map[string]int64, src map[string]int64) map[string]int64 {
	return archivesvc.MergeThreadArchiveMaps(dst, src)
}

func ParseArchiveTimestamp(raw string) int64 {
	return archivesvc.ParseArchiveTimestamp(raw)
}

func BuildThreadArchiveRestoreDeps(
	resolveThreadArchiveRoot func() (string, error),
	sanitizeArchiveNameStrict func(string) (string, error),
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	copyFileOverwrite func(srcPath, targetPath string) error,
	fileSHA256 func(path string) (string, error),
	findLatestManifestPath func(threadDir string) (manifestPath string, found bool, err error),
	readManifestFile func(manifestPath string) (ThreadArchiveManifest, error),
	fileState func(path string) (ThreadArchiveFileState, error),
	removeFile func(path string) error,
) ThreadArchiveRestoreDeps {
	return archivesvc.BuildThreadArchiveRestoreDeps(
		resolveThreadArchiveRoot,
		sanitizeArchiveNameStrict,
		resolveCodexRootDir,
		pathWithinRoot,
		copyFileOverwrite,
		fileSHA256,
		findLatestManifestPath,
		readManifestFile,
		fileState,
		removeFile,
	)
}

func InspectThreadArchiveForRestore(threadID string, deps ThreadArchiveRestoreDeps) (ThreadArchiveRestoreNotice, error) {
	return archivesvc.InspectThreadArchiveForRestore(threadID, deps)
}

func RestoreThreadArchiveSources(
	threadID string,
	resolveThreadArchiveRoot func() (string, error),
	sanitizeArchiveNameStrict func(string) (string, error),
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	copyFileOverwrite func(srcPath, targetPath string) error,
	fileSHA256 func(path string) (string, error),
	findLatestManifestPath func(threadDir string) (manifestPath string, found bool, err error),
	readManifestFile func(manifestPath string) (ThreadArchiveManifest, error),
	fileState func(path string) (ThreadArchiveFileState, error),
	removeFile func(path string) error,
) ([]string, []string, error) {
	return archivesvc.RestoreThreadArchiveSources(
		threadID,
		resolveThreadArchiveRoot,
		sanitizeArchiveNameStrict,
		resolveCodexRootDir,
		pathWithinRoot,
		copyFileOverwrite,
		fileSHA256,
		findLatestManifestPath,
		readManifestFile,
		fileState,
		removeFile,
	)
}

func PruneArchivedCodexSourceFiles(
	threadID string,
	files []ThreadArchiveFile,
	archiveDir string,
	resolveCodexRootDir func() (string, error),
	pathWithinRoot func(root, path string) (bool, error),
	fileSHA256 func(path string) (string, error),
	fileState func(path string) (ThreadArchiveFileState, error),
	removeFile func(path string) error,
	removeEmptyCodexParentDir func(startDir, codexRoot string),
) {
	archivesvc.PruneArchivedCodexSourceFiles(
		threadID,
		files,
		archiveDir,
		resolveCodexRootDir,
		pathWithinRoot,
		fileSHA256,
		fileState,
		removeFile,
		removeEmptyCodexParentDir,
	)
}

func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

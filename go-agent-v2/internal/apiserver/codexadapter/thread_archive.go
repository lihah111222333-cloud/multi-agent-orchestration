package codexadapter

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/codex"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// ThreadArchiveRestoreNotice describes archive integrity status before restore.
type ThreadArchiveRestoreNotice struct {
	Modified      bool
	ManifestPath  string
	ModifiedFiles []string
}

// PruneArchivedCodexSourceFilesOptions carries pruning dependencies.
type PruneArchivedCodexSourceFilesOptions struct {
	ThreadID                  string
	Files                     []ThreadArchiveFile
	ArchiveDir                string
	ResolveCodexRootDir       func() (string, error)
	PathWithinRoot            func(root, path string) (bool, error)
	FileSHA256                func(path string) (string, error)
	RemoveEmptyCodexParentDir func(startDir, codexRoot string)
}

// CollectThreadArtifactCandidates enumerates candidate codex files for archive.
func CollectThreadArtifactCandidates(codexThreadID string, rolloutPath string) []ThreadArtifactCandidate {
	candidates := make([]ThreadArtifactCandidate, 0, 8)
	seen := make(map[string]struct{}, 8)
	addCandidate := func(kind, path string) {
		cleaned := strings.TrimSpace(path)
		if cleaned == "" {
			return
		}
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		candidates = append(candidates, ThreadArtifactCandidate{Kind: kind, Path: cleaned})
	}

	resolvedRollout := strings.TrimSpace(rolloutPath)
	if resolvedRollout == "" && strings.TrimSpace(codexThreadID) != "" {
		if found, err := codex.FindRolloutPath(codexThreadID); err == nil {
			resolvedRollout = found
		}
	}
	if resolvedRollout != "" {
		addCandidate("rollout", resolvedRollout)
	}
	if strings.TrimSpace(codexThreadID) == "" {
		return candidates
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return candidates
	}
	addCandidate("shell_snapshot", filepath.Join(homeDir, ".codex", "shell_snapshots", codexThreadID+".sh"))

	searchRoots := []string{
		filepath.Join(homeDir, ".codex", "sessions"),
		filepath.Join(homeDir, ".codex", "shell_snapshots"),
		filepath.Join(homeDir, ".codex", "archived_sessions"),
		filepath.Join(homeDir, ".codex", "tmp"),
	}
	for _, root := range searchRoots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d == nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.Contains(name, codexThreadID) {
				return nil
			}
			addCandidate(InferThreadArtifactKind(name), path)
			return nil
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].Path < candidates[j].Path
	})
	return candidates
}

// InferThreadArtifactKind infers the artifact kind from filename.
func InferThreadArtifactKind(filename string) string {
	lower := strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasPrefix(lower, "rollout-") && strings.HasSuffix(lower, ".jsonl"):
		return "rollout"
	case strings.Contains(lower, "bp"):
		return "breakpoint"
	case strings.HasSuffix(lower, ".sh"):
		return "shell_snapshot"
	case strings.HasSuffix(lower, ".jsonl"):
		return "jsonl"
	default:
		return "artifact"
	}
}

// PruneArchivedCodexSourceFiles removes source codex files once archived safely.
func PruneArchivedCodexSourceFiles(opt PruneArchivedCodexSourceFilesOptions) {
	if len(opt.Files) == 0 {
		return
	}
	if opt.ResolveCodexRootDir == nil || opt.PathWithinRoot == nil || opt.FileSHA256 == nil {
		return
	}
	threadID := strings.TrimSpace(opt.ThreadID)
	codexRoot, err := opt.ResolveCodexRootDir()
	if err != nil {
		logger.Error("thread/archive: resolve codex root failed",
			logger.FieldThreadID, threadID,
			logger.FieldError, err,
		)
		return
	}

	archiveRoot := strings.TrimSpace(opt.ArchiveDir)
	seen := make(map[string]struct{}, len(opt.Files))
	deleted := 0
	for _, meta := range opt.Files {
		srcPath := strings.TrimSpace(meta.SourcePath)
		if srcPath == "" {
			continue
		}
		if _, ok := seen[srcPath]; ok {
			continue
		}
		seen[srcPath] = struct{}{}

		withinCodex, err := opt.PathWithinRoot(codexRoot, srcPath)
		if err != nil || !withinCodex {
			continue
		}
		if archiveRoot != "" {
			if withinArchive, err := opt.PathWithinRoot(archiveRoot, srcPath); err == nil && withinArchive {
				continue
			}
		}

		info, err := os.Stat(srcPath)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Error("thread/archive: stat source artifact failed",
					logger.FieldThreadID, threadID,
					"source_path", srcPath,
					logger.FieldError, err,
				)
			}
			continue
		}
		if info.IsDir() {
			continue
		}

		expectedSHA256 := strings.TrimSpace(meta.SHA256)
		if expectedSHA256 == "" {
			continue
		}
		sourceSHA256, err := opt.FileSHA256(srcPath)
		if err != nil {
			logger.Error("thread/archive: source artifact checksum failed",
				logger.FieldThreadID, threadID,
				"source_path", srcPath,
				logger.FieldError, err,
			)
			continue
		}
		if !strings.EqualFold(expectedSHA256, sourceSHA256) {
			logger.Error("thread/archive: source artifact changed after backup, skip delete",
				logger.FieldThreadID, threadID,
				"source_path", srcPath,
				"expected_sha256", expectedSHA256,
				"actual_sha256", sourceSHA256,
			)
			continue
		}

		if err := os.Remove(srcPath); err != nil {
			logger.Error("thread/archive: remove source artifact failed",
				logger.FieldThreadID, threadID,
				"source_path", srcPath,
				logger.FieldError, err,
			)
			continue
		}
		deleted++
		if opt.RemoveEmptyCodexParentDir != nil {
			opt.RemoveEmptyCodexParentDir(filepath.Dir(srcPath), codexRoot)
		}
	}

	if deleted > 0 {
		logger.Info("thread/archive: pruned codex source artifacts",
			logger.FieldThreadID, threadID,
			"deleted_count", deleted,
		)
	}
}

// RestoreThreadArchiveSourcesOptions configures restore-time dependencies.
type RestoreThreadArchiveSourcesOptions struct {
	ThreadID                            string
	ResolveThreadArchiveRoot            func() (string, error)
	SanitizeArchiveNameStrict           func(string) (string, error)
	ResolveCodexRootDir                 func() (string, error)
	PathWithinRoot                      func(root, path string) (bool, error)
	CopyFileOverwrite                   func(srcPath, targetPath string) error
	FileSHA256                          func(path string) (string, error)
	FindLatestThreadArchiveManifestPath func(threadDir string) (string, error)
	ReadThreadArchiveManifest           func(manifestPath string) (ThreadArchiveManifest, error)
}

// RestoreThreadArchiveSources restores archived source files back to codex root.
func RestoreThreadArchiveSources(opt RestoreThreadArchiveSourcesOptions) ([]string, []string, error) {
	restored := []string{}
	skipped := []string{}
	id := strings.TrimSpace(opt.ThreadID)
	if id == "" {
		return restored, skipped, nil
	}
	if opt.ResolveThreadArchiveRoot == nil ||
		opt.SanitizeArchiveNameStrict == nil ||
		opt.ResolveCodexRootDir == nil ||
		opt.PathWithinRoot == nil ||
		opt.CopyFileOverwrite == nil ||
		opt.FileSHA256 == nil {
		return nil, nil, apperrors.New("restoreThreadArchiveSources", "restore dependencies are not configured")
	}
	findLatestManifest := opt.FindLatestThreadArchiveManifestPath
	if findLatestManifest == nil {
		findLatestManifest = FindLatestThreadArchiveManifestPath
	}
	readManifest := opt.ReadThreadArchiveManifest
	if readManifest == nil {
		readManifest = ReadThreadArchiveManifest
	}

	rootDir, err := opt.ResolveThreadArchiveRoot()
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "resolve archive root")
	}
	safeThreadID, err := opt.SanitizeArchiveNameStrict(id)
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	manifestPath, err := findLatestManifest(threadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return restored, skipped, nil
		}
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "find latest manifest")
	}
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "read manifest")
	}
	codexRoot, err := opt.ResolveCodexRootDir()
	if err != nil {
		return nil, nil, apperrors.Wrap(err, "restoreThreadArchiveSources", "resolve codex root")
	}

	restoredSet := map[string]struct{}{}
	skippedSet := map[string]struct{}{}
	appendSkipped := func(sourcePath string, archivedPath string, reason string, skipErr error) {
		value := strings.TrimSpace(sourcePath)
		if value == "" {
			return
		}
		if skipErr != nil {
			logger.Error("thread/unarchive: restore artifact skipped",
				logger.FieldThreadID, id,
				"source_path", value,
				"archived_path", strings.TrimSpace(archivedPath),
				"reason", reason,
				logger.FieldError, skipErr,
			)
		} else {
			logger.Error("thread/unarchive: restore artifact skipped",
				logger.FieldThreadID, id,
				"source_path", value,
				"archived_path", strings.TrimSpace(archivedPath),
				"reason", reason,
			)
		}
		if _, ok := skippedSet[value]; ok {
			return
		}
		skippedSet[value] = struct{}{}
		skipped = append(skipped, value)
	}

	for _, meta := range manifest.Files {
		srcPath := strings.TrimSpace(meta.SourcePath)
		if srcPath == "" {
			continue
		}
		withinCodex, err := opt.PathWithinRoot(codexRoot, srcPath)
		if err != nil {
			appendSkipped(srcPath, "", "validate source path scope", err)
			continue
		}
		if !withinCodex {
			appendSkipped(srcPath, "", "source path is outside codex root", nil)
			continue
		}

		archivedPath := strings.TrimSpace(meta.ArchivedPath)
		if archivedPath == "" {
			appendSkipped(srcPath, "", "archived path is empty", nil)
			continue
		}
		if !filepath.IsAbs(archivedPath) && strings.TrimSpace(manifest.ArchiveDir) != "" {
			archivedPath = filepath.Join(strings.TrimSpace(manifest.ArchiveDir), archivedPath)
		}
		if strings.TrimSpace(manifest.ArchiveDir) != "" {
			withinArchive, err := opt.PathWithinRoot(manifest.ArchiveDir, archivedPath)
			if err != nil {
				appendSkipped(srcPath, archivedPath, "validate archived path scope", err)
				continue
			}
			if !withinArchive {
				appendSkipped(srcPath, archivedPath, "archived path is outside archive root", nil)
				continue
			}
		}

		info, err := os.Stat(archivedPath)
		if err != nil {
			appendSkipped(srcPath, archivedPath, "stat archived file", err)
			continue
		}
		if info.IsDir() {
			appendSkipped(srcPath, archivedPath, "archived path is a directory", nil)
			continue
		}

		expectedSHA256 := strings.TrimSpace(meta.SHA256)
		if expectedSHA256 != "" {
			actualArchiveSHA256, err := opt.FileSHA256(archivedPath)
			if err != nil {
				appendSkipped(srcPath, archivedPath, "compute archived checksum", err)
				continue
			}
			if !strings.EqualFold(expectedSHA256, actualArchiveSHA256) {
				appendSkipped(srcPath, archivedPath, "archived checksum mismatch", nil)
				continue
			}
		}

		if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
			appendSkipped(srcPath, archivedPath, "ensure source parent dir", err)
			continue
		}
		if err := opt.CopyFileOverwrite(archivedPath, srcPath); err != nil {
			appendSkipped(srcPath, archivedPath, "restore file to source path", err)
			continue
		}
		if expectedSHA256 != "" {
			actualSourceSHA256, err := opt.FileSHA256(srcPath)
			if err != nil {
				_ = os.Remove(srcPath)
				appendSkipped(srcPath, archivedPath, "compute restored source checksum", err)
				continue
			}
			if !strings.EqualFold(expectedSHA256, actualSourceSHA256) {
				_ = os.Remove(srcPath)
				appendSkipped(srcPath, archivedPath, "restored source checksum mismatch", nil)
				continue
			}
		}
		if _, ok := restoredSet[srcPath]; !ok {
			restoredSet[srcPath] = struct{}{}
			restored = append(restored, srcPath)
		}
	}
	sort.Strings(restored)
	sort.Strings(skipped)
	return restored, skipped, nil
}

// InspectThreadArchiveForRestoreOptions configures manifest integrity check.
type InspectThreadArchiveForRestoreOptions struct {
	ThreadID                            string
	ResolveThreadArchiveRoot            func() (string, error)
	SanitizeArchiveNameStrict           func(string) (string, error)
	PathWithinRoot                      func(root, path string) (bool, error)
	FileSHA256                          func(path string) (string, error)
	FindLatestThreadArchiveManifestPath func(threadDir string) (string, error)
	ReadThreadArchiveManifest           func(manifestPath string) (ThreadArchiveManifest, error)
}

// InspectThreadArchiveForRestore verifies archived files before restore.
func InspectThreadArchiveForRestore(opt InspectThreadArchiveForRestoreOptions) (ThreadArchiveRestoreNotice, error) {
	notice := ThreadArchiveRestoreNotice{
		Modified:      false,
		ManifestPath:  "",
		ModifiedFiles: []string{},
	}
	id := strings.TrimSpace(opt.ThreadID)
	if id == "" {
		return notice, nil
	}
	if opt.ResolveThreadArchiveRoot == nil ||
		opt.SanitizeArchiveNameStrict == nil ||
		opt.PathWithinRoot == nil ||
		opt.FileSHA256 == nil {
		return notice, apperrors.New("inspectThreadArchiveForRestore", "inspect dependencies are not configured")
	}
	findLatestManifest := opt.FindLatestThreadArchiveManifestPath
	if findLatestManifest == nil {
		findLatestManifest = FindLatestThreadArchiveManifestPath
	}
	readManifest := opt.ReadThreadArchiveManifest
	if readManifest == nil {
		readManifest = ReadThreadArchiveManifest
	}

	rootDir, err := opt.ResolveThreadArchiveRoot()
	if err != nil {
		return notice, err
	}
	safeThreadID, err := opt.SanitizeArchiveNameStrict(id)
	if err != nil {
		return notice, apperrors.Wrap(err, "inspectThreadArchiveForRestore", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	manifestPath, err := findLatestManifest(threadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return notice, nil
		}
		return notice, apperrors.Wrap(err, "inspectThreadArchiveForRestore", "find latest manifest")
	}
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return notice, apperrors.Wrap(err, "inspectThreadArchiveForRestore", "read manifest")
	}
	notice.ManifestPath = manifestPath

	modified := make([]string, 0, len(manifest.Files))
	for _, meta := range manifest.Files {
		archivedPath := strings.TrimSpace(meta.ArchivedPath)
		if archivedPath == "" {
			continue
		}
		if !filepath.IsAbs(archivedPath) && strings.TrimSpace(manifest.ArchiveDir) != "" {
			archivedPath = filepath.Join(strings.TrimSpace(manifest.ArchiveDir), archivedPath)
		}
		if strings.TrimSpace(manifest.ArchiveDir) != "" {
			withinRoot, err := opt.PathWithinRoot(manifest.ArchiveDir, archivedPath)
			if err != nil || !withinRoot {
				modified = append(modified, archivedPath)
				continue
			}
		}
		info, err := os.Stat(archivedPath)
		if err != nil || info.IsDir() {
			modified = append(modified, archivedPath)
			continue
		}
		if meta.SizeBytes > 0 && info.Size() != meta.SizeBytes {
			modified = append(modified, archivedPath)
			continue
		}
		if checksum := strings.TrimSpace(meta.SHA256); checksum != "" {
			actualSHA256, err := opt.FileSHA256(archivedPath)
			if err != nil || !strings.EqualFold(checksum, actualSHA256) {
				modified = append(modified, archivedPath)
				continue
			}
		}
	}
	if len(modified) > 0 {
		sort.Strings(modified)
		notice.Modified = true
		notice.ModifiedFiles = modified
	}
	return notice, nil
}

// FindLatestThreadArchiveManifestPath returns the newest manifest path under thread dir.
func FindLatestThreadArchiveManifestPath(threadDir string) (string, error) {
	info, err := os.Stat(threadDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", apperrors.Newf("findLatestThreadArchiveManifestPath", "thread archive path is not a directory: %s", threadDir)
	}

	type candidate struct {
		Path       string
		ModifiedAt int64
	}
	candidates := make([]candidate, 0, 8)
	appendCandidate := func(path string) error {
		fileInfo, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if fileInfo.IsDir() {
			return nil
		}
		candidates = append(candidates, candidate{
			Path:       path,
			ModifiedAt: fileInfo.ModTime().UnixNano(),
		})
		return nil
	}

	if err := appendCandidate(filepath.Join(threadDir, "manifest.json")); err != nil {
		return "", apperrors.Wrap(err, "findLatestThreadArchiveManifestPath", "stat legacy manifest")
	}
	entries, err := os.ReadDir(threadDir)
	if err != nil {
		return "", apperrors.Wrap(err, "findLatestThreadArchiveManifestPath", "read thread archive dir")
	}
	for _, entry := range entries {
		if entry == nil || !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(threadDir, entry.Name(), "manifest.json")
		if err := appendCandidate(manifestPath); err != nil {
			return "", apperrors.Wrapf(err, "findLatestThreadArchiveManifestPath", "stat manifest for snapshot %s", entry.Name())
		}
	}
	if len(candidates) == 0 {
		return "", os.ErrNotExist
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].ModifiedAt != candidates[j].ModifiedAt {
			return candidates[i].ModifiedAt > candidates[j].ModifiedAt
		}
		return candidates[i].Path > candidates[j].Path
	})
	return candidates[0].Path, nil
}

// ReadThreadArchiveManifest loads archive manifest from disk.
func ReadThreadArchiveManifest(manifestPath string) (ThreadArchiveManifest, error) {
	manifest := ThreadArchiveManifest{}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

// WriteThreadArchiveManifest persists archive manifest in archiveDir/manifest.json.
func WriteThreadArchiveManifest(manifest ThreadArchiveManifest) error {
	if strings.TrimSpace(manifest.ArchiveDir) == "" {
		return apperrors.New("writeThreadArchiveManifest", "archive dir is empty")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(manifest.ArchiveDir, "manifest.json")
	return os.WriteFile(manifestPath, data, 0o644)
}

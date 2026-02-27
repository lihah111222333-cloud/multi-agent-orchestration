package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex"
	archivesvc "github.com/multi-agent/go-agent-v2/pkg/codexsdk/service/archive"
	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

func allocateUniquePath(primary string, next func(index int) string) (string, error) {
	if _, err := os.Stat(primary); os.IsNotExist(err) {
		return primary, nil
	} else if err != nil {
		return "", err
	}
	for i := 2; i <= 9999; i++ {
		candidate := next(i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", os.ErrExist
}

func resolveUserHome(op string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", apperrors.Wrap(err, op, "resolve user home")
	}
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return "", apperrors.New(op, "user home is empty")
	}
	return homeDir, nil
}

func ResolveThreadArchiveRootDir() (string, error) {
	homeDir, err := resolveUserHome("ResolveThreadArchiveRootDir")
	if err != nil {
		return "", err
	}
	appRootDir := filepath.Join(homeDir, ".multi-agent")
	if err := os.MkdirAll(appRootDir, 0o755); err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveRootDir", "ensure app root")
	}
	archiveRoot := filepath.Join(appRootDir, "thread-archives")
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveRootDir", "ensure archive root")
	}
	return archiveRoot, nil
}

func ResolveThreadArchiveSnapshotDir(rootDir string, threadID string, archivedAt string) (string, error) {
	safeThreadID, err := archivesvc.SanitizeArchiveNameStrict(threadID)
	if err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveSnapshotDir", "sanitize thread id")
	}
	threadDir := filepath.Join(rootDir, safeThreadID)
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveSnapshotDir", "ensure thread dir")
	}
	snapshotName, err := archivesvc.SanitizeArchiveNameStrict(strings.TrimSpace(archivedAt))
	if err != nil {
		return "", apperrors.Wrap(err, "ResolveThreadArchiveSnapshotDir", "sanitize archive timestamp")
	}
	path, err := allocateUniquePath(filepath.Join(threadDir, snapshotName), func(i int) string {
		return filepath.Join(threadDir, fmt.Sprintf("%s-%d", snapshotName, i))
	})
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", apperrors.New("ResolveThreadArchiveSnapshotDir", "unable to allocate unique archive snapshot dir")
		}
		return "", apperrors.Wrap(err, "ResolveThreadArchiveSnapshotDir", "stat snapshot candidate")
	}
	return path, nil
}

func CollectThreadArtifactCandidates(codexThreadID string, rolloutPath string) []archivesvc.ThreadArtifactCandidate {
	candidates := make([]archivesvc.ThreadArtifactCandidate, 0, 8)
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
		candidates = append(candidates, archivesvc.ThreadArtifactCandidate{Kind: kind, Path: cleaned})
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
	if codexThreadID = strings.TrimSpace(codexThreadID); codexThreadID == "" {
		return candidates
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return candidates
	}
	addCandidate("shell_snapshot", filepath.Join(homeDir, ".codex", "shell_snapshots", codexThreadID+".sh"))

	for _, root := range []string{
		filepath.Join(homeDir, ".codex", "sessions"),
		filepath.Join(homeDir, ".codex", "shell_snapshots"),
		filepath.Join(homeDir, ".codex", "archived_sessions"),
		filepath.Join(homeDir, ".codex", "tmp"),
	} {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d == nil || d.IsDir() || !strings.Contains(d.Name(), codexThreadID) {
				return nil
			}
			addCandidate(archivesvc.InferThreadArtifactKind(d.Name()), path)
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

func NextArchiveFilePath(dir, filename string) (string, error) {
	base, err := archivesvc.SanitizeArchiveNameStrict(filepath.Base(filename))
	if err != nil {
		return "", apperrors.Wrap(err, "NextArchiveFilePath", "sanitize filename")
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	path, err := allocateUniquePath(filepath.Join(dir, base), func(i int) string {
		return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
	})
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", apperrors.New("NextArchiveFilePath", "unable to allocate unique archive filename")
		}
		return "", apperrors.Wrap(err, "NextArchiveFilePath", "stat archive target candidate")
	}
	return path, nil
}

func copyFileAtomic(srcPath, targetPath string, overwrite bool) error {
	op := "CopyFile"
	if overwrite {
		op = "CopyFileOverwrite"
	}
	if !overwrite {
		if _, err := os.Stat(targetPath); err == nil {
			return apperrors.Newf(op, "target already exists: %s", targetPath)
		} else if !os.IsNotExist(err) {
			return apperrors.Wrap(err, op, "stat target path")
		}
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	targetDir := filepath.Dir(targetPath)
	if overwrite {
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return err
		}
	}
	tmpFile, err := os.CreateTemp(targetDir, "."+filepath.Base(targetPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := io.Copy(tmpFile, src); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func CopyFile(srcPath, targetPath string) error { return copyFileAtomic(srcPath, targetPath, false) }

func CopyFileOverwrite(srcPath, targetPath string) error {
	return copyFileAtomic(srcPath, targetPath, true)
}

func FileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func FindLatestThreadArchiveManifestPath(threadDir string) (string, bool, error) {
	info, err := os.Stat(threadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if !info.IsDir() {
		return "", false, apperrors.Newf("FindLatestThreadArchiveManifestPath", "thread archive path is not a directory: %s", threadDir)
	}

	var latestPath string
	var latestAt int64
	checkCandidate := func(path, action string) error {
		stat, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return apperrors.Wrap(err, "FindLatestThreadArchiveManifestPath", action)
		}
		if stat.IsDir() {
			return nil
		}
		modAt := stat.ModTime().UnixNano()
		if latestPath == "" || modAt > latestAt || (modAt == latestAt && path > latestPath) {
			latestPath, latestAt = path, modAt
		}
		return nil
	}

	if err := checkCandidate(filepath.Join(threadDir, "manifest.json"), "stat legacy manifest"); err != nil {
		return "", false, err
	}
	entries, err := os.ReadDir(threadDir)
	if err != nil {
		return "", false, apperrors.Wrap(err, "FindLatestThreadArchiveManifestPath", "read thread archive dir")
	}
	for _, entry := range entries {
		if entry == nil || !entry.IsDir() {
			continue
		}
		if err := checkCandidate(filepath.Join(threadDir, entry.Name(), "manifest.json"), fmt.Sprintf("stat manifest for snapshot %s", entry.Name())); err != nil {
			return "", false, err
		}
	}
	return latestPath, latestPath != "", nil
}

func ReadThreadArchiveManifest(manifestPath string) (archivesvc.ThreadArchiveManifest, error) {
	manifest := archivesvc.ThreadArchiveManifest{}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func WriteThreadArchiveManifest(manifest archivesvc.ThreadArchiveManifest) error {
	if strings.TrimSpace(manifest.ArchiveDir) == "" {
		return apperrors.New("WriteThreadArchiveManifest", "archive dir is empty")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(manifest.ArchiveDir, "manifest.json"), data, 0o644)
}

func ResolveCodexRootDir() (string, error) {
	homeDir, err := resolveUserHome("ResolveCodexRootDir")
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".codex"), nil
}

func FileState(path string) (archivesvc.ThreadArchiveFileState, error) {
	state := archivesvc.ThreadArchiveFileState{}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	state.Exists, state.IsDir, state.SizeBytes = true, info.IsDir(), info.Size()
	return state, nil
}

func RemoveFile(path string) error { return os.Remove(path) }

func RemoveEmptyCodexParentDirs(startDir string, codexRoot string) {
	current := strings.TrimSpace(startDir)
	root := strings.TrimSpace(codexRoot)
	if current == "" || root == "" {
		return
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for current != "" {
		currentAbs, err := filepath.Abs(current)
		if err != nil || currentAbs == rootAbs {
			return
		}
		withinRoot, err := archivesvc.PathWithinRoot(rootAbs, currentAbs)
		if err != nil || !withinRoot {
			return
		}
		entries, err := os.ReadDir(currentAbs)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(currentAbs); err != nil {
			return
		}
		parent := filepath.Dir(currentAbs)
		if parent == currentAbs {
			return
		}
		current = parent
	}
}

func LoadThreadArchiveMapFromDisk() (map[string]int64, error) {
	rootDir, err := ResolveThreadArchiveRootDir()
	if err != nil {
		return nil, err
	}
	return collectThreadArchiveMapFromRoot(rootDir)
}

func collectThreadArchiveMapFromRoot(rootDir string) (map[string]int64, error) {
	result := map[string]int64{}
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return result, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		threadID := strings.TrimSpace(entry.Name())
		if threadID == "" {
			continue
		}
		threadDir := filepath.Join(root, entry.Name())

		var archivedAt int64
		if snapshots, readErr := os.ReadDir(threadDir); readErr == nil {
			for _, snapshot := range snapshots {
				if snapshot != nil && snapshot.IsDir() {
					if parsed := archivesvc.ParseArchiveTimestamp(snapshot.Name()); parsed > archivedAt {
						archivedAt = parsed
					}
				}
			}
		}

		manifestPath, found, manifestErr := FindLatestThreadArchiveManifestPath(threadDir)
		if manifestErr == nil && found {
			if manifest, readErr := ReadThreadArchiveManifest(manifestPath); readErr == nil {
				if id := strings.TrimSpace(manifest.ThreadID); id != "" {
					threadID = id
				}
				if parsed := archivesvc.ParseArchiveTimestamp(manifest.ArchivedAt); parsed > 0 {
					archivedAt = parsed
				}
			}
		}
		if archivedAt > 0 {
			if current, ok := result[threadID]; ok && archivedAt <= current {
				continue
			}
			result[threadID] = archivedAt
		}
	}
	return result, nil
}
